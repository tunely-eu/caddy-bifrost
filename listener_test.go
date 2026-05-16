package caddybifrost

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

func init() {
	caddy.RegisterModule(new(testPassthroughResolverModule))
}

type testPassthroughResolverModule struct {
	Endpoint string `json:"endpoint,omitempty"`
}

func (*testPassthroughResolverModule) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "bifrost.passthrough_resolvers.test",
		New: func() caddy.Module { return new(testPassthroughResolverModule) },
	}
}

func (r *testPassthroughResolverModule) ResolvePassthrough(_ context.Context, serverName string) (string, bool, error) {
	if strings.EqualFold(serverName, "home.example.com") {
		return r.Endpoint, true, nil
	}
	return "", false, nil
}

func TestListenerWrapperUnmarshalCaddyfileRoutes(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	app bifrost
	route_sni Home.Example.Com. home
	route_sni files.example.com home
}`)
	var wrapper ListenerWrapper
	if err := wrapper.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if wrapper.App != "bifrost" {
		t.Fatalf("app = %q", wrapper.App)
	}
	if len(wrapper.Routes) != 2 {
		t.Fatalf("routes = %#v", wrapper.Routes)
	}
	if wrapper.Routes[0].ServerName != "Home.Example.Com." || wrapper.Routes[0].Endpoint != "home" {
		t.Fatalf("route = %#v", wrapper.Routes[0])
	}
}

func TestListenerWrapperRoutesDoNotCreateHTTPHostRoute(t *testing.T) {
	httpApp := provisionHTTPApp(t, `{
	local_certs
	servers :443 {
		listener_wrappers {
			bifrost {
				route_sni home.example.com home
			}
			tls
		}
	}
	bifrost {
		server public.example.com {
			endpoint home {
				token secret
			}
		}
	}
}

media.example.com {
	reverse_proxy home {
		transport bifrost
	}
}`)

	hosts := collectHTTPRouteHosts(httpApp)
	if !containsHost(hosts, "media.example.com") {
		t.Fatalf("expected public HTTP route host, got %#v", hosts)
	}
	if containsHost(hosts, "home.example.com") {
		t.Fatalf("private passthrough SNI leaked into HTTP routes: %#v", hosts)
	}
}

func TestListenerWrapperLoadsPassthroughResolverModule(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	local_certs
}`)
	var cfg caddy.Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal caddy config: %v", err)
	}
	ctx, err := caddy.ProvisionContext(&cfg)
	if err != nil {
		t.Fatalf("ProvisionContext: %v", err)
	}

	wrapper := &ListenerWrapper{
		PassthroughResolverRaw: json.RawMessage(`{"resolver":"test","endpoint":"home"}`),
	}
	resolver, err := wrapper.loadPassthroughResolver(ctx)
	if err != nil {
		t.Fatalf("loadPassthroughResolver: %v", err)
	}
	endpoint, ok, err := resolver.ResolvePassthrough(context.Background(), "home.example.com")
	if err != nil {
		t.Fatalf("ResolvePassthrough: %v", err)
	}
	if !ok || endpoint != "home" {
		t.Fatalf("resolved endpoint = %q, %t", endpoint, ok)
	}
}

func TestListenerWrapperRejectsRoutesWithPassthroughResolver(t *testing.T) {
	wrapper := &ListenerWrapper{
		Routes:                 []config.SNIRoute{{ServerName: "home.example.com", Endpoint: "home"}},
		PassthroughResolverRaw: json.RawMessage(`{"resolver":"test","endpoint":"home"}`),
	}
	configJSON := adaptCaddyfile(t, `{
	local_certs
}`)
	var cfg caddy.Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal caddy config: %v", err)
	}
	ctx, err := caddy.ProvisionContext(&cfg)
	if err != nil {
		t.Fatalf("ProvisionContext: %v", err)
	}
	if err := wrapper.Provision(ctx); err == nil {
		t.Fatal("expected passthrough resolver conflict")
	}
}

func TestListenerWrapperResolverOKFalseReplaysToCaddy(t *testing.T) {
	resolver := &recordingPassthroughResolver{}
	wrapper := listenerWrapperWithResolver(resolver)
	serverConn, handshakeDone := tlsClientHelloConn(t, "home.example.com")

	replayConn, handled := wrapper.handleConnection(serverConn)
	if handled {
		t.Fatal("expected connection to continue into Caddy")
	}
	if replayConn == nil {
		t.Fatal("expected replay connection")
	}
	if resolver.serverName != "home.example.com" {
		t.Fatalf("server name = %q", resolver.serverName)
	}
	_ = replayConn.Close()
	waitHandshakeError(t, handshakeDone)
}

func TestListenerWrapperResolverOKTrueProxiesEndpoint(t *testing.T) {
	resolver := &recordingPassthroughResolver{endpoint: "home", ok: true}
	wrapper := listenerWrapperWithResolver(resolver)
	serverConn, handshakeDone := tlsClientHelloConn(t, "home.example.com")

	replayConn, handled := wrapper.handleConnection(serverConn)
	if !handled {
		t.Fatal("expected connection to be handled by Bifrost")
	}
	if replayConn != nil {
		t.Fatal("expected no replay connection")
	}
	if resolver.serverName != "home.example.com" {
		t.Fatalf("server name = %q", resolver.serverName)
	}
	waitHandshakeError(t, handshakeDone)
}

func TestListenerWrapperResolverErrorClosesConnection(t *testing.T) {
	resolver := &recordingPassthroughResolver{err: errors.New("resolver unavailable")}
	wrapper := listenerWrapperWithResolver(resolver)
	serverConn, handshakeDone := tlsClientHelloConn(t, "home.example.com")

	replayConn, handled := wrapper.handleConnection(serverConn)
	if !handled {
		t.Fatal("expected resolver error to handle and close connection")
	}
	if replayConn != nil {
		t.Fatal("expected no replay connection")
	}
	if resolver.serverName != "home.example.com" {
		t.Fatalf("server name = %q", resolver.serverName)
	}
	waitHandshakeError(t, handshakeDone)
}

func provisionHTTPApp(t *testing.T, input string) *caddyhttp.App {
	t.Helper()
	configJSON := adaptCaddyfile(t, input)
	var cfg caddy.Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal adapted config: %v", err)
	}
	ctx, err := caddy.ProvisionContext(&cfg)
	if err != nil {
		t.Fatalf("ProvisionContext: %v", err)
	}
	app, err := ctx.App("http")
	if err != nil {
		t.Fatalf("http app: %v", err)
	}
	httpApp, ok := app.(*caddyhttp.App)
	if !ok {
		t.Fatalf("http app type = %T", app)
	}
	return httpApp
}

func collectHTTPRouteHosts(httpApp *caddyhttp.App) []string {
	var hosts []string
	for _, server := range httpApp.Servers {
		if server == nil {
			continue
		}
		hosts = collectHTTPRouteListHosts(hosts, server.Routes)
	}
	return hosts
}

func collectHTTPRouteListHosts(hosts []string, routes caddyhttp.RouteList) []string {
	for _, route := range routes {
		for _, matcherSet := range route.MatcherSets {
			for _, matcher := range matcherSet {
				switch hostMatcher := matcher.(type) {
				case *caddyhttp.MatchHost:
					for _, host := range *hostMatcher {
						hosts = append(hosts, strings.ToLower(host))
					}
				case caddyhttp.MatchHost:
					for _, host := range hostMatcher {
						hosts = append(hosts, strings.ToLower(host))
					}
				}
			}
		}
		for _, handler := range route.Handlers {
			if subroute, ok := handler.(*caddyhttp.Subroute); ok {
				hosts = collectHTTPRouteListHosts(hosts, subroute.Routes)
			}
		}
	}
	return hosts
}

func containsHost(hosts []string, target string) bool {
	target = strings.ToLower(target)
	for _, host := range hosts {
		if strings.TrimSuffix(host, ".") == strings.TrimSuffix(target, ".") {
			return true
		}
	}
	return false
}

type recordingPassthroughResolver struct {
	endpoint   string
	ok         bool
	err        error
	serverName string
}

func (r *recordingPassthroughResolver) ResolvePassthrough(_ context.Context, serverName string) (string, bool, error) {
	r.serverName = serverName
	return r.endpoint, r.ok, r.err
}

func listenerWrapperWithResolver(resolver runtime.PassthroughResolver) *ListenerWrapper {
	server := &runtime.Server{}
	server.SetPassthroughResolver(resolver)
	return &ListenerWrapper{
		App:        "bifrost",
		ctx:        context.Background(),
		logger:     zap.NewNop(),
		bifrostApp: &App{runtime: server},
	}
}

func tlsClientHelloConn(t *testing.T, serverName string) (net.Conn, <-chan error) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	if err := serverConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set server deadline: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		tlsConn := tls.Client(clientConn, &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         serverName,
			InsecureSkipVerify: true,
		})
		done <- tlsConn.Handshake()
		_ = tlsConn.Close()
	}()
	return serverConn, done
}

func waitHandshakeError(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected client handshake error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client handshake")
	}
}

var _ PassthroughResolverModule = (*testPassthroughResolverModule)(nil)
