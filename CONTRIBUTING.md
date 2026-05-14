# Contributing

Thanks for helping improve `caddy-bifrost`.

## Development

Use Go 1.25.x and build Caddy module binaries with `xcaddy`.
The Caddy version is sourced from `github.com/caddyserver/caddy/v2` in `go.mod`.

```sh
go test ./...
go test -race ./...
go vet ./...
go build -buildvcs=false ./...
make xcaddy-build
make verify-module
```

The module should stay generic and Tunely-agnostic. Tunely-specific control-plane
integration belongs in a separate module.

## Pull Requests

Keep changes scoped and include tests for behavior changes. For Caddyfile changes,
include both parser-level tests and adapted JSON coverage where practical.

Before opening a pull request, run the development checks above.
