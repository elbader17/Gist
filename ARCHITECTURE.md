# Architecture

This document describes the internal design of Gist.

## Module Dependency Graph

```
cmd/gist
    |
    +-- pkg/config
    +-- pkg/budget  -----> pkg/config
    +-- pkg/cache
    +-- pkg/metrics
    +-- pkg/capture
    +-- pkg/mcp     -----> pkg/aligner
                     +--> pkg/ast
                     +--> pkg/budget
                     +--> pkg/cache
                     +--> pkg/config
                     +--> pkg/diff
                     +--> pkg/metrics
                     +--> pkg/squeeze
    +-- pkg/squeeze -----> pkg/aligner
                     +--> pkg/ast
                     +--> pkg/cache
                     +--> pkg/tokenizer
    +-- pkg/tokenizer
```

Each `pkg/` module is independently importable. `cmd/gist` wires them
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
    +--> view_file_slim   --> pkg/cache (lookup) --> pkg/ast   (read file, AST collapse)
    +--> enforce_budget   --> pkg/budget (counter, loop detector, debounced flush)
    +--> align_context    --> pkg/aligner (sort + hash + reorder, markdown render)
    +--> fetch_diff       --> pkg/diff   (git subprocess + parallel enrich)
    +--> squeeze_context  --> pkg/squeeze (compose: cache + ast + aligner + cap)
    +--> report_savings   --> pkg/metrics (per-session telemetry)
    |
    v
ToolCallResult {content: [{type:"text", text:"..."}]}
    |
    v stdout JSON-RPC
Client
```

### pkg/capture

Transparent I/O capture used by the `gist wrap` subcommand. Each session
opens a JSONL file at `<configdir>/captures/<timestamp>-<pid>.jsonl` and
writes:

1. A `SessionHeader` describing the wrapped command and PID.
2. One `CaptureEvent` per chunk of stdin / stdout / stderr.
3. A `SessionSummary` with byte counters, exit code, and prompt count.

`IsLikelyPrompt` (in `detect.go`) flags user input that looks like an LLM
prompt: JSON payloads (OpenAI/Anthropic API requests), markdown code fences,
headings, or any block longer than 400 chars. When flagged, the event is
augmented with an `aligner.AlignedPayload` so the optimized layout is
preserved alongside the original input for later inspection.

## Module Details

### pkg/config

Holds runtime configuration: cost limits, pricing, tokenizer encoding,
cache bounds. Persisted as JSON at `~/.config/gist/config.json`. Override
directory with `GIST_CONFIG_DIR`. Exposes `SetConfigDir` / `ResetConfigDir`
for tests.

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

`TruncateToTokens` accounts for the trailing marker length so the returned
string never exceeds the requested cap.

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

For non-Go files, `PruneNonGo` (in `multi.go`) keeps imports and top-level
declarations as signatures, replacing bodies with `CollapseMarker`. Supports
Python, JavaScript, TypeScript, Rust, Java, C/C++, and Ruby. Handles nested
signatures inside classes by closing the parent body before re-processing.
Structured formats (json/yaml/toml/markdown) pass through unchanged; unknown
extensions fall back to a hard truncation cap.

### pkg/cache

LRU cache for pruned file content. Each entry is keyed by `(path, mtime, size)`
so file updates invalidate automatically. Bounded by both entry count
(default 256) and cumulative byte size (default 64 MB). Provides Stats
(hits/misses) for the metrics pipeline.

### pkg/budget

Two types: `Store` (persisted sessions) and `Budget` (in-memory policy).
The optional `Flusher` debounces saves to one write per `interval` (default
2s); trip conditions force an immediate flush.

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
6. Mark dirty (flusher persists) or force-save on trip.

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

`RenderMarkdown` (in `markdown.go`) concatenates an `AlignedPayload` into a
single cache-friendly markdown document with explicit `<!-- layer:N:name:hash -->`
markers. `DedupRatio` quantifies bytes saved by deduping the static layer.

### pkg/squeeze

The flagship composer. `Squeeze(opts)` returns a `Result` containing:

- Per-section (`system_rules`, `static_files`, `history`, `dynamic_input`)
  content, token hints, and stable hashes.
- `Combined`: plain-text concatenation separated by `---`.
- `Markdown`: cache-friendly markdown with layer markers.
- `TotalTokens` / `OriginalTokens` / `SavedTokens` / `SavedRatio`.
- `CacheHits` / `CacheMisses` for observability.

When `MaxTokens` is set and the total exceeds it, sections are trimmed in
reverse priority order (history → static → system). Each section's
`Truncated` flag is set when content was cut.

File pruning is parallel; results are sorted alphabetically before joining
to maximize provider cache stability. A `*cache.Cache` short-circuits repeat
reads.

### pkg/metrics

Records `(session_id, tool, input_tokens, output_tokens)` observations and
exposes them via:

- `Recorder.Session(id)` — per-session summary.
- `Recorder.Aggregate()` — global totals with a per-tool breakdown.

A background flusher persists the snapshot to `~/.config/gist/metrics.json`
every 2 seconds; `Stop()` performs a final synchronous flush.

### pkg/diff

Two-phase git diff:

1. `Fetch(opts)` runs `git diff --numstat -M --no-color HEAD` to get a fast
   per-file line-count summary. Output format: `<added>\t<removed>\t<path>`.
2. `EnrichParallel(cwd, base, files, maxWorkers)` runs a worker pool (up to 8
   goroutines) re-executing `git diff --unified=0 HEAD -- <path>` for each
   file in parallel, then extracts function/type names and detects
   log-only / comment-only changes.

Structural lines (package decls, function signatures, braces) are skipped when
detecting log-only / comment-only changes.

### pkg/mcp

JSON-RPC 2.0 over stdio. Implements:

- `initialize` - returns protocol version + server info.
- `ping` - returns `{status: "pong"}`.
- `tools/list` - returns the six tool definitions.
- `tools/call` - dispatches via `Dispatcher.Handle`.
- `notifications/*` - silently ignored (no `id` means no response).

Errors use standard JSON-RPC codes:
- `-32700` parse error
- `-32601` method not found
- `-32602` invalid params
- `-32603` internal error

The server is fully synchronous: one read, one write per request. Concurrent
writes are serialized via `sync.Mutex` on the encoder.

### cmd/gist

The `main()` function delegates to `run(args, stdin, stdout, stderr)` for
testability. `run` handles:

- `--version` / `--help` / `config` / `init` subcommands.
- Default behavior: load config, open budget store, start flusher, build
  cache, start metrics recorder, start MCP server.

The `init` subcommand writes a default config without starting the server.

## Persistence

`config.json`, `sessions.json`, and `metrics.json` are written atomically via
temp-file + rename to prevent corruption on crash. The sessions store
flushes every 2 seconds via the debounced flusher (trip conditions force
immediate flush). Metrics flush on the same cadence.

`configDirOnce` was removed in favor of a per-call check so tests can flip the
directory without leaking state.

## Concurrency

- `pkg/budget.Store` uses a `sync.Mutex` on the sessions map; each `Session`
  embeds its own `sync.Mutex`.
- `pkg/budget.Flusher` serializes state via its own mutex; the underlying
  `Store.Save` takes the store mutex.
- `pkg/cache.Cache` is fully thread-safe (`container/list` + map + mutex).
- `pkg/mcp.Server` serializes encoder writes with `sync.Mutex`; reads from
  stdin are inherently sequential.
- `pkg/squeeze` parallelizes per-file pruning with a goroutine per source.
- `pkg/diff.EnrichParallel` uses a worker pool capped at 8 goroutines.
- `pkg/aligner`, `pkg/ast` are pure-functional, no shared state.

The concurrent tests (`pkg/budget/concurrency_test.go`,
`pkg/cache/cache_test.go`) exercise high goroutine counts against a single
shared structure to validate race-free accounting.

## Testing Strategy

- **Unit tests**: every package has `*_test.go` next to source.
- **Fixtures**: `pkg/diff/git_test.go` and `pkg/diff/parallel_test.go` build
  real git repos in `t.TempDir()` to exercise the full Fetch/Enrich pipeline.
- **Race detector**: `go test -race ./...` runs in CI.
- **Coverage**: target 85%+ overall; the table in the README shows the
  current state.

## Future Work

- Real BPE encoding via on-demand vocab download.
- Tree-sitter integration for the long tail of non-Go languages.
- Streaming MCP responses for very large diffs.
- Per-tool cost breakdown in `report_savings`.