package caddybifrost

import (
	"fmt"
	"net/http"

	"github.com/caddyserver/caddy/v2"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

// Transport is a Caddy reverse_proxy transport that opens streams to active
// Bifrost endpoints instead of dialing DNS or TCP upstream addresses directly.
//
// The upstream dial address is interpreted as the Bifrost endpoint key. For
// example, "reverse_proxy home { transport bifrost }" opens a stream to endpoint
// "home".
type Transport struct {
	// App is the Caddy app name that owns the Bifrost server runtime. It defaults
	// to "bifrost".
	App string `json:"app,omitempty"`

	// DialTimeout bounds how long RoundTrip waits for a stream to an endpoint.
	DialTimeout caddy.Duration `json:"dial_timeout,omitempty"`

	transport *runtime.Transport
}

// CaddyModule returns the module registration for the reverse_proxy transport.
func (*Transport) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.transport.bifrost",
		New: func() caddy.Module { return new(Transport) },
	}
}

// Provision resolves the configured Bifrost app and prepares the runtime
// transport.
func (t *Transport) Provision(ctx caddy.Context) error {
	cfg := config.Transport{
		App:         t.App,
		DialTimeout: t.DialTimeout,
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
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

// RoundTrip proxies one HTTP request through a Bifrost stream to the endpoint
// named by the request URL host.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.transport == nil {
		return nil, fmt.Errorf("bifrost transport is not provisioned")
	}
	return t.transport.RoundTrip(req)
}

// Cleanup releases transport resources during Caddy shutdown or reload.
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
