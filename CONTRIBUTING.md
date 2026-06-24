# Contributing

Thanks for your interest in Gist! This document covers how to develop, test,
and submit changes.

## Development Setup

Requirements:

- Go 1.21+ (tested with 1.26)
- git
- GNU make (optional)

```
git clone https://github.com/tokenless/tokenless
cd tokenless
go test ./...
```

## Code Style

- Format with `gofmt` / `goimports` before committing.
- Avoid external dependencies unless absolutely necessary (the spec requires
  zero non-stdlib deps for the runtime binary).
- Keep public APIs documented with godoc-style comments.
- Prefer small, focused PRs.

## Testing

```
make test           # runs go test ./...
make test-race      # runs go test -race ./...
make cover          # coverage report
```

All packages should keep coverage above 70%; the table in README.md shows
current numbers.

When adding a new feature:

1. Add a unit test next to the code (`*_test.go`).
2. If the feature touches external state (filesystem, git), use `t.TempDir()`
   and clean up automatically.
3. If the feature is concurrent, add a `-race` test that exercises the race.

## Commit Messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(budget): add per-tool cost breakdown
fix(mcp): avoid writing response for notifications
docs(readme): add benchmark section
test(diff): add log-only detection edge cases
refactor(config): drop sync.Once caching
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `chore`, `build`,
`ci`.

## Pull Requests

- Branch from `main`.
- Keep commits logically grouped; squash fixup commits.
- Update CHANGELOG.md under "Unreleased".
- CI must pass: build, vet, race tests, coverage.

## Architecture Decisions

See `ARCHITECTURE.md` for the module map and data flow. If your change alters
the architecture, update the document in the same PR.

## Release Process

1. Update CHANGELOG.md: move unreleased entries to a dated version.
2. Tag the release: `git tag -a v0.2.0 -m "Release 0.2.0"`.
3. Push the tag: `git push origin v0.2.0`.
4. (Optional) Cut a GitHub release with the CHANGELOG entry.

## License

By contributing, you agree that your contributions will be licensed under the
MIT License. See `LICENSE`.