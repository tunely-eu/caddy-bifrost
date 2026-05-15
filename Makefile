GO ?= go
XCADDY ?= xcaddy
XCADDY_WITH ?= github.com/tunely-eu/caddy-bifrost=.
XCADDY_FLAGS ?=
CADDY_VERSION ?= $(shell awk '$$1 == "github.com/caddyserver/caddy/v2" { sub(/^v/, "", $$2); print $$2 }' go.mod)

.PHONY: all fmt fmt-check vet test race-test build caddy-version xcaddy-build docker-build verify-module clean

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

caddy-version:
	@printf '%s\n' "$(CADDY_VERSION)"

xcaddy-build:
	$(XCADDY) build v$(CADDY_VERSION) --with $(XCADDY_WITH) $(XCADDY_FLAGS)

docker-build:
	docker build --build-arg CADDY_VERSION=$(CADDY_VERSION) -t caddy-bifrost:dev .

verify-module:
	./caddy list-modules | grep -E '^bifrost[[:space:]]*$$'
	./caddy list-modules | grep -E '^caddy\.listeners\.bifrost[[:space:]]*$$'
	./caddy list-modules | grep -E '^http\.reverse_proxy\.transport\.bifrost[[:space:]]*$$'

clean:
	rm -rf dist caddy
