package ast

import (
	"strings"
	"testing"
)

func TestPrunePython(t *testing.T) {
	src := `import os
import sys

def foo(x):
    return x + 1

class Bar:
    def __init__(self):
        self.x = 0

    def baz(self, y):
        if y:
            return y * 2
        return 0
`
	out := PruneNonGo(src, "python", 0)
	for _, want := range []string{"import os", "import sys", "def foo(x)", "class Bar", "def __init__(self)", "def baz(self, y)", CollapseMarker} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	for _, mustNot := range []string{"return x + 1", "self.x = 0", "return y * 2"} {
		if strings.Contains(out, mustNot) {
			t.Fatalf("body leaked: %q in:\n%s", mustNot, out)
		}
	}
}

func TestPruneJavaScript(t *testing.T) {
	src := `import { foo } from 'bar';

export function add(a, b) {
  return a + b;
}

export class Counter {
  constructor() {
    this.n = 0;
  }
  inc() {
    this.n++;
  }
}

const x = 42;
`
	out := PruneNonGo(src, "javascript", 0)
	for _, want := range []string{"import { foo }", "export function add(a, b)", "export class Counter", "const x = 42"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	for _, mustNot := range []string{"return a + b;", "this.n = 0;", "this.n++;"} {
		if strings.Contains(out, mustNot) {
			t.Fatalf("body leaked: %q in:\n%s", mustNot, out)
		}
	}
}

func TestPruneRust(t *testing.T) {
	src := `use std::io;

pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

pub struct Point {
    pub x: f64,
    pub y: f64,
}
`
	out := PruneNonGo(src, "rust", 0)
	for _, want := range []string{"use std::io;", "pub fn add", "pub struct Point"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a + b") {
		t.Fatalf("body leaked in:\n%s", out)
	}
}

func TestPruneJava(t *testing.T) {
	src := `package com.example;

public class Greeter {
    public String greet(String name) {
        return "hello " + name;
    }
}
`
	out := PruneNonGo(src, "java", 0)
	for _, want := range []string{"package com.example;", "public class Greeter", "public String greet(String name)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"hello " + name`) {
		t.Fatalf("body leaked in:\n%s", out)
	}
}

func TestPruneC(t *testing.T) {
	src := `#include <stdio.h>

int add(int a, int b) {
    return a + b;
}

int main(int argc, char** argv) {
    printf("hi\n");
    return 0;
}
`
	out := PruneNonGo(src, "c", 0)
	for _, want := range []string{"#include <stdio.h>", "int add(int a, int b)", "int main(int argc, char** argv)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "return a + b;") || strings.Contains(out, `printf("hi\n");`) {
		t.Fatalf("body leaked in:\n%s", out)
	}
}

func TestPruneStructuredPassthrough(t *testing.T) {
	src := `{"a": 1, "b": 2}`
	for _, lang := range []string{"json", "yaml", "toml", "markdown"} {
		out := PruneNonGo(src, lang, 0)
		if out != src {
			t.Fatalf("language %q should pass through unchanged; got %q", lang, out)
		}
	}
}

func TestPruneUnknownFallsBackToTruncate(t *testing.T) {
	src := strings.Repeat("line\n", 200)
	out := PruneNonGo(src, "weirdlang", 10)
	if !strings.Contains(out, "// ... [truncated by gist]") {
		t.Fatalf("expected truncation marker, got:\n%s", out)
	}
	if strings.Count(out, "\n") > 12 {
		t.Fatalf("expected ~10 lines + marker, got %d lines", strings.Count(out, "\n"))
	}
}

func TestPruneEmpty(t *testing.T) {
	if out := PruneNonGo("", "python", 0); out != "" {
		t.Fatalf("empty input should return empty, got %q", out)
	}
}

func TestPruneGoPassthrough(t *testing.T) {
	src := "package main\n"
	if out := PruneNonGo(src, "go", 0); out != src {
		t.Fatalf("go language should pass through unchanged")
	}
}