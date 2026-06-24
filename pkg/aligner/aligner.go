// Package aligner reorders prompt components into stable layers optimized
// for provider-side prompt caching.
//
// The four layers produced by Align are, in order:
//
//   1. system_rules    - deduped system prompts.
//   2. static_files    - deduped file contents, sorted alphabetically for
//                        byte-stable cache keys.
//   3. history         - prior conversation turn.
//   4. dynamic_input   - errors or other runtime-varying content.
//
// CacheReady reports whether the first two layers exceed the 1024-token
// minimum cache block required by Anthropic / Google / OpenAI.
package aligner

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// AlignedBlock is a single layer of an aligned prompt.
type AlignedBlock struct {
	Layer      int      `json:"layer"`
	LayerName  string   `json:"layer_name"`
	Content    string   `json:"content"`
	TokenHint  int      `json:"token_hint"`
	StableHash string   `json:"stable_hash"`
	Components []string `json:"components,omitempty"`
}

// AlignedPayload is the result of an Align call.
type AlignedPayload struct {
	Blocks     []AlignedBlock `json:"blocks"`
	Combined   string         `json:"combined"`
	CacheReady bool           `json:"cache_ready"`
	TotalHint  int            `json:"total_token_hint"`
	Warnings   []string       `json:"warnings,omitempty"`
}

// Layer identifiers.
const (
	LayerSystemRules  = 1
	LayerStaticFiles  = 2
	LayerHistory      = 3
	LayerDynamicInput = 4
)

// MinCacheBlockTokens is the minimum size each cacheable layer must reach.
const MinCacheBlockTokens = 1024

// Align builds the four-layer payload from the supplied components.
func Align(systemPrompts, staticFilesContext []string, dynamicInput, history string) *AlignedPayload {
	system := joinUnique(systemPrompts)
	static := joinAndSort(staticFilesContext)

	blocks := make([]AlignedBlock, 0, 4)
	blocks = append(blocks, AlignedBlock{
		Layer:      LayerSystemRules,
		LayerName:  "system_rules",
		Content:    system,
		TokenHint:  estimateTokens(system),
		StableHash: hashBlock(system),
		Components: append([]string{}, systemPrompts...),
	})
	blocks = append(blocks, AlignedBlock{
		Layer:      LayerStaticFiles,
		LayerName:  "static_files",
		Content:    static,
		TokenHint:  estimateTokens(static),
		StableHash: hashBlock(static),
		Components: staticFilesContext,
	})
	blocks = append(blocks, AlignedBlock{
		Layer:      LayerHistory,
		LayerName:  "history",
		Content:    strings.TrimSpace(history),
		TokenHint:  estimateTokens(history),
		StableHash: hashBlock(history),
	})
	blocks = append(blocks, AlignedBlock{
		Layer:      LayerDynamicInput,
		LayerName:  "dynamic_input",
		Content:    strings.TrimSpace(dynamicInput),
		TokenHint:  estimateTokens(dynamicInput),
		StableHash: hashBlock(dynamicInput),
	})

	var combined strings.Builder
	total := 0
	warnings := []string{}
	for i, b := range blocks {
		if i > 0 {
			combined.WriteString("\n\n---\n\n")
		}
		combined.WriteString(b.Content)
		total += b.TokenHint
	}

	cacheReady := blocks[0].TokenHint >= MinCacheBlockTokens && blocks[1].TokenHint >= MinCacheBlockTokens
	if !cacheReady {
		if blocks[0].TokenHint < MinCacheBlockTokens {
			warnings = append(warnings, "system_rules block below provider cache threshold")
		}
		if blocks[1].TokenHint < MinCacheBlockTokens {
			warnings = append(warnings, "static_files block below provider cache threshold")
		}
	}

	return &AlignedPayload{
		Blocks:     blocks,
		Combined:   combined.String(),
		CacheReady: cacheReady,
		TotalHint:  total,
		Warnings:   warnings,
	}
}

func joinUnique(items []string) string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		s := strings.TrimSpace(it)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return strings.Join(out, "\n\n")
}

func joinAndSort(items []string) string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		s := strings.TrimSpace(it)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, "\n\n")
}

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

func hashBlock(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}