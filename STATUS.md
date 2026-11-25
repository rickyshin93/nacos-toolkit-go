# Status

## Doing

- Real Nacos smoke test awaits an authorized address and config reference. The session supplied no `NACOS_*` settings and no local listener was found on port 8848. Run `make test-live` with the test settings.

## Next

## Backlog

## Done

- 2026-11-25 · Published [v0.0.1](https://github.com/rickyshin93/nacos-toolkit-go/releases/tag/v0.0.1) under MIT. Added English and Simplified Chinese READMEs and GitHub Actions for Linux/macOS checks and tagged releases. The [tag workflow](https://github.com/rickyshin93/nacos-toolkit-go/actions/runs/45515900613) passed, a fresh Go module downloaded and tested the version through the public mirror, and [pkg.go.dev](https://pkg.go.dev/github.com/rickyshin93/nacos-toolkit-go@v0.0.1) serves its API docs.

- 2026-11-25 · Implemented local YAML/JSON parsing and discovery, typed template rendering, deep merge, and ordered Nacos HTTP batches with optional login and per-manager caching.
- 2026-11-25 · Pinned Go 1.27.4 and go-yaml v4.0.0. Passed `make check` on macOS arm64 and `go mod verify`. The sibling Rust baseline at `4ab8f42` passed 110 tests with the live test ignored. The Go HTTP tests use local `httptest` servers.

## Known issues

- See README.md for the explicit Go cache API, growing text cycle behavior, and JSON Unicode compatibility boundary.
