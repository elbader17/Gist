package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
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
	run("init", "-q")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	main := filepath.Join(dir, "main.go")
	if err := os.WriteFile(main, []byte("package main\nfunc main() { println(\"v1\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")

	newMain := "package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"v2\") }\nfunc NewHelper() {}\n"
	if err := os.WriteFile(main, []byte(newMain), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGitIsRepo(t *testing.T) {
	dir := setupGitRepo(t)
	if !GitIsRepo(dir) {
		t.Error("expected isRepo=true")
	}
	if GitIsRepo("/nonexistent") {
		t.Error("expected isRepo=false for missing dir")
	}
}

func TestFetchOnGitRepo(t *testing.T) {
	dir := setupGitRepo(t)
	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(sd.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	fc := sd.Files[0]
	if fc.AddedLines == 0 {
		t.Errorf("expected added > 0, got %d", fc.AddedLines)
	}
	if sd.TotalAdded == 0 {
		t.Error("expected total_added > 0")
	}
}

func TestEnrichExtractsFunctions(t *testing.T) {
	dir := setupGitRepo(t)
	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := Enrich(dir, sd.Base, sd.Files); err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	found := false
	for _, fn := range sd.Files[0].Functions {
		if fn == "NewHelper" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NewHelper in functions, got %v", sd.Files[0].Functions)
	}
	if sd.Files[0].Summary == "" {
		t.Error("summary should be set by enrich")
	}
}

func TestEnrichDetectsLogOnly(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
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
	run("init", "-q")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "t")

	main := filepath.Join(dir, "main.go")
	if err := os.WriteFile(main, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")

	if err := os.WriteFile(main, []byte("package main\nfunc main() {\n\tfmt.Println(\"a\")\n\tfmt.Println(\"b\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := Enrich(dir, sd.Base, sd.Files); err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if !sd.Files[0].LogOnly {
		t.Errorf("expected log_only=true, got %+v", sd.Files[0])
	}
	if !strings.Contains(sd.Files[0].Summary, "log/print") {
		t.Errorf("summary should mention log/print: %q", sd.Files[0].Summary)
	}
}

func TestEnrichCommentOnly(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
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
	run("init", "-q")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "t")

	comments := filepath.Join(dir, "comments.go")
	if err := os.WriteFile(comments, []byte("// initial comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")

	if err := os.WriteFile(comments, []byte("// initial comment\n// new comment\n// another\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := Enrich(dir, sd.Base, sd.Files); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	if len(sd.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	if !sd.Files[0].CommentOnly {
		t.Errorf("expected comment_only=true, got %+v", sd.Files[0])
	}
}

func TestFetchMaxFiles(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
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
	run("init", "-q")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "t")

	for i := 0; i < 5; i++ {
		f := filepath.Join(dir, "f"+string(rune('a'+i))+".go")
		os.WriteFile(f, []byte("package x\n"), 0o644)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")

	for i := 0; i < 5; i++ {
		f := filepath.Join(dir, "f"+string(rune('a'+i))+".go")
		os.WriteFile(f, []byte("package x\n// change\n"), 0o644)
	}

	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD", MaxFiles: 2})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(sd.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(sd.Files))
	}
}

func TestFetchInvalidRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestEnrichSummaryLogOnly(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@t.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "t")

	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	os.WriteFile(a, []byte("package x\n"), 0o644)
	os.WriteFile(b, []byte("package x\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "i")

	os.WriteFile(a, []byte("package x\nfmt.Println(\"a\")\n"), 0o644)
	os.WriteFile(b, []byte("package x\nfmt.Println(\"b\")\n"), 0o644)

	sd, err := Fetch(Options{Cwd: dir, Base: "HEAD"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := Enrich(dir, sd.Base, sd.Files); err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	allLog := true
	for _, f := range sd.Files {
		if !f.LogOnly {
			allLog = false
		}
	}
	if !allLog {
		t.Errorf("expected all files log_only, got %+v", sd.Files)
	}
}