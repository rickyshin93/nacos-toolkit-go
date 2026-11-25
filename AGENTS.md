# Project instructions

- This Go module implements the configuration behavior in the sibling `nacos-toolkit-rs` README, source and tests. Preserve observable config and Nacos HTTP v1 contracts.
- Use the standard library for HTTP and JSON. Keep the YAML parser as the sole runtime dependency. Use explicit per-manager caches and caller contexts.
- Run `make check` for formatting, vet, race tests and build. Keep compatibility and HTTP tests reproducible without a live server. Use `make test-live` only with authorized test settings.
- Update README.md when the public contract changes and STATUS.md when delivery status changes.
