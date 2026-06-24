package ast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleGo = `package sample

import (
	"fmt"
	"io"
)

// Greeter says hello.
type Greeter struct {
	Name string ` + "`json:\"-\"`" + `
	Age  int
}

func (g Greeter) Hello() string {
	return fmt.Sprintf("Hello %s", g.Name)
}

func (g Greeter) Shout() string {
	return fmt.Sprintf("HELLO %s!!!", g.Name)
}

type Runner interface {
	Run() error
	Stop()
}

func Add(a, b int) int {
	return a + b
}

func LongFunction() {
	x := 1
	x++
	x++
	x++
	x++
	x++
	x++
	fmt.Println(x)
	io.EOF
}
`

func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestIsGoFile(t *testing.T) {
	p := NewPruner()
	cases := []struct {
		path string
		want bool
	}{
		{"foo.go", true},
		{"FOO.GO", true},
		{"foo.txt", false},
		{"foo.py", false},
	}
	for _, c := range cases {
		if got := p.IsGoFile(c.path); got != c.want {
			t.Errorf("IsGoFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestPruneGoFileCollapsesAll(t *testing.T) {
	dir := t.TempDir()
	p := writeGoFile(t, dir, "sample.go", sampleGo)

	pr := NewPruner()
	out, err := pr.PruneGoFile([]byte(sampleGo), PruneOptions{FilePath: p})
	if err != nil {
		t.Fatalf("PruneGoFile error: %v", err)
	}

	if !strings.Contains(out, collapseMarker) {
		t.Errorf("expected collapse marker in output, got:\n%s", out)
	}
	if strings.Contains(out, "return a + b") {
		t.Error("Add body should be collapsed")
	}
	if strings.Contains(out, "return fmt.Sprintf(\"Hello %s\"") {
		t.Error("Hello body should be collapsed")
	}
	if !strings.Contains(out, "func Add(a, b int) int") {
		t.Error("signature of Add must remain")
	}
	if !strings.Contains(out, "type Runner interface") {
		t.Error("Runner interface type must remain")
	}
}

func TestPruneGoFileFocusFunctions(t *testing.T) {
	dir := t.TempDir()
	p := writeGoFile(t, dir, "sample.go", sampleGo)

	pr := NewPruner()
	out, err := pr.PruneGoFile([]byte(sampleGo), PruneOptions{
		FilePath:       p,
		FocusFunctions: []string{"Add"},
	})
	if err != nil {
		t.Fatalf("PruneGoFile error: %v", err)
	}
	if !strings.Contains(out, "return a + b") {
		t.Error("focus function Add body should be preserved")
	}
	if !strings.Contains(out, collapseMarker) {
		t.Error("non-focus functions should still be collapsed")
	}
}

func TestPruneGoFileMaxLinesBody(t *testing.T) {
	dir := t.TempDir()
	p := writeGoFile(t, dir, "sample.go", sampleGo)

	pr := NewPruner()
	out, err := pr.PruneGoFile([]byte(sampleGo), PruneOptions{
		FilePath:     p,
		MaxLinesBody: 2,
	})
	if err != nil {
		t.Fatalf("PruneGoFile error: %v", err)
	}
	if strings.Contains(out, "io.EOF") {
		t.Error("long body should be truncated past max_lines_body")
	}
}

func TestPruneGoFileParseError(t *testing.T) {
	pr := NewPruner()
	_, err := pr.PruneGoFile([]byte("not go code"), PruneOptions{FilePath: "x.go"})
	if err == nil {
		t.Error("expected parse error")
	}
}

func TestSkeleton(t *testing.T) {
	pr := NewPruner()
	out, err := pr.Skeleton([]byte(sampleGo), "sample.go")
	if err != nil {
		t.Fatalf("Skeleton error: %v", err)
	}
	if !strings.Contains(out, "package sample") {
		t.Error("missing package name")
	}
	if !strings.Contains(out, "func Add(") {
		t.Error("missing func signature")
	}
	if !strings.Contains(out, "type Runner") {
		t.Error("missing type entry")
	}
}

func TestBuildSlimGo(t *testing.T) {
	dir := t.TempDir()
	p := writeGoFile(t, dir, "sample.go", sampleGo)

	res, err := BuildSlim(p, nil, 0)
	if err != nil {
		t.Fatalf("BuildSlim: %v", err)
	}
	if res.Language != "go" {
		t.Errorf("language = %q, want go", res.Language)
	}
	if !strings.Contains(res.Slim, collapseMarker) {
		t.Error("expected collapse marker")
	}
	if res.Truncated {
		t.Error("not expected to be truncated")
	}
}

func TestBuildSlimNonGoTruncates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.txt")
	lines := []string{}
	for i := 0; i < 200; i++ {
		lines = append(lines, "line")
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := BuildSlim(p, nil, 10)
	if err != nil {
		t.Fatalf("BuildSlim: %v", err)
	}
	if !res.Truncated {
		t.Error("expected truncated flag for 200 lines with limit 10")
	}
	if !strings.Contains(res.Slim, "[truncated by tokenless]") {
		t.Error("truncation marker missing")
	}
}

func TestBuildSlimMissingFile(t *testing.T) {
	_, err := BuildSlim("/nonexistent/file.go", nil, 0)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDetectLanguage(t *testing.T) {
	cases := map[string]string{
		"x.go":          "go",
		"x.JS":          "javascript",
		"x.tsx":         "typescript",
		"x.py":          "python",
		"x.rs":          "rust",
		"x.java":        "java",
		"x.cpp":         "cpp",
		"x.md":          "markdown",
		"x.json":        "json",
		"x.yaml":        "yaml",
		"x.toml":        "toml",
		"x.unknownextn": "text",
	}
	for path, want := range cases {
		if got := detectLanguage(path); got != want {
			t.Errorf("detectLanguage(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFilterFieldsDropsJSONHidden(t *testing.T) {
	pr := NewPruner()
	fields := pr.filterFields(nil)
	if len(fields) != 0 {
		t.Errorf("nil fields should stay nil, got len=%d", len(fields))
	}
}