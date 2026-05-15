package caddybifrost

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
	"github.com/tunely-eu/caddy-bifrost/internal/testutil"
)

func TestServerClientProxyStreamRawTLS(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	passthroughAddr := testutil.FreeTCPAddr(t)

	server, client, _, originCA, stop := startServerClient(t, connectorAddr, true)
	defer stop()

	if server == nil || client == nil {
		t.Fatal("expected server and client")
	}
	listener, err := net.Listen("tcp", passthroughAddr)
	if err != nil {
		t.Fatalf("listen passthrough test listener: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = server.ProxyStream(context.Background(), "home", conn)
			}()
		}
	}()
	response := testutil.WaitHTTPSResponse(t, passthroughAddr, "home.example.com", originCA)
	assertOKResponse(t, response)
}

func TestTransportProxiesHTTPOverBifrost(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	server, _, transport, _, stop := startServerClient(t, connectorAddr, false)
	defer stop()

	if server == nil || transport == nil {
		t.Fatal("expected server and transport")
	}
	response := testutil.WaitHTTPTransportResponse(t, transport, "http://home/")
	assertOKResponse(t, response)
}

func TestServerClientUsesInjectedAcceptProvider(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	originAddr, stopOrigin := testutil.StartHTTPOrigin(t)
	defer stopOrigin()

	dir := t.TempDir()
	bifrostCert, bifrostKey, bifrostCA := testutil.WriteTestCertFiles(t, dir, "localhost")
	bifrostKeyPair, err := tls.LoadX509KeyPair(bifrostCert, bifrostKey)
	if err != nil {
		t.Fatalf("load bifrost cert: %v", err)
	}

	server, err := runtime.NewServerWithTLSConfig(
		&config.Server{
			Connector: config.Connector{
				Listen:     connectorAddr,
				TLSSubject: "localhost",
			},
		},
		&tls.Config{Certificates: []tls.Certificate{bifrostKeyPair}},
		zap.NewNop(),
		runtime.WithAcceptProvider(staticTestAcceptProvider{token: "dynamic-secret", endpoint: "home"}),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer server.Stop()

	client, err := runtime.NewClient(&config.Client{
		Connect:       connectorAddr,
		Token:         "dynamic-secret",
		Forward:       originAddr,
		TLSCAFile:     bifrostCA,
		TLSServerName: "localhost",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.Start(); err != nil {
		t.Fatalf("client start: %v", err)
	}
	defer client.Stop()

	transport, err := runtime.NewTransport(config.Transport{Endpoint: "home"}, server)
	if err != nil {
		t.Fatalf("new transport: %v", err)
	}
	defer transport.Cleanup()

	response := testutil.WaitHTTPTransportResponse(t, transport, "http://home/")
	assertOKResponse(t, response)
}

func TestTransportReturnsErrorWithoutActiveEndpoint(t *testing.T) {
	transport, err := runtime.NewTransport(config.Transport{Endpoint: "missing"}, nil)
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	defer transport.Cleanup()

	req, err := http.NewRequest(http.MethodGet, "http://missing/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestTransportRoundTripDoesNotMutateRequest(t *testing.T) {
	transport, err := runtime.NewTransport(config.Transport{Endpoint: "home"}, nil)
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	defer transport.Cleanup()

	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, _ = transport.RoundTrip(req)
	if req.URL.Scheme != "" || req.URL.Host != "" {
		t.Fatalf("request URL mutated to %s", req.URL.String())
	}
}

func TestTransportSupportsReloadOverlap(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)

	first, firstClient, _, _, firstStop := startServerClient(t, connectorAddr, false)
	defer firstStop()
	if first == nil || firstClient == nil {
		t.Fatal("expected first runtime")
	}

	secondServer, secondClient, secondTransport, _, secondStop := startServerClient(t, connectorAddr, false)
	defer secondStop()
	if secondServer == nil || secondClient == nil || secondTransport == nil {
		t.Fatal("expected second runtime")
	}

	if err := first.Stop(); err != nil {
		t.Fatalf("first server stop: %v", err)
	}
	if err := firstClient.Stop(); err != nil {
		t.Fatalf("first client stop: %v", err)
	}

	httpResponse := testutil.WaitHTTPTransportResponse(t, secondTransport, "http://home/")
	assertOKResponse(t, httpResponse)
}

func TestCaddyLifecycleLoadReloadStopReleasesListeners(t *testing.T) {
	_ = caddy.Stop()
	t.Cleanup(func() {
		_ = caddy.Stop()
	})

	connectorAddr := testutil.FreeTCPAddr(t)
	httpsAddr := testutil.FreeTCPAddr(t)
	storageDir := t.TempDir()

	firstConfig := caddyLifecycleConfig(t, connectorAddr, httpsAddr, storageDir, "home.example.com")
	secondConfig := caddyLifecycleConfig(t, connectorAddr, httpsAddr, storageDir, "home.example.com", "files.example.com")

	if err := caddy.Load(firstConfig, true); err != nil {
		t.Fatalf("first caddy load: %v", err)
	}
	waitPublicHTTPSResponse(t, httpsAddr)

	if err := caddy.Load(secondConfig, true); err != nil {
		t.Fatalf("caddy reload on same listeners: %v", err)
	}
	waitPublicHTTPSResponse(t, httpsAddr)

	if err := caddy.Stop(); err != nil {
		t.Fatalf("caddy stop after reload: %v", err)
	}
	assertTCPListenAvailable(t, connectorAddr)
	assertTCPListenAvailable(t, httpsAddr)

	if err := caddy.Load(secondConfig, true); err != nil {
		t.Fatalf("caddy load after stop on same listeners: %v", err)
	}
	waitPublicHTTPSResponse(t, httpsAddr)

	if err := caddy.Stop(); err != nil {
		t.Fatalf("final caddy stop: %v", err)
	}
	assertTCPListenAvailable(t, connectorAddr)
	assertTCPListenAvailable(t, httpsAddr)
}

func startServerClient(t *testing.T, connectorAddr string, originTLS bool) (*runtime.Server, *runtime.Client, http.RoundTripper, []byte, func()) {
	t.Helper()
	dir := t.TempDir()
	bifrostCert, bifrostKey, bifrostCA := testutil.WriteTestCertFiles(t, dir, "localhost")
	bifrostKeyPair, err := tls.LoadX509KeyPair(bifrostCert, bifrostKey)
	if err != nil {
		t.Fatalf("load bifrost cert: %v", err)
	}

	var originAddr string
	var originCA []byte
	var stopOrigin func()
	if originTLS {
		originCertPEM, originKeyPEM := originCert(t)
		originCA = originCertPEM
		originCert, err := tls.X509KeyPair(originCertPEM, originKeyPEM)
		if err != nil {
			t.Fatalf("load origin cert: %v", err)
		}
		originAddr, stopOrigin = testutil.StartTLSOrigin(t, originCert)
	} else {
		originAddr, stopOrigin = testutil.StartHTTPOrigin(t)
	}

	serverConfig := &config.Server{
		Connector: config.Connector{
			Listen:     connectorAddr,
			TLSSubject: "localhost",
			Endpoints: []config.Endpoint{
				{
					Key:    "home",
					Token:  "secret",
					Policy: "replace_existing",
					Limits: config.EndpointLimits{MaxStreams: 10},
				},
			},
		},
	}
	server, err := runtime.NewServerWithTLSConfig(serverConfig, &tls.Config{Certificates: []tls.Certificate{bifrostKeyPair}}, zap.NewNop())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}

	client, err := runtime.NewClient(&config.Client{
		Connect:       connectorAddr,
		Token:         "secret",
		Forward:       originAddr,
		TLSCAFile:     bifrostCA,
		TLSServerName: "localhost",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.Start(); err != nil {
		t.Fatalf("client start: %v", err)
	}

	transport, err := runtime.NewTransport(config.Transport{Endpoint: "home"}, server)
	if err != nil {
		t.Fatalf("new transport: %v", err)
	}

	stop := func() {
		_ = transport.Cleanup()
		_ = client.Stop()
		_ = server.Stop()
		stopOrigin()
	}
	return server, client, transport, originCA, stop
}

func caddyLifecycleConfig(t *testing.T, connectorAddr, httpsAddr, storageDir string, routes ...string) []byte {
	t.Helper()
	routeConfig := ""
	for _, route := range routes {
		routeConfig += fmt.Sprintf("\n\t\t\t\troute_sni %s home", route)
	}
	return adaptCaddyfile(t, fmt.Sprintf(`{
	admin off
	local_certs
	skip_install_trust
	auto_https disable_redirects
	storage file_system {
		root %s
	}
	storage_clean_interval off
	servers {
		protocols h1 h2
		listener_wrappers {
			bifrost {%s
			}
			tls
		}
	}
	bifrost {
		server {
			connector %s {
				tls bifrost.example.com
				endpoint home {
					token secret
					policy replace_existing
					limits {
						max_streams 10
					}
				}
			}
		}
	}
}

https://public.example.com:%s {
	bind 127.0.0.1
	respond "public-ok"
}`, storageDir, routeConfig, connectorAddr, tcpPort(t, httpsAddr)))
}

func waitPublicHTTPSResponse(t *testing.T, addr string) {
	t.Helper()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName:         "public.example.com",
				InsecureSkipVerify: true,
			},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
	req, err := http.NewRequest(http.MethodGet, "https://public.example.com/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status = %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("public HTTPS request did not become ready: %v", lastErr)
}

func assertTCPListenAvailable(t *testing.T, addr string) {
	t.Helper()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("expected %s to be released: %v", addr, err)
	}
	_ = listener.Close()
}

func tcpPort(t *testing.T, addr string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port %q: %v", addr, err)
	}
	return port
}

func assertOKResponse(t *testing.T, response []byte) {
	t.Helper()
	if !bytes.Contains(response, []byte("HTTP/1.1 200 OK")) {
		t.Fatalf("response = %q", response)
	}
	if !bytes.Contains(response, []byte("bifrost-ok")) {
		t.Fatalf("response body = %q", response)
	}
}

func originCert(t *testing.T) ([]byte, []byte) {
	t.Helper()
	return testutil.MakeTestCert(t, "home.example.com")
}

type staticTestAcceptProvider struct {
	token    string
	endpoint string
}

func (p staticTestAcceptProvider) Accept(_ context.Context, req bifrost.AcceptRequest) (bifrost.AcceptDecision, error) {
	if req.Headers[bifrost.TokenHeader] != p.token {
		return bifrost.AcceptDecision{Allow: false, Reason: "test provider rejected token"}, nil
	}
	return bifrost.AcceptDecision{
		Allow:       true,
		EndpointKey: p.endpoint,
		Limits:      bifrost.PlanLimits{MaxStreams: 10},
	}, nil
}
