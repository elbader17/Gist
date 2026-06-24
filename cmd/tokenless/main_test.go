package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withFiles(t *testing.T) (*os.File, *os.File, *os.File) {
	t.Helper()
	dir := t.TempDir()
	in, err := os.Create(filepath.Join(dir, "in"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := os.Create(filepath.Join(dir, "err"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.Close()
		out.Close()
		errOut.Close()
	})
	return in, out, errOut
}

func TestRunVersion(t *testing.T) {
	in, out, errOut := withFiles(t)
	err := run([]string{"tokenless", "--version"}, in, out, errOut)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out.Seek(0, 0)
	buf := make([]byte, 1024)
	n, _ := out.Read(buf)
	got := strings.TrimSpace(string(buf[:n]))
	if !strings.Contains(got, "tokenless") || !strings.Contains(got, "0.1.0") {
		t.Errorf("unexpected version output: %q", got)
	}
}

func TestRunHelp(t *testing.T) {
	in, out, errOut := withFiles(t)
	err := run([]string{"tokenless", "--help"}, in, out, errOut)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out.Seek(0, 0)
	buf := make([]byte, 4096)
	n, _ := out.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, "Usage") {
		t.Errorf("help output missing Usage: %q", got)
	}
}

func TestRunConfig(t *testing.T) {
	in, out, errOut := withFiles(t)
	t.Setenv("TOKENLESS_CONFIG_DIR", t.TempDir())
	err := run([]string{"tokenless", "config"}, in, out, errOut)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out.Seek(0, 0)
	buf := make([]byte, 1024)
	n, _ := out.Read(buf)
	got := strings.TrimSpace(string(buf[:n]))
	if !strings.HasSuffix(got, "config.json") {
		t.Errorf("expected path ending in config.json, got %q", got)
	}
}

func TestRunInit(t *testing.T) {
	in, out, errOut := withFiles(t)
	dir := t.TempDir()
	t.Setenv("TOKENLESS_CONFIG_DIR", dir)

	err := run([]string{"tokenless", "init"}, in, out, errOut)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Errorf("expected config.json: %v", err)
	}
}

func TestRunServerEOF(t *testing.T) {
	in, out, errOut := withFiles(t)
	t.Setenv("TOKENLESS_CONFIG_DIR", t.TempDir())
	err := run([]string{"tokenless"}, in, out, errOut)
	if err != nil {
		t.Fatalf("run on empty stdin: %v", err)
	}
}

func TestRunInvalidFlag(t *testing.T) {
	in, out, errOut := withFiles(t)
	t.Setenv("TOKENLESS_CONFIG_DIR", t.TempDir())

	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"unknown/method"}`)
	in.Sync()
	in.Seek(0, 0)

	err := run([]string{"tokenless"}, in, out, errOut)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out.Seek(0, 0)
	buf := make([]byte, 4096)
	n, _ := out.Read(buf)
	if !strings.Contains(string(buf[:n]), "Method not found") {
		t.Errorf("expected method-not-found in output, got %q", string(buf[:n]))
	}
}