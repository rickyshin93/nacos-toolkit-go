# Nacos Toolkit for Go

[简体中文](README.zh-CN.md)

[![CI](https://github.com/rickyshin93/nacos-toolkit-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/rickyshin93/nacos-toolkit-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/rickyshin93/nacos-toolkit-go.svg)](https://pkg.go.dev/github.com/rickyshin93/nacos-toolkit-go)

Load configuration from Nacos HTTP OpenAPI v1 or the local filesystem. The library renders placeholders, merges overrides, and caches a processed Nacos batch. The behavior reference is the sibling `nacos-toolkit-rs` at `4ab8f42`.

## Install

From a Go project's module directory, run:

```sh
go get github.com/rickyshin93/nacos-toolkit-go@v0.0.1
```

## Read a Nacos configuration

```go
import (
    "context"

    toolkit "github.com/rickyshin93/nacos-toolkit-go"
)

func load(ctx context.Context, conn toolkit.Connection) (any, error) {
    manager, err := toolkit.New(conn)
    if err != nil {
        return nil, err
    }

    base := []toolkit.ConfigRef{
        {DataID: "common.yml", Group: "DEFAULT_GROUP"},
        {DataID: "app.yml", Group: "DEFAULT_GROUP"},
    }
    result, err := manager.Get(ctx, base, nil, false)
    return result.Config, err
}
```

Keep the manager for later reads of the same batch. Use a separate manager for each independent connection or cache.

- `Connection.ServerAddr` accepts a host and port or an HTTP(S) origin. An empty `Namespace` omits the `tenant` query parameter. If either credential is non-empty, the first fetch posts to `/nacos/v1/auth/login`. A successful token is reused for the lifetime of the source. Each HTTP request has a 10-second timeout and honors the caller context.
- `Get` fetches every base reference in order and parses each as YAML. The merged top-level keys supply variables, with later values winning. Only the last base config becomes output. `DEPLOY_ENV` is set to the namespace in the external context. Keys in the config being rendered take precedence.
- Supply an override reference to fetch it after the base batch. A `.json` suffix selects JSON. The override's own keys take precedence over the rendered base and external variables. Objects deep-merge, while arrays and scalars are replaced.
- The first successful batch result is cached per manager. Later calls reuse it regardless of the references supplied. Debug mode returns the shallow-merged raw base context with the original values. `ClearCache` clears both results and retains the source's login token. Concurrent cold loads are serialized.
- Request failures stop the batch and return errors. Non-success statuses can be checked with `errors.As` and `*toolkit.HTTPError`. A failed override leaves the fetched base context available via `Raw` and leaves the processed cache empty.

## Process configuration and discover local files

```go
config := toolkit.Process(
    "host: ${HOST}\nport: 3000",
    toolkit.YAML,
    map[string]any{"HOST": "localhost"},
    nil,
)

path, found := toolkit.FindLocalConfig("app", "./config")
if found {
    local, err := toolkit.ParseConfigFile(path)
    // Use local or handle err.
    _, _ = local, err
}
```

- Decoded configs use `map[string]any`, `[]any`, and JSON scalars. Parsed numbers use `json.Number`. `Parse` and `Process` return an empty object for invalid input or a non-object YAML root. JSON accepts any root. `ParseConfigFile` returns errors directly and accepts scalar and array roots for both formats.
- Local discovery checks `.json`, then `.yaml`, then `.yml`. `GetLocalConfig` reports false when the first match cannot be parsed. Explicit paths accept case-insensitive extensions.
- `Render` and `Process` expand `${path.to.key}`. Whole-value references preserve their type. Direct string entries in arrays use text substitutions, and nested arrays are copied as-is. Missing and null variables leave placeholders intact.
- `RenderText` uses up to five passes. Full rendering resolves referenced variable chains and terminates cycles, including cycles that would otherwise keep growing. Whole-container expansion is limited to five levels.
- By default, processing splits comma-separated `cors.whitelist` strings into trimmed arrays. A non-nil slice of dot-paths sets the fields to convert. An empty slice disables conversion. `ProcessAndMerge` exposes the same override behavior for local values.

## Development

Use Go **1.27.4**, the latest stable patch verified on 2026-11-25. The sole runtime dependency is **go.yaml.in/yaml/v4 v4.0.0**, maintained by the YAML organization. The YAML decoder uses the parser's node representation to preserve the reference library's scalar and mapping semantics. [Go release history](https://go.dev/doc/devel/release), [go-yaml v4.0.0 release](https://github.com/yaml/go-yaml/releases/tag/v4.0.0)

```sh
make check
```

The check runs `gofmt -l`, `go vet ./...`, `go test -race -count=1 ./...`, and `go build ./...`. It uses a project-local Go installation under `.local/go` if present, or Go from `PATH`. The offline tests include the Rust/Python crosscheck fixture and local HTTP tests for URL encoding, optional login, batch order, caching, response errors, and context cancellation.

To run a read-only smoke test with authorized Nacos settings, supply `NACOS_ADDR`, `NACOS_DATA_ID`, and `NACOS_GROUP` in the environment. `NACOS_NS`, `NACOS_USER`, and `NACOS_PASS` are optional. Run `make test-live`. The smoke test checks for a readable configuration object and avoids printing the content.

## Release a version

Run `go mod tidy` and `make check`, then land the changes on `main`. GitHub Actions checks PRs and `main` on Linux and macOS. Wait for both checks on the commit you intend to release.

Choose a new semantic version and push its Git tag. For example, to release `v0.0.2`:

```sh
git switch main
git pull --ff-only origin main
git tag -a v0.0.2 -m "Release v0.0.2"
git push origin v0.0.2
GOPROXY=https://proxy.golang.org go list -m github.com/rickyshin93/nacos-toolkit-go@v0.0.2
```

The public Git tag is the Go module version. The proxy fetch also makes it eligible for discovery on pkg.go.dev. GitHub Actions reruns both checks on the tag and creates a GitHub Release with generated notes after they pass. For changes after a release, publish a new version tag. [Go module publishing guide](https://go.dev/doc/modules/publishing)

## Compatibility boundaries

The Go API uses explicit manager instances, caller contexts and standard Go errors. Its transport covers configuration reads through Nacos HTTP OpenAPI v1. For previously unbounded text cycles, rendering returns a string with unresolved placeholders. JSON decoding uses Go's handling of unpaired Unicode surrogate escapes, which can produce replacement characters where the Rust parser rejects the input.

## License

MIT.
