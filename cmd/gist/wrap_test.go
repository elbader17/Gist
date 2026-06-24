package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elbader17/gist/pkg/capture"
)

func TestRunWrapNoCommand(t *testing.T) {
	var stderr bytes.Buffer
	code := RunWrap("", nil, WrapOptions{Stderr: &stderr})
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "missing command") {
		t.Errorf("stderr should mention missing command: %q", stderr.String())
	}
}

func TestRunWrapBadCommand(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := RunWrap("/nonexistent/binary", []string{"x"}, WrapOptions{
		Dir:    dir,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
	})
	if code == 0 {
		t.Error("expected non-zero exit for missing binary")
	}
}

func TestRunWrapEchoCommand(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader("hello world\n")

	code := RunWrap("cat", nil, WrapOptions{
		Dir:    dir,
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
		Quiet:  true,
	})
	if code != 0 {
		t.Errorf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "hello world") {
		t.Errorf("stdout should contain echoed text, got %q", stdout.String())
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("expected capture file in %s, matches=%v err=%v", dir, matches, err)
	}
}

func TestRunWrapExitCodePropagation(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := RunWrap("sh", []string{"-c", "exit 42"}, WrapOptions{
		Dir:    dir,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
		Quiet:  true,
	})
	if code != 42 {
		t.Errorf("expected exit 42, got %d", code)
	}
}

func TestRunWrapDetectsPrompt(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	prompt := strings.Repeat("p", capture.MinLongTextLen+50) + "\n"
	stdin := strings.NewReader(prompt)

	code := RunWrap("cat", nil, WrapOptions{
		Dir:    dir,
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
		Quiet:  true,
	})
	if code != 0 {
		t.Fatalf("run failed: exit=%d stderr=%q", code, stderr.String())
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("no capture file")
	}

	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	promptFound := false
	dec := json.NewDecoder(f)
	for {
		var raw map[string]interface{}
		if err := dec.Decode(&raw); err != nil {
			break
		}
		if isPrompt, _ := raw["is_prompt"].(bool); isPrompt {
			promptFound = true
			if _, ok := raw["aligned"]; !ok {
				t.Error("prompt event missing aligned payload")
			}
		}
	}
	if !promptFound {
		t.Error("expected at least one prompt event in capture")
	}
}

func TestRunWrapQuietSuppressesInfo(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := RunWrap("echo", []string{"hi"}, WrapOptions{
		Dir:    dir,
		Stdout: io.Discard,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
		Quiet:  true,
	})
	if code != 0 {
		t.Errorf("exit code = %d", code)
	}
	if strings.Contains(stderr.String(), "capturing session") {
		t.Errorf("quiet mode should suppress info messages, got %q", stderr.String())
	}
}

func TestRunWrapNotQuietPrintsInfo(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := RunWrap("echo", []string{"hi"}, WrapOptions{
		Dir:    dir,
		Stdout: io.Discard,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
		Quiet:  false,
	})
	if code != 0 {
		t.Errorf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "capturing session") {
		t.Errorf("non-quiet mode should print info, got %q", stderr.String())
	}
}

func TestRunWrapStderrStream(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := RunWrap("sh", []string{"-c", "echo error >&2"}, WrapOptions{
		Dir:    dir,
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
		Quiet:  true,
	})
	if code != 0 {
		t.Errorf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "error") {
		t.Errorf("expected stderr stream, got %q", stderr.String())
	}
}