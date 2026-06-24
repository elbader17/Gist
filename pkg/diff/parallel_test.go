package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFileIn(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "test")
	return dir
}

func TestEnrichParallelEquivalentToSerial(t *testing.T) {
	dir := initRepo(t)
	writeFileIn(t, dir, "a.go", "package x\nfunc A() int { return 1 }\n")
	writeFileIn(t, dir, "b.go", "package x\n// log only\n// fmt.Println(\"x\")\n")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "init")

	writeFileIn(t, dir, "a.go", "package x\nfunc A() int { return 2 }\nfunc B() int { return 3 }\n")
	writeFileIn(t, dir, "b.go", "package x\n// log only\n// fmt.Println(\"y\")\n// fmt.Println(\"z\")\n")

	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := EnrichParallel(dir, sd.Base, sd.Files, 4); err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if len(sd.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(sd.Files))
	}
	for _, fc := range sd.Files {
		base := filepath.Base(fc.Path)
		switch base {
		case "a.go":
			if fc.LogOnly || fc.CommentOnly {
				t.Fatalf("a.go should not be log/comment only")
			}
			found := false
			for _, fn := range fc.Functions {
				if fn == "A" || fn == "B" {
					found = true
				}
			}
			if !found {
				t.Fatalf("a.go missing A/B in functions: %v", fc.Functions)
			}
		case "b.go":
			if !fc.LogOnly {
				t.Fatalf("b.go should be log-only: %s", fc.Summary)
			}
		}
	}
}

func TestEnrichParallelEmptyFileList(t *testing.T) {
	if err := EnrichParallel(".", "HEAD", nil, 4); err != nil {
		t.Fatalf("nil list: %v", err)
	}
	files := []*FileChange{{Path: ""}}
	if err := EnrichParallel(".", "HEAD", files, 4); err != nil {
		t.Fatalf("empty path: %v", err)
	}
}

func TestEnrichParallelSingleWorker(t *testing.T) {
	dir := initRepo(t)
	writeFileIn(t, dir, "main.go", "package main\nfunc main() {}\n")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "init")
	writeFileIn(t, dir, "main.go", "package main\nfunc main() { println(\"x\") }\n")

	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := EnrichParallel(dir, sd.Base, sd.Files, 1); err != nil {
		t.Fatalf("enrich single: %v", err)
	}
	if len(sd.Files) != 1 || sd.Files[0].Path != "main.go" {
		t.Fatalf("unexpected files: %+v", sd.Files)
	}
	if !strings.Contains(sd.Files[0].Summary, "main") {
		t.Fatalf("summary should mention main: %s", sd.Files[0].Summary)
	}
}