// Package caddybifrost registers Bifrost tunnel support for Caddy.
//
// The package provides three Caddy modules:
//
//   - the "bifrost" app, which runs either a public connector server or a
//     private connector client
//   - the "http.reverse_proxy.transport.bifrost" transport, which lets public
//     Caddy proxy HTTP requests to an active Bifrost endpoint
//   - the "caddy.listeners.bifrost" listener wrapper, which can route raw TLS
//     streams by ClientHello SNI before Caddy's HTTP pipeline
//
// Import the package for its side effects when building Caddy with xcaddy or a
// custom main package.
package caddybifrost

import (
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

func init() {
	caddy.RegisterModule(new(App))
	caddy.RegisterModule(new(Transport))
	caddy.RegisterModule(new(ListenerWrapper))
	httpcaddyfile.RegisterGlobalOption("bifrost", parseApp)
}

func parseApp(d *caddyfile.Dispenser, _ any) (any, error) {
	app := new(App)
	if err := app.UnmarshalCaddyfile(d); err != nil {
		return nil, err
	}
	return httpcaddyfile.App{
		Name:  "bifrost",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

var (
	_ caddy.App         = (*App)(nil)
	_ caddy.Provisioner = (*App)(nil)
)
