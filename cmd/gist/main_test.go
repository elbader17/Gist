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
	code := run([]string{"gist", "--version"}, in, out, errOut)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	out.Seek(0, 0)
	buf := make([]byte, 1024)
	n, _ := out.Read(buf)
	got := strings.TrimSpace(string(buf[:n]))
	if !strings.Contains(got, "gist") || !strings.Contains(got, "0.1.0") {
		t.Errorf("unexpected version output: %q", got)
	}
}

func TestRunHelp(t *testing.T) {
	in, out, errOut := withFiles(t)
	code := run([]string{"gist", "--help"}, in, out, errOut)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	out.Seek(0, 0)
	buf := make([]byte, 4096)
	n, _ := out.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, "Usage") {
		t.Errorf("help output missing Usage: %q", got)
	}
	if !strings.Contains(got, "gist wrap") {
		t.Errorf("help output should mention wrap: %q", got)
	}
}

func TestRunConfig(t *testing.T) {
	in, out, errOut := withFiles(t)
	t.Setenv("GIST_CONFIG_DIR", t.TempDir())
	code := run([]string{"gist", "config"}, in, out, errOut)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
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
	t.Setenv("GIST_CONFIG_DIR", dir)

	code := run([]string{"gist", "init"}, in, out, errOut)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Errorf("expected config.json: %v", err)
	}
}

func TestRunServerEOF(t *testing.T) {
	in, out, errOut := withFiles(t)
	t.Setenv("GIST_CONFIG_DIR", t.TempDir())
	code := run([]string{"gist"}, in, out, errOut)
	if code != 0 {
		t.Fatalf("exit code on empty stdin = %d", code)
	}
}

func TestRunInvalidFlag(t *testing.T) {
	in, out, errOut := withFiles(t)
	t.Setenv("GIST_CONFIG_DIR", t.TempDir())

	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"unknown/method"}`)
	in.Sync()
	in.Seek(0, 0)

	code := run([]string{"gist"}, in, out, errOut)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	out.Seek(0, 0)
	buf := make([]byte, 4096)
	n, _ := out.Read(buf)
	if !strings.Contains(string(buf[:n]), "Method not found") {
		t.Errorf("expected method-not-found in output, got %q", string(buf[:n]))
	}
}

func TestParseWrapArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantCmd string
		wantArg string
		wantDir string
		wantQ   bool
		wantErr bool
	}{
		{"basic", []string{"--", "echo", "hi"}, "echo", "hi", "", false, false},
		{"no dash", []string{"echo", "hi"}, "echo", "hi", "", false, false},
		{"quiet", []string{"--quiet", "--", "echo"}, "echo", "", "", true, false},
		{"dir", []string{"--dir", "/tmp/x", "--", "echo"}, "echo", "", "/tmp/x", false, false},
		{"dir missing arg", []string{"--dir"}, "", "", "", false, true},
		{"unknown flag", []string{"--foo"}, "", "", "", false, true},
		{"no command", []string{"--"}, "", "", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, opts, err := parseWrapArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(args) > 0 && args[0] != tt.wantArg {
				t.Errorf("args[0] = %q, want %q", args[0], tt.wantArg)
			}
			if opts.Dir != tt.wantDir {
				t.Errorf("opts.Dir = %q, want %q", opts.Dir, tt.wantDir)
			}
			if opts.Quiet != tt.wantQ {
				t.Errorf("opts.Quiet = %v, want %v", opts.Quiet, tt.wantQ)
			}
		})
	}
}