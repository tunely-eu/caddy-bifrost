package caddybifrost

import (
	"fmt"
	"net/http"

	"github.com/caddyserver/caddy/v2"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

type Transport struct {
	Endpoint    string         `json:"endpoint,omitempty"`
	App         string         `json:"app,omitempty"`
	DialTimeout caddy.Duration `json:"dial_timeout,omitempty"`

	transport *runtime.Transport
}

func (*Transport) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.transport.bifrost",
		New: func() caddy.Module { return new(Transport) },
	}
}

func (t *Transport) Provision(ctx caddy.Context) error {
	cfg := config.Transport{
		Endpoint:    t.Endpoint,
		App:         t.App,
		DialTimeout: t.DialTimeout,
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	t.Endpoint = cfg.Endpoint
	t.App = cfg.App
	t.DialTimeout = cfg.DialTimeout

	app, err := ctx.App(cfg.App)
	if err != nil {
		return fmt.Errorf("getting %s app: %w", cfg.App, err)
	}
	bifrostApp, ok := app.(*App)
	if !ok {
		return fmt.Errorf("%s app has unexpected type %T", cfg.App, app)
	}
	server, ok := bifrostApp.runtime.(*runtime.Server)
	if !ok {
		return fmt.Errorf("%s app must be configured with server runtime", cfg.App)
	}
	transport, err := runtime.NewTransport(cfg, server)
	if err != nil {
		return err
	}
	t.transport = transport
	return nil
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.transport == nil {
		return nil, fmt.Errorf("bifrost transport is not provisioned")
	}
	if req.URL.Scheme == "" {
		req.URL.Scheme = "http"
	}
	if req.URL.Host == "" {
		req.URL.Host = t.Endpoint
	}
	return t.transport.RoundTrip(req)
}

func (t *Transport) Cleanup() error {
	if t.transport != nil {
		return t.transport.Cleanup()
	}
	return nil
}

var (
	_ caddy.Provisioner  = (*Transport)(nil)
	_ caddy.CleanerUpper = (*Transport)(nil)
	_ http.RoundTripper  = (*Transport)(nil)
)
