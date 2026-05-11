GO ?= go
XCADDY ?= xcaddy
XCADDY_WITH ?= github.com/tunely-eu/caddy-bifrost=.
XCADDY_FLAGS ?=

.PHONY: all fmt fmt-check vet test race-test build xcaddy-build verify-module clean

all: fmt-check vet test build

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race-test:
	$(GO) test -race ./...

build:
	$(GO) build -buildvcs=false ./...

xcaddy-build:
	$(XCADDY) build --with $(XCADDY_WITH) $(XCADDY_FLAGS)

verify-module:
	./caddy list-modules | grep -E '^bifrost[[:space:]]*$$'

clean:
	rm -rf dist caddy
