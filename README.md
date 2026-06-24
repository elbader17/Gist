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
  - [squeeze_context](#squeeze_context)
  - [report_savings](#report_savings)
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
- Re-send the same files repeatedly across turns.

Gist solves all four with **six** MCP tools that intercept, prune, dedup,
cache, and budget context before it leaves your machine.

## Features

- **Zero external dependencies** - Pure Go stdlib.
- **Static binary** - `CGO_ENABLED=0` produces a self-contained ELF/Mach/PE.
- **Prompt-cache aware** - Reorders context into layers optimized for
  Anthropic / Google / OpenAI caching, with stable per-layer hashes.
- **AST-aware pruning** - Uses `go/parser` + `go/printer` to collapse function
  bodies in Go source files while preserving signatures and types.
- **Multi-language pruning** - Signature-only reduction for Python,
  JavaScript, TypeScript, Rust, Java, C/C++, and Ruby.
- **LRU pruning cache** - Skip re-parsing recently read files. Keyed by
  `(path, mtime, size)` so updates invalidate automatically.
- **One-call optimizer** - `squeeze_context` prunes, aligns, and enforces
  a hard token cap, returning a single ready-to-send prompt plus savings
  metrics.
- **Token-savings telemetry** - `report_savings` exposes cumulative
  per-session and per-tool savings, persisted across restarts.
- **Semantic git diff** - Replaces raw diffs with `[File] -> [Function] ->
  change` summaries, with parallel enrichment for speed.
- **Loop circuit breaker** - Halts repeated actions before they burn the
  budget. Session writes are debounced to avoid disk I/O on every call.

## Installation

### From source

```
go install github.com/elbader17/gist/cmd/gist@latest
```

### Build locally

```
git clone https://github.com/elbader17/gist
cd gist
CGO_ENABLED=0 go build -o bin/gist ./cmd/gist
```

The binary at `bin/gist` is fully static and ~5 MB.

## Quick Start

### As an MCP server

Configure your MCP client (Claude Code, OpenCode, etc.) to spawn the binary:

```json
{
  "mcpServers": {
    "gist": {
      "command": "/path/to/bin/gist",
      "args": []
    }
  }
}
```

The server speaks JSON-RPC 2.0 over stdio. It registers the six tools
described below.

### Wrap any command (capture by default)

```
gist wrap -- claude
gist wrap -- aider
gist wrap --your-favorite-llm-cli
```

Every byte of stdin / stdout / stderr flowing through the wrapped command is
captured to `~/.config/gist/captures/<timestamp>-<pid>.jsonl`. When the user
input looks like a prompt (JSON, code block, long markdown), Gist runs the
aligner on it and attaches the optimized payload to the recorded event.

The wrap is **always on** by default — no agent opt-in required. Add a shell
alias to make it permanent:

```sh
alias claude='gist wrap -- claude'
alias aider='gist wrap -- aider'
```

Flags:

| Flag         | Description                              |
|--------------|------------------------------------------|
| `--dir PATH` | Override the capture directory           |
| `--quiet`    | Suppress informational messages on stderr|

Override the capture directory via `GIST_CAPTURES_DIR`.

### Standalone commands

```
gist --version   # print version
gist --help      # print help
gist config      # print resolved config path
gist init        # write default config to ~/.config/gist/config.json
gist wrap -- CMD [args...]   # capture I/O of CMD
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
  "slim_content": "package main\n\nfunc Add(a, b int) int {\n\t`// ... [Cuerpo colapsado por Gist para optimizar contexto] ...`\n}",
  "truncated": false,
  "cache_hit": false
}
```

For non-Go files the tool runs `pkg/ast.PruneNonGo`: signature-only
reduction for Python, JavaScript, TypeScript, Rust, Java, C/C++, and Ruby;
structured formats (JSON/YAML/TOML/Markdown) pass through unchanged; unknown
extensions fall back to a hard truncation at `max_lines_body` lines.

Repeated reads of the same file are served from an LRU cache; the response
includes `"cache_hit": true` on hit.

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
Enrichment runs in parallel via a worker pool (capped at 8 goroutines).

### squeeze_context

One-call optimizer: prunes a list of files (via the LRU cache), aligns them
into cache-friendly layers, enforces an optional hard token cap, and returns
a single ready-to-send prompt plus savings metrics.

**Input:**
```json
{
  "session_id": "dev-session-1",
  "system_prompts": ["You are a Go expert", "Be concise"],
  "static_files": [
    {"path": "/abs/path/to/a.go"},
    {"path": "/abs/path/to/b.go", "focus_functions": ["Important"]}
  ],
  "history": "user: ...\nassistant: ...",
  "dynamic_input": "compile error: undefined: foo",
  "max_tokens": 8000,
  "encoding": "cl100k_base"
}
```

**Output:**
```json
{
  "sections": [
    {"layer": 1, "name": "system_rules",  "tokens": 32, "hash": "..."},
    {"layer": 2, "name": "static_files",  "tokens": 1840, "hash": "..."},
    {"layer": 3, "name": "history",       "tokens": 200, "hash": "..."},
    {"layer": 4, "name": "dynamic_input", "tokens": 18,  "hash": "..."}
  ],
  "combined": "system_rules...\n\n---\n\nstatic_files...",
  "markdown": "<!-- layer:1:system_rules:... -->\n...\n<!-- layer:4:dynamic_input:... -->\n...",
  "total_tokens": 2090,
  "max_tokens": 8000,
  "truncated": false,
  "original_tokens": 12500,
  "saved_tokens": 10410,
  "saved_ratio": 0.833,
  "cache_hits": 1,
  "cache_misses": 1,
  "cache_ready": true,
  "warnings": []
}
```

When `max_tokens` is set and the total exceeds it, sections are trimmed in
reverse priority order (history → static → system). The `markdown` field is
the recommended payload: identical content produces byte-identical output
so provider-side prompt caches can reuse the prefix across calls.

### report_savings

Return cumulative token-savings telemetry.

**Input:**
```json
{
  "session_id": "dev-session-1"   // optional: omit for global aggregate
}
```

**Output (aggregate):**
```json
{
  "sessions": 3,
  "call_count": 47,
  "input_tokens": 312000,
  "output_tokens": 64200,
  "saved_tokens": 247800,
  "saved_ratio": 0.794,
  "by_tool": {
    "view_file_slim":   {"call_count": 22, "input_tokens": 48000,  "output_tokens": 9600, "saved_tokens": 38400},
    "squeeze_context":  {"call_count": 12, "input_tokens": 248000, "output_tokens": 48000, "saved_tokens": 200000},
    "align_context_cache": {"call_count": 13, "input_tokens": 16000, "output_tokens": 6600, "saved_tokens": 9400}
  }
}
```

Telemetry is persisted to `~/.config/gist/metrics.json` via a 2-second
debounced flusher.

## Configuration

Config is loaded from `~/.config/gist/config.json`. Override the directory
with `GIST_CONFIG_DIR=<path>`.

```json
{
  "max_session_cost_usd": 2.00,
  "max_session_tokens": 500000,
  "default_tokenizer_encoding": "cl100k_base",
  "loop_detection_threshold": 3,
  "cache_alignment_enabled": true,
  "cache_max_entries": 256,
  "cache_max_bytes": 67108864,
  "pricing": {
    "prompt_per_million": 3.00,
    "completion_per_million": 15.00,
    "cached_prompt_per_million": 0.30
  }
}
```

Sessions are persisted at `~/.config/gist/sessions.json`; savings telemetry at
`~/.config/gist/metrics.json`. Both are written atomically via temp-file +
rename and flushed on a 2-second debounce (trip conditions force an immediate
flush).

## Architecture

```
cmd/gist/main.go              CLI entrypoint, flag dispatch, wrap subcommand
pkg/ast/                      Go AST pruning + multi-language signature pruner
pkg/aligner/                  Prompt caching layer reorder + markdown render
pkg/budget/                   Session tracking + circuit breaker + debounced flush
pkg/cache/                    LRU cache for pruned file content
pkg/capture/                  Transparent I/O capture + prompt detection
pkg/config/                   Config load/save
pkg/diff/                     Semantic git diff (parallel enrichment)
pkg/mcp/                      JSON-RPC MCP protocol
pkg/metrics/                  Token-savings telemetry
pkg/squeeze/                  One-call context optimizer
pkg/tokenizer/                Local token counting
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for a deep dive.

## Performance

The implementation follows four guiding principles:

1. **Zero-allocation hot paths** - `sync.Pool` for tokenizer buffers.
2. **Streaming reads** - `bufio.Scanner` with 4 MB max line length.
3. **Static binary** - `CGO_ENABLED=0`, no dynamic linking.
4. **Cache-first** - the LRU cache skips re-parsing on repeat reads; the
   budget store flushes asynchronously instead of on every call.

Benchmarking on a 1000-line Go file:

```
view_file_slim (cold)    ~3ms     (AST parse + print + collapse)
view_file_slim (warm)    <1ms     (cache hit, no parse)
align_context_cache      <1ms     (string ops + sha256)
enforce_budget           <1ms     (in-memory counters; flush is debounced)
fetch_diff_context       ~80ms    (git subprocess + parallel per-file enrich, ~2x faster)
squeeze_context          ~4ms     (parallel prune + align + cap, single result)
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
CGO_ENABLED=0 go build -o bin/gist ./cmd/gist
go test -race ./...
go test -cover ./...
```

## Testing

Tests live next to the code they cover (`*_test.go`). Coverage by package:

| Package              | Coverage |
|----------------------|----------|
| `pkg/aligner`        | 98.5%    |
| `pkg/squeeze`        | 94.0%    |
| `pkg/metrics`        | 92.3%    |
| `pkg/diff`           | 91.0%    |
| `pkg/budget`         | 90.5%    |
| `pkg/tokenizer`      | 89.7%    |
| `pkg/cache`          | 88.9%    |
| `pkg/ast`            | 84.8%    |
| `pkg/mcp`            | 85.9%    |
| `pkg/capture`        | 75.4%    |
| `cmd/gist`           | 71.6%    |
| `pkg/config`         | 65.0%    |

Run with race detector:

```
go test -race ./...
```

## License

[MIT](LICENSE)