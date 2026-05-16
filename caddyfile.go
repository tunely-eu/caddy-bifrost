package caddybifrost

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"

	bifrostcaddyfile "github.com/tunely-eu/caddy-bifrost/internal/caddyfile"
)

// UnmarshalCaddyfile parses the global "bifrost" Caddyfile option into either a
// server or client app runtime.
func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	server, client, err := bifrostcaddyfile.ParseApp(d)
	if err != nil {
		return err
	}
	a.Server = server
	a.Client = client
	return nil
}

// UnmarshalCaddyfile parses "transport bifrost" inside Caddy's reverse_proxy
// handler.
func (t *Transport) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	cfg, err := bifrostcaddyfile.ParseTransport(d)
	if err != nil {
		return err
	}
	t.App = cfg.App
	t.DialTimeout = cfg.DialTimeout
	return nil
}
