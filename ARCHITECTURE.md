# Architecture

This document describes the internal design of Gist (TokenLess).

## Module Dependency Graph

```
cmd/tokenless
    |
    +-- pkg/config
    +-- pkg/budget  -----> pkg/config
    +-- pkg/mcp     -----> pkg/aligner
                     +--> pkg/ast
                     +--> pkg/budget
                     +--> pkg/config
                     +--> pkg/diff
    +-- pkg/tokenizer
```

Each `pkg/` module is independently importable. `cmd/tokenless` wires them
together into a single process.

## Data Flow

```
Client (Claude Code, OpenCode, ...)
    |  stdin JSON-RPC
    v
+----------------------+
|  pkg/mcp (Server)    |  parses JSON-RPC, dispatches by method
+----------------------+
    |
    | tools/call {name, args}
    v
+----------------------+
| pkg/mcp (Dispatcher) |  routes to handler by tool name
+----------------------+
    |
    +--> view_file_slim   --> pkg/ast   (read file, AST collapse)
    +--> enforce_budget   --> pkg/budget (counter, loop detector)
    +--> align_context    --> pkg/aligner (sort + hash + reorder)
    +--> fetch_diff       --> pkg/diff   (git subprocess)
    |
    v
ToolCallResult {content: [{type:"text", text:"..."}]}
    |
    v stdout JSON-RPC
Client
```

## Module Details

### pkg/config

Holds runtime configuration: cost limits, pricing, tokenizer encoding.
Persisted as JSON at `~/.config/tokenless/config.json`. Override directory with
`TOKENLESS_CONFIG_DIR`. Exposes `SetConfigDir` / `ResetConfigDir` for tests.

### pkg/tokenizer

Approximate BPE-style token counting using per-encoding byte ratios:

| Encoding     | Ratio (bytes / token) |
|--------------|-----------------------|
| cl100k_base  | 4.0                   |
| o200k_base   | 3.5                   |
| p50k_base    | 4.2                   |

A real BPE implementation would require downloading the vocab files; this
approximation is sufficient for budget estimation. Use `Tokenizer.CountReader`
to stream-count large files without loading them entirely in memory.

### pkg/ast

Two-step file slimming:

1. `BuildSlim(path, focus, maxLines)` opens the file and detects language by
   extension.
2. For Go files, `PruneGoFile` uses `go/parser` to build an AST, walks
   declarations, and replaces each function body (`ast.BlockStmt.List`) with a
   single `ExprStmt` containing the marker string.
3. `printer.Fprint` then re-emits valid Go source with collapsed bodies.

Struct fields tagged `json:"-"` are filtered out of struct types. Interface
method bodies are dropped entirely.

For non-Go files, the file is truncated to `max_lines_body` lines (default 50).

### pkg/budget

Two types: `Store` (persisted sessions) and `Budget` (in-memory policy).

The `Store` holds `Session` entries keyed by `session_id`. Each session tracks:

- `total_tokens`, `total_cost_usd`
- `recent_actions` (ring buffer, default 12 entries)
- `tripped` flag

`Budget.Check(session_id, action, tokens)`:

1. Acquire session lock.
2. Append the action to recent buffer.
3. Run loop detection: count trailing consecutive matches of normalized action.
4. Check cost limit.
5. Check token limit.
6. Persist session atomically (write to `*.tmp` then rename).

If any limit trips, the returned status has `Allowed=false, Tripped=true, Reason=...`
and the dispatcher surfaces that as `isError=true` in the JSON-RPC response.

### pkg/aligner

Reorders prompt components into four layers:

1. `system_rules` - deduped system prompts, joined with `\n\n`.
2. `static_files` - deduped, sorted alphabetically for cache stability.
3. `history` - trimmed history string.
4. `dynamic_input` - trimmed dynamic input string.

Each layer is hashed (sha256 truncated to 8 bytes / 16 hex chars) so the caller
can detect content drift between calls.

`CacheReady` is true when both the system and static layers exceed the
1024-token minimum required by Anthropic / Google prompt caching.

### pkg/diff

Two-phase git diff:

1. `Fetch(opts)` runs `git diff --numstat -M --no-color HEAD` to get a fast
   per-file line-count summary. Output format: `<added>\t<removed>\t<path>`.
2. `Enrich(cwd, base, files)` re-runs `git diff --unified=0 HEAD -- <path>` for
   each file to extract function/type names modified and detect log-only /
   comment-only changes.

Structural lines (package decls, function signatures, braces) are skipped when
detecting log-only / comment-only changes.

### pkg/mcp

JSON-RPC 2.0 over stdio. Implements:

- `initialize` - returns protocol version + server info.
- `ping` - returns `{status: "pong"}`.
- `tools/list` - returns the four tool definitions.
- `tools/call` - dispatches via `Dispatcher.Handle`.
- `notifications/*` - silently ignored (no `id` means no response).

Errors use standard JSON-RPC codes:
- `-32700` parse error
- `-32601` method not found
- `-32602` invalid params
- `-32603` internal error

The server is fully synchronous: one read, one write per request. Concurrent
writes are serialized via `sync.Mutex` on the encoder.

### cmd/tokenless

The `main()` function delegates to `run(args, stdin, stdout, stderr)` for
testability. `run` handles:

- `--version` / `--help` / `config` / `init` subcommands.
- Default behavior: load config, open budget store, start MCP server.

The `init` subcommand writes a default config without starting the server.

## Persistence

Both `config.json` and `sessions.json` are written atomically via temp-file +
rename to prevent corruption on crash.

`configDirOnce` was removed in favor of a per-call check so tests can flip the
directory without leaking state.

## Concurrency

- `pkg/budget.Store` uses a `sync.Mutex` on the sessions map; each `Session`
  embeds its own `sync.Mutex`.
- `pkg/mcp.Server` serializes encoder writes with `sync.Mutex`; reads from
  stdin are inherently sequential.
- `pkg/aligner` is pure-functional, no shared state.
- `pkg/ast` / `pkg/diff` are pure-functional per call.

The concurrent test in `pkg/budget/concurrency_test.go` exercises 20 goroutines
× 50 iterations against a single session to validate race-free accounting.

## Testing Strategy

- **Unit tests**: every package has `*_test.go` next to source.
- **Fixtures**: `pkg/diff/git_test.go` builds real git repos in `t.TempDir()`
  to exercise the full Fetch/Enrich pipeline.
- **Race detector**: `go test -race ./...` runs in CI.
- **Coverage**: target 85%+ overall; the table in the README shows the
  current state.

## Future Work

- Real BPE encoding via on-demand vocab download.
- Tree-sitter integration for non-Go languages.
- Streaming MCP responses for very large diffs.
- Per-session cost breakdown by tool.