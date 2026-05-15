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
