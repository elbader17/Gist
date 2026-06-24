# Changelog

All notable changes to Gist will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `gist wrap -- CMD` subcommand: transparent I/O capture for any command.
- `pkg/capture`: JSONL session writer, prompt detection, aligner integration.
- Capture files at `~/.config/gist/captures/<timestamp>-<pid>.jsonl`.

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