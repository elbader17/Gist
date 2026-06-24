package aligner

import (
	"strings"
	"testing"
)

func TestRenderMarkdownBasic(t *testing.T) {
	p := Align(
		[]string{"sys"},
		[]string{"file a"},
		"err",
		"hist",
	)
	md := RenderMarkdown(p)
	for _, marker := range []string{"<!-- layer:1:system_rules:", "<!-- layer:2:static_files:", "<!-- layer:3:history:", "<!-- layer:4:dynamic_input:"} {
		if !strings.Contains(md, marker) {
			t.Fatalf("missing marker %q in:\n%s", marker, md)
		}
	}
}

func TestRenderMarkdownStable(t *testing.T) {
	a := Align([]string{"s"}, []string{"x"}, "d", "h")
	b := Align([]string{"s"}, []string{"x"}, "d", "h")
	if RenderMarkdown(a) != RenderMarkdown(b) {
		t.Fatalf("identical inputs must produce identical markdown")
	}
}

func TestRenderMarkdownNilSafe(t *testing.T) {
	if got := RenderMarkdown(nil); got != "" {
		t.Fatalf("expected empty string for nil payload, got %q", got)
	}
}

func TestDedupRatio(t *testing.T) {
	// Two identical inputs -> ~50% savings on the dedup side.
	r := DedupRatio([]string{"abc", "abc"}, "abc")
	if r <= 0.4 || r >= 0.6 {
		t.Fatalf("expected ~0.5, got %.3f", r)
	}
	if r := DedupRatio(nil, ""); r != 0 {
		t.Fatalf("empty input should yield 0, got %.3f", r)
	}
	if r := DedupRatio([]string{"x"}, "x"); r != 0 {
		t.Fatalf("identical input/output should yield 0, got %.3f", r)
	}
}

func TestBlockHash(t *testing.T) {
	h1 := BlockHash("hello")
	h2 := BlockHash("hello")
	h3 := BlockHash("world")
	if h1 != h2 {
		t.Fatalf("equal strings must produce equal hashes")
	}
	if h1 == h3 {
		t.Fatalf("different strings must produce different hashes")
	}
	if len(h1) != 16 {
		t.Fatalf("hash should be 16 hex chars, got %d", len(h1))
	}
}