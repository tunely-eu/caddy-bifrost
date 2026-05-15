package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	if strings.TrimSpace(next.URL.Host) == "" {
		return nil, fmt.Errorf("bifrost transport requires a reverse_proxy upstream host to derive the endpoint")
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

func (t *Transport) dialContext(ctx context.Context, _, address string) (net.Conn, error) {
	if t.server == nil {
		return nil, fmt.Errorf("bifrost server app is not configured")
	}
	if t.cfg.DialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t.cfg.DialTimeout))
		defer cancel()
	}
	endpoint, err := endpointFromDialAddress(address)
	if err != nil {
		return nil, err
	}
	return t.server.OpenStream(ctx, endpoint)
}

func endpointFromDialAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("bifrost endpoint cannot be derived from an empty upstream address")
	}
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		host = strings.TrimSpace(host)
		if host == "" {
			return "", fmt.Errorf("bifrost endpoint cannot be derived from upstream address %q", address)
		}
		return host, nil
	}
	if strings.HasPrefix(address, "[") && strings.Contains(address, "]") {
		host = strings.TrimPrefix(address[:strings.Index(address, "]")], "[")
	} else if strings.Count(address, ":") == 1 {
		host, _, _ = strings.Cut(address, ":")
	} else {
		host = address
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("bifrost endpoint cannot be derived from upstream address %q", address)
	}
	return host, nil
}
