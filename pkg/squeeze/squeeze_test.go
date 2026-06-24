package squeeze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elbader17/gist/pkg/cache"
	"github.com/elbader17/gist/pkg/tokenizer"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestSqueezeBasicSections(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.go", "package x\nfunc A() int { return 1 + 1 + 1 + 1 + 1 }\n")

	res, err := Squeeze(Options{
		SystemPrompts: []string{"You are concise."},
		StaticFiles:   []FileSource{{Path: a}},
		DynamicInput:  "compile error: undefined foo",
		MaxTokens:     200,
		Encoding:      tokenizer.CL100KBase,
		Cache:         cache.New(64, 1<<20),
	})
	if err != nil {
		t.Fatalf("squeeze: %v", err)
	}
	if len(res.Sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(res.Sections))
	}
	if res.Sections[0].Name != "system_rules" {
		t.Fatalf("layer 1 should be system_rules, got %s", res.Sections[0].Name)
	}
	if res.Sections[3].Name != "dynamic_input" {
		t.Fatalf("layer 4 should be dynamic_input, got %s", res.Sections[3].Name)
	}
	if !strings.Contains(res.Combined, "You are concise.") {
		t.Fatalf("combined missing system: %s", res.Combined)
	}
	if !strings.Contains(res.Combined, "compile error") {
		t.Fatalf("combined missing dynamic: %s", res.Combined)
	}
	if !strings.Contains(res.Markdown, "<!-- layer:1:system_rules:") {
		t.Fatalf("markdown missing system_rules marker: %s", res.Markdown)
	}
	if !strings.Contains(res.Markdown, "<!-- layer:4:dynamic_input:") {
		t.Fatalf("markdown missing dynamic_input marker: %s", res.Markdown)
	}
}

func TestSqueezeCacheHits(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.go", "package x\nfunc A() int { return 1 }\n")
	c := cache.New(64, 1<<20)

	opts := Options{
		SystemPrompts: []string{"s"},
		StaticFiles:   []FileSource{{Path: a}},
		DynamicInput:  "d",
		Cache:         c,
	}

	first, err := Squeeze(opts)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.CacheMisses != 1 || first.CacheHits != 0 {
		t.Fatalf("expected 0 hits/1 miss, got %d/%d", first.CacheHits, first.CacheMisses)
	}

	second, err := Squeeze(opts)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.CacheHits != 1 || second.CacheMisses != 0 {
		t.Fatalf("expected 1 hit/0 miss, got %d/%d", second.CacheHits, second.CacheMisses)
	}
	if second.Sections[1].Content != first.Sections[1].Content {
		t.Fatalf("cached slim differs from first call")
	}
}

func TestSqueezeMaxTokensTrimsHistory(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.go", "package x\nfunc A() {}\n")

	res, err := Squeeze(Options{
		SystemPrompts: []string{"sys"},
		StaticFiles:   []FileSource{{Path: a}},
		History:       strings.Repeat("hist-line ", 500),
		DynamicInput:  "d",
		MaxTokens:     50,
		Cache:         cache.New(64, 1<<20),
	})
	if err != nil {
		t.Fatalf("squeeze: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("expected truncation flag")
	}
	if res.TotalTokens > 50 {
		t.Fatalf("total tokens %d exceeds cap 50", res.TotalTokens)
	}
	if res.Sections[2].Tokens > 50 {
		t.Fatalf("history should be trimmed first, got %d", res.Sections[2].Tokens)
	}
	if !res.Sections[2].Truncated {
		t.Fatalf("history should be marked truncated")
	}
}

func TestSqueezeCacheReady(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.go", "package x\n// "+strings.Repeat("comment ", 1500)+"\nfunc A() {}\n")

	bigSys := strings.Repeat("rule ", 1500)
	res, err := Squeeze(Options{
		SystemPrompts: []string{bigSys},
		StaticFiles:   []FileSource{{Path: a}},
		DynamicInput:  "d",
	})
	if err != nil {
		t.Fatalf("squeeze: %v", err)
	}
	if !res.CacheReady {
		t.Fatalf("expected CacheReady=true; tokens=%+v", res.Sections)
	}
}

func TestSqueezeNoInputs(t *testing.T) {
	if _, err := Squeeze(Options{}); err == nil {
		t.Fatalf("expected error for empty inputs")
	}
}

func TestSqueezeDedupSystemPrompts(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.go", "package x\nfunc A() {}\n")
	res, err := Squeeze(Options{
		SystemPrompts: []string{"same", "same", "different"},
		StaticFiles:   []FileSource{{Path: a}},
		DynamicInput:  "d",
	})
	if err != nil {
		t.Fatalf("squeeze: %v", err)
	}
	if strings.Count(res.Sections[0].Content, "same") != 1 {
		t.Fatalf("expected dedup, got %q", res.Sections[0].Content)
	}
	if !strings.Contains(res.Sections[0].Content, "different") {
		t.Fatalf("missing distinct prompt: %q", res.Sections[0].Content)
	}
}

func TestSqueezeStaticFilesSorted(t *testing.T) {
	dir := t.TempDir()
	z := writeFile(t, dir, "z.go", "package z\nfunc Z() {}\n")
	a := writeFile(t, dir, "a.go", "package a\nfunc A() {}\n")
	m := writeFile(t, dir, "m.go", "package m\nfunc M() {}\n")

	res, err := Squeeze(Options{
		StaticFiles:  []FileSource{{Path: z}, {Path: a}, {Path: m}},
		DynamicInput: "d",
	})
	if err != nil {
		t.Fatalf("squeeze: %v", err)
	}
	ia := strings.Index(res.Sections[1].Content, "package a")
	im := strings.Index(res.Sections[1].Content, "package m")
	iz := strings.Index(res.Sections[1].Content, "package z")
	if !(ia >= 0 && im > ia && iz > im) {
		t.Fatalf("static_files should be sorted alphabetically:\n%s", res.Sections[1].Content)
	}
}

func TestSqueezeSavings(t *testing.T) {
	dir := t.TempDir()
	body := "package x\nfunc A() int {\n\treturn " + strings.Repeat("1+", 50) + "0\n}\n"
	a := writeFile(t, dir, "a.go", body)
	res, err := Squeeze(Options{
		StaticFiles:  []FileSource{{Path: a}},
		DynamicInput: "d",
	})
	if err != nil {
		t.Fatalf("squeeze: %v", err)
	}
	if res.OriginalTokens <= res.TotalTokens {
		t.Fatalf("expected original > total; original=%d total=%d", res.OriginalTokens, res.TotalTokens)
	}
	if res.SavedTokens <= 0 {
		t.Fatalf("expected positive saved tokens, got %d", res.SavedTokens)
	}
	if res.SavedRatio <= 0 {
		t.Fatalf("expected positive ratio, got %.3f", res.SavedRatio)
	}
}

func TestSqueezeStableOutput(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.go", "package x\nfunc A() int { return 1 }\n")

	mkRes := func() *Result {
		r, err := Squeeze(Options{
			SystemPrompts: []string{"sys"},
			StaticFiles:   []FileSource{{Path: a}},
			DynamicInput:  "d",
			History:       "h",
			Cache:         cache.New(64, 1<<20),
		})
		if err != nil {
			t.Fatalf("squeeze: %v", err)
		}
		return r
	}
	r1, r2 := mkRes(), mkRes()
	if r1.Combined != r2.Combined {
		t.Fatalf("combined output should be stable across calls:\n%s\nvs\n%s", r1.Combined, r2.Combined)
	}
	if r1.Markdown != r2.Markdown {
		t.Fatalf("markdown output should be stable across calls")
	}
}