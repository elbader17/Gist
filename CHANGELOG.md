# Changelog

All notable changes to Gist will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `pkg/cache`: thread-safe LRU cache keyed by `(path, mtime, size)` that
  reuses pruned file content between calls. Bounded by entry count and
  cumulative byte size.
- `pkg/squeeze`: one-call context optimizer that composes file pruning
  (cached), alignment, and a token-cap enforcer into a single
  ready-to-send prompt. Returns per-section token hints and savings
  metrics.
- `pkg/metrics`: per-session token-savings recorder with a background
  flusher, persisted to `~/.config/gist/metrics.json`.
- `pkg/ast` multi-language pruner: signature-only reduction for Python,
  JavaScript, TypeScript, Rust, Java, C/C++, and Ruby. Structured
  formats (JSON/YAML/TOML/Markdown) pass through unchanged.
- `pkg/aligner` cache-friendly markdown renderer with stable layer
  hashes and a dedup-ratio helper.
- `pkg/budget` debounced flusher: sessions.json is flushed every 2s
  instead of on every `Check()` call. Trips still force an immediate
  flush.
- `pkg/diff` parallel enrich: worker pool (up to 8 goroutines) replaces
  the serial `git diff` per-file loop.
- New MCP tool `squeeze_context` — single call to prune + align + cap.
- New MCP tool `report_savings` — cumulative token-savings telemetry
  across sessions and tools.

### Changed

- `view_file_slim` now consults `pkg/cache` for repeat reads and reports
  a `cache_hit` flag in the response.
- `align_context_cache` returns a markdown `combined` block with stable
  layer markers, suitable for direct provider caching.
- `fetch_diff_context` uses `EnrichParallel` for per-file enrichment.
- `pkg/config` exposes `cache_max_entries` and `cache_max_bytes` for
  tuning the LRU cache.

## [0.2.0] - 2026-06-24

### Added

- `gist wrap -- CMD` subcommand: transparent I/O capture for any command.
- `pkg/capture`: JSONL session writer, prompt detection, aligner integration.
- Capture files at `~/.config/gist/captures/<timestamp>-<pid>.jsonl`.
- New `squeeze_context` and `report_savings` MCP tools.
- New `pkg/cache`, `pkg/squeeze`, `pkg/metrics` packages.

## [0.1.0] - 2026-06-24

### Added

- Initial implementation of Gist MCP server in Go.
- Four MCP tools:
  - `view_file_slim` - AST-based function body pruning for Go source.
  - `enforce_budget` - Cost / token / loop circuit breaker.
  - `align_context_cache` - 4-layer prompt reorder for prompt caching.
  - `fetch_diff_context` - Semantic git diff summary.
- Configuration via `~/.config/gist/config.json`.
- Session persistence at `~/.config/gist/sessions.json`.
- Comprehensive test suite: 7 packages, ~85% coverage.
- Static binary build (`CGO_ENABLED=0`).
- Race-detector clean.
- Documentation: README, ARCHITECTURE, godoc comments.