package tokenizer

import (
	"strings"
	"testing"
)

func TestCountApprox(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		encoding Encoding
		wantMin  int
		wantMax  int
	}{
		{"empty", "", CL100KBase, 0, 0},
		{"single char", "a", CL100KBase, 1, 1},
		{"cl100k ratio", strings.Repeat("hello world ", 40), CL100KBase, 110, 125},
		{"o200k denser", strings.Repeat("hello world ", 40), O200KBase, 125, 145},
		{"p50k less dense", strings.Repeat("hello world ", 40), P50KBase, 100, 120},
		{"unknown encoding falls back", "hello world", Encoding("unknown"), 2, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountApprox(tt.text, tt.encoding)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CountApprox() = %d, want in [%d, %d]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCountApproxNonEmpty(t *testing.T) {
	got := CountApprox("x", CL100KBase)
	if got < 1 {
		t.Errorf("non-empty input should yield >= 1 token, got %d", got)
	}
}

func TestTokenizerCountString(t *testing.T) {
	tok := New(CL100KBase)
	if got := tok.CountString("hello world"); got <= 0 {
		t.Errorf("expected positive token count, got %d", got)
	}
}

func TestTokenizerCountReader(t *testing.T) {
	tok := New(CL100KBase)
	r := strings.NewReader("line one\nline two\nline three\n")
	got, err := tok.CountReader(r)
	if err != nil {
		t.Fatalf("CountReader error: %v", err)
	}
	if got <= 0 {
		t.Errorf("expected positive token count, got %d", got)
	}
}

func TestTokenizerCountReaderError(t *testing.T) {
	tok := New(CL100KBase)
	r := &errReader{}
	_, err := tok.CountReader(r)
	if err == nil {
		t.Error("expected error from failing reader")
	}
}

func TestCountWords(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"single", "hello", 1},
		{"two words", "hello world", 2},
		{"multi-space", "   hello    world   ", 2},
		{"tabs and newlines", "a\tb\nc", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountWords(tt.text); got != tt.want {
				t.Errorf("CountWords() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEstimateLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"single line", "hello", 1},
		{"two lines", "a\nb", 2},
		{"trailing newline", "a\nb\n", 2},
		{"only newlines", "\n\n\n", 0},
		{"three lines", "a\nb\nc", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateLines(tt.text); got != tt.want {
				t.Errorf("EstimateLines() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTruncateToTokens(t *testing.T) {
	long := strings.Repeat("abcdefgh", 100)

	t.Run("no truncation", func(t *testing.T) {
		got := TruncateToTokens("short", 100, CL100KBase)
		if got != "short" {
			t.Errorf("expected unchanged, got %q", got)
		}
	})

	t.Run("zero max returns original", func(t *testing.T) {
		got := TruncateToTokens(long, 0, CL100KBase)
		if got != long {
			t.Error("zero max should not truncate")
		}
	})

	t.Run("truncation applies", func(t *testing.T) {
		got := TruncateToTokens(long, 10, CL100KBase)
		if len(got) >= len(long) {
			t.Errorf("truncated len=%d should be < original=%d", len(got), len(long))
		}
		if !strings.HasSuffix(got, "[truncated by gist]") {
			t.Error("truncated output missing marker")
		}
	})

	t.Run("negative max returns original", func(t *testing.T) {
		got := TruncateToTokens(long, -5, CL100KBase)
		if got != long {
			t.Error("negative max should not truncate")
		}
	})
}

func TestRatioFor(t *testing.T) {
	cases := []struct {
		enc  Encoding
		want float64
	}{
		{CL100KBase, 4.0},
		{O200KBase, 3.5},
		{P50KBase, 4.2},
		{"weird", 4.0},
	}
	for _, c := range cases {
		if got := ratioFor(c.enc); got != c.want {
			t.Errorf("ratioFor(%q) = %v, want %v", c.enc, got, c.want)
		}
	}
}

type errReader struct{}

func (e *errReader) Read(_ []byte) (int, error) { return 0, errFake }

var errFake = &readerErr{}

type readerErr struct{}

func (e *readerErr) Error() string { return "fake reader error" }