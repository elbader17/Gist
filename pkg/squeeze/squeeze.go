// Package squeeze composes the Gist context-optimization pipeline into a
// single call: file pruning + cache + aligner + token-cap enforcement.
//
// Callers get a single self-contained prompt block plus per-section
// token hints and an estimated savings figure. Repeated calls on the same
// files benefit from the bundled LRU cache.
package squeeze

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/elbader17/gist/pkg/aligner"
	"github.com/elbader17/gist/pkg/ast"
	"github.com/elbader17/gist/pkg/cache"
	"github.com/elbader17/gist/pkg/tokenizer"
)

// FileSource describes a file to be pruned and included as a static-files
// layer entry.
type FileSource struct {
	Path           string   `json:"path"`
	FocusFunctions []string `json:"focus_functions,omitempty"`
}

// Options configures a Squeeze call.
type Options struct {
	SessionID    string        `json:"session_id,omitempty"`
	SystemPrompts []string     `json:"system_prompts,omitempty"`
	StaticFiles   []FileSource `json:"static_files,omitempty"`
	History       string        `json:"history,omitempty"`
	DynamicInput  string        `json:"dynamic_input,omitempty"`
	MaxTokens     int           `json:"max_tokens,omitempty"`
	Encoding      tokenizer.Encoding `json:"encoding,omitempty"`
	Cache         *cache.Cache  `json:"-"`
}

// Section is one aligned layer of the squeezed output.
type Section struct {
	Layer     int    `json:"layer"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	Tokens    int    `json:"tokens"`
	Hash      string `json:"hash"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Result is the structured output of Squeeze.
type Result struct {
	Sections       []Section `json:"sections"`
	Combined       string    `json:"combined"`
	Markdown       string    `json:"markdown"`
	TotalTokens    int       `json:"total_tokens"`
	MaxTokens      int       `json:"max_tokens"`
	Truncated      bool      `json:"truncated"`
	OriginalTokens int       `json:"original_tokens"`
	SavedTokens    int       `json:"saved_tokens"`
	SavedRatio     float64   `json:"saved_ratio"`
	CacheHits      int       `json:"cache_hits"`
	CacheMisses    int       `json:"cache_misses"`
	Warnings       []string  `json:"warnings,omitempty"`
	CacheReady     bool      `json:"cache_ready"`
}

// ErrNoInputs is returned when callers provide neither files, system
// prompts, history, nor dynamic input.
var ErrNoInputs = errors.New("squeeze: no inputs provided")

// Squeeze runs the pipeline.
func Squeeze(opts Options) (*Result, error) {
	if len(opts.SystemPrompts) == 0 && len(opts.StaticFiles) == 0 &&
		opts.History == "" && opts.DynamicInput == "" {
		return nil, ErrNoInputs
	}

	tok := tokenizer.New(opts.Encoding)
	res := &Result{MaxTokens: opts.MaxTokens, Warnings: []string{}}

	// Layer 1: system rules.
	sysContent := joinUnique(opts.SystemPrompts)
	res.Sections = append(res.Sections, Section{
		Layer: 1, Name: "system_rules",
		Content: sysContent,
		Tokens:  tok.CountString(sysContent),
		Hash:    blockHash(sysContent),
	})

	// Layer 2: static files (pruned, cached, parallel).
	staticParts, originalTokens, hits, misses := pruneFiles(opts, tok)
	res.CacheHits = hits
	res.CacheMisses = misses
	sort.Strings(staticParts)
	staticContent := strings.Join(staticParts, "\n\n")
	res.Sections = append(res.Sections, Section{
		Layer: 2, Name: "static_files",
		Content: staticContent,
		Tokens:  tok.CountString(staticContent),
		Hash:    blockHash(staticContent),
	})

	// Layer 3: history (cheap to trim, do it last if needed).
	histContent := strings.TrimSpace(opts.History)
	res.Sections = append(res.Sections, Section{
		Layer: 3, Name: "history",
		Content: histContent,
		Tokens:  tok.CountString(histContent),
		Hash:    blockHash(histContent),
	})

	// Layer 4: dynamic input (always last, never trimmed).
	dynContent := strings.TrimSpace(opts.DynamicInput)
	res.Sections = append(res.Sections, Section{
		Layer: 4, Name: "dynamic_input",
		Content: dynContent,
		Tokens:  tok.CountString(dynContent),
		Hash:    blockHash(dynContent),
	})

	res.TotalTokens = 0
	for _, s := range res.Sections {
		res.TotalTokens += s.Tokens
	}
	res.CacheReady = res.Sections[0].Tokens >= aligner.MinCacheBlockTokens &&
		res.Sections[1].Tokens >= aligner.MinCacheBlockTokens

	// Enforce max_tokens: trim history first, then static.
	if opts.MaxTokens > 0 && res.TotalTokens > opts.MaxTokens {
		overflow := res.TotalTokens - opts.MaxTokens
		overflow = trimSection(&res.Sections[2], overflow, opts.Encoding, &res.Truncated)
		if overflow > 0 {
			overflow = trimSection(&res.Sections[1], overflow, opts.Encoding, &res.Truncated)
		}
		if overflow > 0 {
			overflow = trimSection(&res.Sections[0], overflow, opts.Encoding, &res.Truncated)
		}
		res.TotalTokens = 0
		for _, s := range res.Sections {
			res.TotalTokens += s.Tokens
		}
	}

	// Combine as plain markdown and as aligned layer-marker markdown.
	res.Combined = joinSections(res.Sections, "\n\n---\n\n")
	aligned := &aligner.AlignedPayload{
		Blocks: []aligner.AlignedBlock{
			{Layer: 1, LayerName: "system_rules", Content: res.Sections[0].Content, StableHash: res.Sections[0].Hash, TokenHint: res.Sections[0].Tokens},
			{Layer: 2, LayerName: "static_files", Content: res.Sections[1].Content, StableHash: res.Sections[1].Hash, TokenHint: res.Sections[1].Tokens},
			{Layer: 3, LayerName: "history", Content: res.Sections[2].Content, StableHash: res.Sections[2].Hash, TokenHint: res.Sections[2].Tokens},
			{Layer: 4, LayerName: "dynamic_input", Content: res.Sections[3].Content, StableHash: res.Sections[3].Hash, TokenHint: res.Sections[3].Tokens},
		},
		CacheReady: res.CacheReady,
		TotalHint:  res.TotalTokens,
	}
	res.Markdown = aligner.RenderMarkdown(aligned)

	// Savings: compare the static-files' original size vs slim size plus
	// the dynamic-layer (which is always sent verbatim).
	res.OriginalTokens = originalTokens + res.Sections[0].Tokens + res.Sections[2].Tokens + res.Sections[3].Tokens
	res.SavedTokens = res.OriginalTokens - res.TotalTokens
	if res.SavedTokens < 0 {
		res.SavedTokens = 0
	}
	if res.OriginalTokens > 0 {
		res.SavedRatio = float64(res.SavedTokens) / float64(res.OriginalTokens)
	}

	if !res.CacheReady {
		if res.Sections[0].Tokens < aligner.MinCacheBlockTokens {
			res.Warnings = append(res.Warnings, "system_rules block below provider cache threshold")
		}
		if res.Sections[1].Tokens < aligner.MinCacheBlockTokens {
			res.Warnings = append(res.Warnings, "static_files block below provider cache threshold")
		}
	}
	return res, nil
}

type pruneJob struct {
	index   int
	path    string
	focus   []string
	content string
	tokens  int
	original int
	hit     bool
	err     error
}

func pruneFiles(opts Options, tok *tokenizer.Tokenizer) ([]string, int, int, int) {
	if len(opts.StaticFiles) == 0 {
		return nil, 0, 0, 0
	}
	jobs := make([]pruneJob, len(opts.StaticFiles))
	var wg sync.WaitGroup
	for i, fs := range opts.StaticFiles {
		i, fs := i, fs
		wg.Add(1)
		go func() {
			defer wg.Done()
			jobs[i].index = i
			jobs[i].path = fs.Path
			jobs[i].focus = fs.FocusFunctions
			if opts.Cache != nil {
				if e, ok := opts.Cache.Get(fs.Path); ok {
					jobs[i].content = e.Slim
					jobs[i].tokens = e.SlimTokens
					jobs[i].original = e.OriginalTokens
					jobs[i].hit = true
					return
				}
			}
			res, err := ast.BuildSlim(fs.Path, fs.FocusFunctions, 0)
			if err != nil {
				jobs[i].err = err
				jobs[i].content = "// error: " + err.Error()
				jobs[i].tokens = 1
				return
			}
			jobs[i].content = res.Slim
			jobs[i].tokens = tok.CountString(res.Slim)
			jobs[i].original = int(res.OriginalSize / 4)
			if opts.Cache != nil {
				opts.Cache.Put(fs.Path, res.Slim, jobs[i].tokens)
			}
		}()
	}
	wg.Wait()

	out := make([]string, 0, len(jobs))
	hits, misses := 0, 0
	original := 0
	for _, j := range jobs {
		if j.hit {
			hits++
		} else {
			misses++
		}
		original += j.original
		out = append(out, j.content)
	}
	return out, original, hits, misses
}

func trimSection(s *Section, overflow int, enc tokenizer.Encoding, truncated *bool) int {
	if overflow <= 0 || s.Tokens == 0 {
		return overflow
	}
	tok := tokenizer.New(enc)
	target := s.Tokens - overflow
	if target < 0 {
		target = 0
	}
	if target == s.Tokens {
		return overflow
	}
	s.Content = tokenizer.TruncateToTokens(s.Content, target, enc)
	s.Tokens = tok.CountString(s.Content)
	s.Truncated = true
	*truncated = true
	return overflow - (s.Tokens - target)
}

func joinSections(sections []Section, sep string) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s.Content)
	}
	return b.String()
}

func joinUnique(parts []string) string {
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return strings.Join(out, "\n\n")
}

func blockHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}