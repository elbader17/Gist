# Gist

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://golang.org)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen)]()
[![Coverage](https://img.shields.io/badge/coverage-85%25-brightgreen)]()
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**High-performance Go CLI that acts as an MCP server for LLM context optimization.**

Gist sits between agentic clients (Claude Code, OpenCode, etc.) and the local
file system to **prune**, **restructure**, and **budget** the information sent
to the model. The result: lower token spend, higher cache hit rates, and no
runaway agent loops.

## Table of Contents

- [Why Gist?](#why-gist)
- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [MCP Tools](#mcp-tools)
  - [view_file_slim](#view_file_slim)
  - [enforce_budget](#enforce_budget)
  - [align_context_cache](#align_context_cache)
  - [fetch_diff_context](#fetch_diff_context)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Performance](#performance)
- [Development](#development)
- [Testing](#testing)
- [License](#license)

## Why Gist?

Modern agentic coding tools have a new bottleneck: **unnecessary context
saturation**. Agents tend to:

- Read entire files of thousands of lines.
- Inject massive build logs into prompts.
- Enter infinite retry loops on failing tests.

Gist solves this with four MCP tools that intercept, prune, and budget context
before it leaves your machine.

## Features

- **Zero external dependencies** - Pure Go stdlib.
- **Static binary** - `CGO_ENABLED=0` produces a self-contained ELF/Mach/PE.
- **Prompt-cache aware** - Reorders context into layers optimized for
  Anthropic / Google / OpenAI caching.
- **AST-aware pruning** - Uses `go/parser` + `go/printer` to collapse function
  bodies in Go source files while preserving signatures and types.
- **Semantic git diff** - Replaces raw diffs with `[File] -> [Function] ->
  change` summaries, plus log-only / comment-only detection.
- **Loop circuit breaker** - Halts repeated actions before they burn the budget.

## Installation

### From source

```
go install github.com/tokenless/tokenless/cmd/tokenless@latest
```

### Build locally

```
git clone https://github.com/tokenless/tokenless
cd tokenless
CGO_ENABLED=0 go build -o bin/tokenless ./cmd/tokenless
```

The binary at `bin/tokenless` is fully static and ~5 MB.

## Quick Start

### As an MCP server

Configure your MCP client (Claude Code, OpenCode, etc.) to spawn the binary:

```json
{
  "mcpServers": {
    "gist": {
      "command": "/path/to/bin/tokenless",
      "args": []
    }
  }
}
```

The server speaks JSON-RPC 2.0 over stdio. It registers the four tools
described below.

### Standalone commands

```
tokenless --version   # print version
tokenless --help      # print help
tokenless config      # print resolved config path
tokenless init        # write default config to ~/.config/tokenless/config.json
```

## MCP Tools

### view_file_slim

Read a file returning a syntactically pruned version with function bodies
collapsed for token efficiency.

**Input:**
```json
{
  "file_path": "/abs/path/to/file.go",
  "focus_functions": ["Add"],          // optional: keep these expanded
  "max_lines_body": 0                   // optional: 0 = collapse fully
}
```

**Output:**
```json
{
  "file_path": "/abs/path/to/file.go",
  "language": "go",
  "slim_content": "package main\n\nfunc Add(a, b int) int {\n\t`// ... [Cuerpo colapsado por TokenLess para optimizar contexto] ...`\n}",
  "truncated": false
}
```

For non-Go files, the tool truncates to `max_lines_body` lines (default 50).

### enforce_budget

Circuit breaker that tracks session tokens, cost, and detects repeated actions
to halt runaway loops.

**Input:**
```json
{
  "session_id": "dev-session-1",
  "current_action": "go test ./...",
  "estimated_tokens": 1500
}
```

**Output (allowed):**
```json
{
  "allowed": true,
  "tripped": false,
  "total_tokens": 1500,
  "total_cost_usd": 0.0045,
  "remaining_usd": 1.9955,
  "max_cost_usd": 2.0
}
```

**Output (tripped on loop):**
```json
{
  "allowed": false,
  "tripped": true,
  "reason": "Loop detected: action \"go test ./...\" repeated 3 times (threshold 3)",
  "loop_detected": true,
  "repeated_action": "go test ./...",
  "repeated_count": 3
}
```

Trip conditions:
1. Action repeated `loop_detection_threshold` times consecutively.
2. Cumulative session cost >= `max_session_cost_usd`.
3. Cumulative session tokens >= `max_session_tokens`.

### align_context_cache

Reorder prompt components into cache-friendly layers.

**Input:**
```json
{
  "system_prompts": ["You are a Go expert", "Be concise"],
  "static_files_context": ["file A", "file B"],
  "dynamic_input": "compile error: undefined: foo",
  "history": "user: ...\nassistant: ..."
}
```

**Output:**
```json
{
  "blocks": [
    {"layer": 1, "layer_name": "system_rules", "token_hint": 8},
    {"layer": 2, "layer_name": "static_files", "token_hint": 4},
    {"layer": 3, "layer_name": "history", "token_hint": 12},
    {"layer": 4, "layer_name": "dynamic_input", "token_hint": 8}
  ],
  "cache_ready": false,
  "warnings": ["system_rules block below provider cache threshold"]
}
```

Static files are sorted alphabetically to maximize cache hit consistency.

### fetch_diff_context

Summarize git diff semantically.

**Input:**
```json
{
  "target_branch": "main",
  "base": "HEAD",
  "cwd": ".",
  "max_files": 50
}
```

**Output:**
```json
{
  "target": "main",
  "base": "HEAD",
  "files": [
    {
      "path": "pkg/foo.go",
      "status": "modified",
      "added_lines": 10,
      "removed_lines": 5,
      "functions": ["Bar", "Baz (type)"],
      "summary": "pkg/foo.go: modified Bar, Baz (type)",
      "log_only": false,
      "comment_only": false
    }
  ],
  "summary": "2 files changed, +15 -7 lines",
  "total_added": 15,
  "total_removed": 7
}
```

The tool uses `git --numstat` for accurate per-file counts and re-runs
`git diff --unified=0` per file to extract modified function names.

## Configuration

Config is loaded from `~/.config/tokenless/config.json`. Override the directory
with `TOKENLESS_CONFIG_DIR=<path>`.

```json
{
  "max_session_cost_usd": 2.00,
  "max_session_tokens": 500000,
  "default_tokenizer_encoding": "cl100k_base",
  "loop_detection_threshold": 3,
  "cache_alignment_enabled": true,
  "pricing": {
    "prompt_per_million": 3.00,
    "completion_per_million": 15.00,
    "cached_prompt_per_million": 0.30
  }
}
```

Sessions are persisted at `~/.config/tokenless/sessions.json`.

## Architecture

```
cmd/tokenless/main.go         CLI entrypoint, flag dispatch
pkg/ast/                      Go AST pruning + skeleton generation
pkg/aligner/                  Prompt caching layer reorder
pkg/budget/                   Session tracking + circuit breaker
pkg/config/                   Config load/save
pkg/diff/                     Semantic git diff
pkg/mcp/                      JSON-RPC MCP protocol handler
pkg/tokenizer/                Local token counting
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for a deep dive.

## Performance

The implementation follows three guiding principles from the original spec:

1. **Zero-allocation hot paths** - `sync.Pool` for tokenizer buffers.
2. **Streaming reads** - `bufio.Scanner` with 4 MB max line length.
3. **Static binary** - `CGO_ENABLED=0`, no dynamic linking.

Benchmarking on a 1000-line Go file:

```
view_file_slim          ~3ms     (AST parse + print + collapse)
align_context_cache     <1ms     (string ops + sha256)
enforce_budget          <1ms     (json read/write + counters)
fetch_diff_context      ~150ms   (git subprocess + per-file diffs)
```

## Development

```
make build       # compile
make test        # run tests
make cover       # coverage report
make run         # build + run
```

Or manually:

```
CGO_ENABLED=0 go build -o bin/tokenless ./cmd/tokenless
go test -race ./...
go test -cover ./...
```

## Testing

Tests live next to the code they cover (`*_test.go`). Coverage by package:

| Package              | Coverage |
|----------------------|----------|
| `pkg/aligner`        | 100%     |
| `pkg/diff`           | 92.1%    |
| `pkg/budget`         | 92.0%    |
| `pkg/tokenizer`      | 90.9%    |
| `pkg/ast`            | 89.5%    |
| `pkg/mcp`            | 85.7%    |
| `cmd/tokenless`      | 80.0%    |
| `pkg/config`         | 70.4%    |

Run with race detector:

```
go test -race ./...
```

## License

[MIT](LICENSE)