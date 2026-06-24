package capture

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elbader17/gist/pkg/aligner"
)

func TestIsLikelyPrompt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", false},
		{"short", "hi", false},
		{"short line", "ls -la", false},
		{"long text", strings.Repeat("a", MinLongTextLen+10), true},
		{"code fence", "hello world this is a longer prompt\n```go\nfunc f() {}\n```\n", true},
		{"markdown heading", "# Title\n\nSome content here that is long enough to count", true},
		{"json", `{"model":"gpt-4","messages":[{"role":"user","content":"hi there friend"}]}`, true},
		{"plain short", "build the project please", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLikelyPrompt(tt.input); got != tt.want {
				t.Errorf("IsLikelyPrompt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"json", `{"model":"gpt-4"}`, KindJSON},
		{"code", "```python\nprint(1)\n```", KindCodeBlock},
		{"heading", "# Hello world this is a heading with enough text to qualify", KindMarkdown},
		{"long", strings.Repeat("x", MinLongTextLen+5), KindLongText},
		{"short", "ls", KindOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.input); got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLooksLikeJSON(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`{}`, true},
		{`[]`, true},
		{`{"a":1}`, true},
		{`not json`, false},
		{`{broken`, false},
		{``, false},
		{"plain text", false},
	}
	for _, c := range cases {
		if got := LooksLikeJSON(c.in); got != c.want {
			t.Errorf("LooksLikeJSON(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestExtractLayersFromJSON(t *testing.T) {
	in := `{
		"model": "gpt-4",
		"system": "You are a Go expert",
		"messages": [
			{"role": "system", "content": "Be concise"},
			{"role": "user", "content": "fix the bug"}
		],
		"static_files": ["file a", "file b"]
	}`
	sys, static := ExtractLayers(in)
	if len(sys) != 2 {
		t.Errorf("expected 2 system rules, got %d: %v", len(sys), sys)
	}
	if len(static) != 2 {
		t.Errorf("expected 2 static files, got %d: %v", len(static), static)
	}
}

func TestExtractLayersPlainText(t *testing.T) {
	in := "just some plain text without any structure that qualifies as a prompt at all"
	sys, static := ExtractLayers(in)
	if len(sys) != 0 || len(static) != 0 {
		t.Errorf("plain text should yield empty layers, got sys=%v static=%v", sys, static)
	}
}

func TestExtractLayersDedupe(t *testing.T) {
	in := `{"system":"rule","messages":[{"role":"system","content":"rule"}]}`
	sys, _ := ExtractLayers(in)
	if len(sys) != 1 {
		t.Errorf("expected deduped single rule, got %d: %v", len(sys), sys)
	}
}

func TestSessionOpenClose(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Command: "echo", Args: []string{"hi"}, Dir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Record(DirStdin, "hello world")
	if err := s.Close(0); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := splitLines(string(data))
	if len(lines) < 3 {
		t.Fatalf("expected >=3 lines (header, event, summary), got %d", len(lines))
	}

	var header SessionHeader
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header unmarshal: %v", err)
	}
	if header.Command != "echo" {
		t.Errorf("header.Command = %q", header.Command)
	}

	var summary SessionSummary
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &summary); err != nil {
		t.Fatalf("summary unmarshal: %v", err)
	}
	if summary.ExitCode != 0 {
		t.Errorf("summary.ExitCode = %d", summary.ExitCode)
	}
}

func TestSessionDetectsPrompt(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Command: "echo", Args: nil, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s.Record(DirStdin, strings.Repeat("a", MinLongTextLen+50))
	s.Close(0)

	f, err := os.Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var foundPrompt bool
	for scanner.Scan() {
		var evt CaptureEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.Direction == "header" || evt.Direction == "summary" {
			continue
		}
		if evt.IsPrompt {
			foundPrompt = true
			if evt.Aligned == nil {
				t.Error("prompt event missing aligned payload")
			}
			if evt.PromptKind != KindLongText {
				t.Errorf("prompt_kind = %q, want long_text", evt.PromptKind)
			}
		}
	}
	if !foundPrompt {
		t.Error("expected prompt to be detected")
	}
}

func TestSessionByteCounters(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Command: "x", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s.Record(DirStdin, "12345")
	s.Record(DirStdout, "1234567890")
	s.Record(DirStderr, "12")
	if err := s.Close(7); err != nil {
		t.Fatal(err)
	}

	f, _ := os.Open(s.Path())
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var summary SessionSummary
	for scanner.Scan() {
		var raw map[string]interface{}
		_ = json.Unmarshal(scanner.Bytes(), &raw)
		if kind, _ := raw["kind"].(string); kind == "gist.capture.summary/v1" {
			_ = json.Unmarshal(scanner.Bytes(), &summary)
		}
	}
	if summary.BytesStdin != 5 || summary.BytesStdout != 10 || summary.BytesStderr != 2 {
		t.Errorf("byte counters wrong: %+v", summary)
	}
	if summary.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", summary.ExitCode)
	}
}

func TestSessionCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(Options{Command: "x", Dir: dir})
	if err := s.Close(0); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(0); err != nil {
		t.Errorf("second Close should not error: %v", err)
	}
}

func TestSessionAlignedPayloadShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(Options{Command: "x", Dir: dir})
	s.Record(DirStdin, strings.Repeat("z", MinLongTextLen+10))
	s.Close(0)

	f, _ := os.Open(s.Path())
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt CaptureEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.Aligned != nil {
			if len(evt.Aligned.Blocks) != 4 {
				t.Errorf("aligned should have 4 layers, got %d", len(evt.Aligned.Blocks))
			}
		}
	}
}

func TestSessionUsesDefaultDir(t *testing.T) {
	t.Setenv("GIST_CAPTURES_DIR", t.TempDir())
	s, err := Open(Options{Command: "x"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(0)
	dir, _ := CapturesDir()
	if !strings.HasPrefix(s.Path(), dir) {
		t.Errorf("path %s should be under %s", s.Path(), dir)
	}
}

func TestSessionTeeReaderForwards(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(Options{Command: "tee-test", Dir: dir})
	defer s.Close(0)

	input := "hello world\nthis is line two\n"
	var outBuf strings.Builder
	reader := s.TeeReader(strings.NewReader(input), DirStdout, &outBuf)
	data, _ := readAll(reader)
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("TeeReader did not forward data, got %q", string(data))
	}
	if !strings.Contains(outBuf.String(), "this is line two") {
		t.Errorf("TeeReader did not write to dst, got %q", outBuf.String())
	}
}

func TestSessionTeeReaderWithNilDst(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(Options{Command: "nil-dst", Dir: dir})
	defer s.Close(0)

	reader := s.TeeReader(strings.NewReader("a\nb\nc\n"), DirStdout, nil)
	data, _ := readAll(reader)
	if !strings.Contains(string(data), "b") {
		t.Errorf("nil dst should still forward: %q", string(data))
	}
}

func TestPathContainsCommand(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Command: "mycmd", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(0)
	if !strings.HasSuffix(filepath.Base(s.Path()), ".jsonl") {
		t.Errorf("path should end in .jsonl: %s", s.Path())
	}
}

func TestAlignAttachedToPrompt(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(Options{Command: "x", Dir: dir})
	defer s.Close(0)
	prompt := strings.Repeat("p", MinLongTextLen+50)
	s.Record(DirStdin, prompt)

	f, _ := os.Open(s.Path())
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt CaptureEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.IsPrompt && evt.Aligned == nil {
			t.Fatal("prompt without aligned payload")
		}
		if evt.Aligned != nil && evt.Aligned.TotalHint == 0 {
			t.Error("aligned payload has zero total hint")
		}
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 512)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}

// Sanity-check that the aligner import is wired in.
var _ = aligner.Align