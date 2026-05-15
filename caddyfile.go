package caddybifrost

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"

	bifrostcaddyfile "github.com/tunely-eu/caddy-bifrost/internal/caddyfile"
)

func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	server, client, err := bifrostcaddyfile.ParseApp(d)
	if err != nil {
		return err
	}
	a.Server = server
	a.Client = client
	return nil
}

func (t *Transport) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	cfg, err := bifrostcaddyfile.ParseTransport(d)
	if err != nil {
		return err
	}
	t.App = cfg.App
	t.DialTimeout = cfg.DialTimeout
	return nil
}
