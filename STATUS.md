# Status

## Doing

- Publish the first public GitHub tag and verify hosted CI, the release workflow, and Go module download.

- Real Nacos smoke test awaits an authorized address and config reference. The session supplied no `NACOS_*` settings and no local listener was found on port 8848. Run `make test-live` with the test settings.

## Next

## Backlog

## Done

- 2026-11-25 · Implemented local YAML/JSON parsing and discovery, typed template rendering, deep merge, and ordered Nacos HTTP batches with optional login and per-manager caching.
- 2026-11-25 · Pinned Go 1.27.4 and go-yaml v4.0.0. Passed `make check` on macOS arm64 and `go mod verify`. The sibling Rust baseline at `4ab8f42` passed 110 tests with the live test ignored. The Go HTTP tests use local `httptest` servers.

## Known issues

- See README.md for the explicit Go cache API, growing text cycle behavior, and JSON Unicode compatibility boundary.
