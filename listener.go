package caddybifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/netutil"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

// ListenerWrapper routes raw TLS connections to Bifrost endpoints before Caddy's
// TLS listener wrapper consumes the connection.
//
// It is used for private TLS / SNI passthrough deployments. Matching ClientHello
// SNI names are proxied through Bifrost as raw streams. Non-matching connections
// are replayed into Caddy's normal listener pipeline.
type ListenerWrapper struct {
	// App is the Caddy app name that owns the Bifrost server runtime. It defaults
	// to "bifrost".
	App string `json:"app,omitempty"`

	// Routes maps exact SNI names to Bifrost endpoint keys.
	Routes []config.SNIRoute `json:"routes,omitempty"`

	// PassthroughResolverRaw optionally loads a custom Caddy module from the
	// bifrost.passthrough_resolvers namespace. It replaces static route_sni
	// mappings.
	PassthroughResolverRaw json.RawMessage `json:"passthrough_resolver,omitempty" caddy:"namespace=bifrost.passthrough_resolvers inline_key=resolver"`

	ctx        context.Context
	logger     *zap.Logger
	bifrostApp *App
}

// CaddyModule returns the module registration for the listener wrapper.
func (*ListenerWrapper) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.listeners.bifrost",
		New: func() caddy.Module { return new(ListenerWrapper) },
	}
}

// Provision resolves the Bifrost server runtime and installs any static SNI
// passthrough routes.
func (w *ListenerWrapper) Provision(ctx caddy.Context) error {
	if strings.TrimSpace(w.App) == "" {
		w.App = config.DefaultAppName
	}
	w.ctx = ctx.Context
	w.logger = ctx.Logger(w)

	if len(w.Routes) > 0 && len(w.PassthroughResolverRaw) > 0 {
		return fmt.Errorf("bifrost listener routes cannot be combined with passthrough_resolver")
	}

	app, err := ctx.App(w.App)
	if err != nil {
		return fmt.Errorf("getting %s app: %w", w.App, err)
	}
	bifrostApp, ok := app.(*App)
	if !ok {
		return fmt.Errorf("%s app has unexpected type %T", w.App, app)
	}
	w.bifrostApp = bifrostApp

	if len(w.PassthroughResolverRaw) > 0 {
		resolver, err := w.loadPassthroughResolver(ctx)
		if err != nil {
			return err
		}
		server, err := w.serverRuntime()
		if err != nil {
			return err
		}
		server.SetPassthroughResolver(resolver)
		w.logger.Info("installed bifrost passthrough resolver")
	} else if len(w.Routes) > 0 {
		resolver, err := runtime.NewStaticPassthroughResolver(w.Routes)
		if err != nil {
			return fmt.Errorf("bifrost listener passthrough routes: %w", err)
		}
		server, err := w.serverRuntime()
		if err != nil {
			return err
		}
		server.SetPassthroughResolver(resolver)
		w.logger.Info("installed bifrost passthrough routes", zap.Int("routes", len(w.Routes)))
	}
	return nil
}

func (w *ListenerWrapper) loadPassthroughResolver(ctx caddy.Context) (PassthroughResolver, error) {
	if len(w.PassthroughResolverRaw) == 0 {
		return nil, nil
	}
	mod, err := ctx.LoadModule(w, "PassthroughResolverRaw")
	if err != nil {
		return nil, fmt.Errorf("loading bifrost passthrough resolver: %w", err)
	}
	resolver, ok := mod.(PassthroughResolver)
	if !ok {
		return nil, fmt.Errorf("bifrost passthrough resolver module has unexpected type %T", mod)
	}
	return resolver, nil
}

// WrapListener returns a listener that intercepts matching TLS ClientHello SNI
// names and forwards them through Bifrost.
func (w *ListenerWrapper) WrapListener(listener net.Listener) net.Listener {
	return &bifrostListener{Listener: listener, wrapper: w}
}

// UnmarshalCaddyfile parses the "bifrost" listener wrapper block.
func (w *ListenerWrapper) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	if d.NextArg() {
		return d.ArgErr()
	}
	for d.NextBlock(0) {
		switch d.Val() {
		case "app":
			if !d.NextArg() {
				return d.ArgErr()
			}
			w.App = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "route_sni":
			if !d.NextArg() {
				return d.ArgErr()
			}
			serverName := d.Val()
			if !d.NextArg() {
				return d.ArgErr()
			}
			endpoint := d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
			w.Routes = append(w.Routes, config.SNIRoute{ServerName: serverName, Endpoint: endpoint})
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func (w *ListenerWrapper) serverRuntime() (*runtime.Server, error) {
	if w == nil || w.bifrostApp == nil {
		return nil, fmt.Errorf("bifrost listener wrapper is not provisioned")
	}
	server, ok := w.bifrostApp.runtime.(*runtime.Server)
	if !ok {
		return nil, fmt.Errorf("%s app must be configured with server runtime", w.App)
	}
	return server, nil
}

type bifrostListener struct {
	net.Listener
	wrapper *ListenerWrapper
}

func (l *bifrostListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		replayConn, handled := l.wrapper.handleConnection(conn)
		if handled {
			continue
		}
		return replayConn, nil
	}
}

func (w *ListenerWrapper) handleConnection(conn net.Conn) (net.Conn, bool) {
	serverName, replayConn, err := netutil.PeekClientHelloServerName(conn)
	if replayConn == nil {
		replayConn = conn
	}
	if err != nil {
		w.logger.Debug("bifrost passthrough client hello peek failed", zap.Error(err))
		return replayConn, false
	}

	server, err := w.serverRuntime()
	if err != nil {
		w.logger.Warn("bifrost passthrough server unavailable", zap.Error(err))
		_ = replayConn.Close()
		return nil, true
	}
	ctx := w.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint, ok, err := server.ResolvePassthrough(ctx, serverName)
	if err != nil {
		w.logger.Warn("bifrost passthrough route resolution failed", zap.String("server_name", serverName), zap.Error(err))
		_ = replayConn.Close()
		return nil, true
	}
	if !ok {
		return replayConn, false
	}

	go func() {
		if err := server.ProxyStream(ctx, endpoint, replayConn); err != nil {
			w.logger.Warn("bifrost passthrough proxy failed", zap.String("server_name", serverName), zap.String("endpoint", endpoint), zap.Error(err))
		}
	}()
	return nil, true
}

var (
	_ caddy.Provisioner     = (*ListenerWrapper)(nil)
	_ caddy.ListenerWrapper = (*ListenerWrapper)(nil)
	_ caddyfile.Unmarshaler = (*ListenerWrapper)(nil)
)
