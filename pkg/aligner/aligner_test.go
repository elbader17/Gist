package aligner

import (
	"strings"
	"testing"
)

func TestAlignBasic(t *testing.T) {
	system := []string{"You are a Go expert", "Be concise"}
	static := []string{"file a contents", "file b contents"}
	dynamic := "compile error: undefined: foo"
	history := "user: hi\nassistant: hello"

	p := Align(system, static, dynamic, history)
	if len(p.Blocks) != 4 {
		t.Fatalf("expected 4 layers, got %d", len(p.Blocks))
	}

	wantLayers := []int{LayerSystemRules, LayerStaticFiles, LayerHistory, LayerDynamicInput}
	for i, b := range p.Blocks {
		if b.Layer != wantLayers[i] {
			t.Errorf("block %d layer = %d, want %d", i, b.Layer, wantLayers[i])
		}
	}

	if p.Blocks[0].Content != "You are a Go expert\n\nBe concise" {
		t.Errorf("system layer unexpected: %q", p.Blocks[0].Content)
	}
	if !strings.Contains(p.Blocks[1].Content, "file a contents") || !strings.Contains(p.Blocks[1].Content, "file b contents") {
		t.Error("static layer missing files")
	}
	if p.Blocks[2].Content != history {
		t.Errorf("history layer mismatch: %q", p.Blocks[2].Content)
	}
	if p.Blocks[3].Content != dynamic {
		t.Errorf("dynamic layer mismatch: %q", p.Blocks[3].Content)
	}
}

func TestAlignStaticSorted(t *testing.T) {
	static := []string{"z.txt", "a.txt", "m.txt"}
	p := Align(nil, static, "", "")
	content := p.Blocks[1].Content
	idxA := strings.Index(content, "a.txt")
	idxM := strings.Index(content, "m.txt")
	idxZ := strings.Index(content, "z.txt")
	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("static layer not sorted alphabetically: %q", content)
	}
}

func TestAlignStaticDedupe(t *testing.T) {
	static := []string{"a", "a", "b"}
	p := Align(nil, static, "", "")
	if strings.Count(p.Blocks[1].Content, "a") != 1 {
		t.Errorf("duplicates not removed: %q", p.Blocks[1].Content)
	}
}

func TestAlignSystemDedupe(t *testing.T) {
	system := []string{"rule", "rule", "rule2"}
	p := Align(system, nil, "", "")
	if strings.Count(p.Blocks[0].Content, "rule\n\nrule2") == 0 &&
		!strings.Contains(p.Blocks[0].Content, "rule2") {
		t.Errorf("expected dedupe but got %q", p.Blocks[0].Content)
	}
}

func TestAlignCacheReady(t *testing.T) {
	big := strings.Repeat("X ", 3000)
	p := Align([]string{big}, []string{big}, "dynamic", "")
	if !p.CacheReady {
		t.Error("expected cache_ready=true with large blocks")
	}
	if len(p.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", p.Warnings)
	}
}

func TestAlignCacheNotReadyWarns(t *testing.T) {
	p := Align([]string{"tiny"}, []string{"small"}, "dyn", "")
	if p.CacheReady {
		t.Error("expected cache_ready=false with small blocks")
	}
	if len(p.Warnings) < 2 {
		t.Errorf("expected >=2 warnings, got %d", len(p.Warnings))
	}
}

func TestAlignCombinedOrder(t *testing.T) {
	p := Align([]string{"SYS"}, []string{"STATIC"}, "DYN", "HIST")
	if !strings.HasPrefix(p.Combined, "SYS") {
		t.Errorf("combined should start with system layer: %q", p.Combined)
	}
	if !strings.HasSuffix(p.Combined, "DYN") {
		t.Errorf("combined should end with dynamic layer: %q", p.Combined)
	}
	if !strings.Contains(p.Combined, "STATIC") {
		t.Error("combined missing static")
	}
	if !strings.Contains(p.Combined, "HIST") {
		t.Error("combined missing history")
	}
}

func TestJoinUniqueSkipsEmpty(t *testing.T) {
	got := joinUnique([]string{"", "a", "  ", "a", "b"})
	want := "a\n\nb"
	if got != want {
		t.Errorf("joinUnique = %q, want %q", got, want)
	}
}

func TestJoinAndSortSkipsEmpty(t *testing.T) {
	got := joinAndSort([]string{"z", "", "a", "a", "m"})
	want := "a\n\nm\n\nz"
	if got != want {
		t.Errorf("joinAndSort = %q, want %q", got, want)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
	}
	for _, c := range cases {
		if got := estimateTokens(c.s); got != c.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestHashBlockStable(t *testing.T) {
	h1 := hashBlock("hello")
	h2 := hashBlock("hello")
	if h1 != h2 {
		t.Error("hash must be deterministic")
	}
	if h1 == hashBlock("world") {
		t.Error("different inputs must produce different hashes")
	}
	if len(h1) != 16 {
		t.Errorf("expected 16-char hex hash, got %d", len(h1))
	}
}

func TestAlignBlockHashes(t *testing.T) {
	p := Align([]string{"sys1"}, []string{"static1"}, "dyn1", "hist1")
	for i, b := range p.Blocks {
		if b.StableHash == "" {
			t.Errorf("block %d missing stable hash", i)
		}
	}
}

func TestAlignTotalHint(t *testing.T) {
	p := Align([]string{"hello"}, []string{"world"}, "!", "?")
	want := estimateTokens("hello") + estimateTokens("world") + estimateTokens("!") + estimateTokens("?")
	if p.TotalHint != want {
		t.Errorf("TotalHint = %d, want %d", p.TotalHint, want)
	}
}