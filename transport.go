package caddybifrost

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
)

type Transport struct {
	Endpoint    string         `json:"endpoint,omitempty"`
	App         string         `json:"app,omitempty"`
	DialTimeout caddy.Duration `json:"dial_timeout,omitempty"`

	server    *Server
	transport *http.Transport
}

func (*Transport) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.transport.bifrost",
		New: func() caddy.Module { return new(Transport) },
	}
}

func (t *Transport) Provision(ctx caddy.Context) error {
	t.Endpoint = strings.TrimSpace(t.Endpoint)
	if t.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if t.App == "" {
		t.App = "bifrost"
	}
	app, err := ctx.App(t.App)
	if err != nil {
		return fmt.Errorf("getting %s app: %w", t.App, err)
	}
	bifrostApp, ok := app.(*App)
	if !ok {
		return fmt.Errorf("%s app has unexpected type %T", t.App, app)
	}
	if bifrostApp.Server == nil {
		return fmt.Errorf("%s app must be configured with server runtime", t.App)
	}
	t.server = bifrostApp.Server
	t.transport = &http.Transport{
		DialContext:       t.dialContext,
		DisableKeepAlives: false,
		ForceAttemptHTTP2: false,
	}
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
		t.transport.CloseIdleConnections()
	}
	return nil
}

func (t *Transport) dialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	if t.server == nil {
		return nil, fmt.Errorf("bifrost server app is not configured")
	}
	if t.DialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t.DialTimeout))
		defer cancel()
	}
	return t.server.OpenStream(ctx, t.Endpoint)
}

var (
	_ caddy.Provisioner  = (*Transport)(nil)
	_ caddy.CleanerUpper = (*Transport)(nil)
	_ http.RoundTripper  = (*Transport)(nil)
)
