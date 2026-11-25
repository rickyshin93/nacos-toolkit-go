GO ?= $(if $(wildcard .local/go/bin/go),.local/go/bin/go,go)
GOFMT ?= $(if $(wildcard .local/go/bin/gofmt),.local/go/bin/gofmt,gofmt)

ifneq ($(wildcard .local/go/bin/go),)
export GOCACHE ?= $(CURDIR)/.local/go-cache
export GOMODCACHE ?= $(CURDIR)/.local/go-mod
endif

.PHONY: check fmt-check vet test build test-live

check: fmt-check vet test build

fmt-check:
	@files=$$($(GOFMT) -l *.go) && test -z "$$files" || { printf 'gofmt required for %s\n' "$$files"; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -count=1 ./...

build:
	$(GO) build ./...

test-live:
	NACOS_LIVE_TEST=1 $(GO) test -count=1 -run '^TestLiveNacos$$' -v .
