package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
)

type Transport struct {
	cfg       config.Transport
	server    *Server
	transport *http.Transport
}

func NewTransport(cfg config.Transport, server *Server) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	t := &Transport{
		cfg:    cfg,
		server: server,
	}
	t.transport = &http.Transport{
		DialContext:       t.dialContext,
		DisableKeepAlives: false,
		ForceAttemptHTTP2: false,
	}
	return t, nil
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.transport == nil {
		return nil, fmt.Errorf("bifrost transport is not provisioned")
	}
	next := req.Clone(req.Context())
	next.URL = cloneURL(req)
	if next.URL.Scheme == "" {
		next.URL.Scheme = "http"
	}
	if next.URL.Host == "" {
		next.URL.Host = t.cfg.Endpoint
	}
	return t.transport.RoundTrip(next)
}

func cloneURL(req *http.Request) *url.URL {
	cloned := *req.URL
	return &cloned
}

func (t *Transport) Cleanup() error {
	if t != nil && t.transport != nil {
		t.transport.CloseIdleConnections()
	}
	return nil
}

func (t *Transport) dialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	if t.server == nil {
		return nil, fmt.Errorf("bifrost server app is not configured")
	}
	if t.cfg.DialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t.cfg.DialTimeout))
		defer cancel()
	}
	return t.server.OpenStream(ctx, t.cfg.Endpoint)
}
