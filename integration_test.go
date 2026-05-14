package caddybifrost

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"testing"

	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
	"github.com/tunely-eu/caddy-bifrost/internal/testutil"
)

func TestServerClientPassthroughRawTLSBySNI(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	passthroughAddr := testutil.FreeTCPAddr(t)

	server, client, _, originCA, stop := startServerClient(t, connectorAddr, passthroughAddr, true)
	defer stop()

	if server == nil || client == nil {
		t.Fatal("expected server and client")
	}
	response := testutil.WaitHTTPSResponse(t, passthroughAddr, "home.example.com", originCA)
	assertOKResponse(t, response)
}

func TestTransportProxiesHTTPOverBifrost(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	server, _, transport, _, stop := startServerClient(t, connectorAddr, "", false)
	defer stop()

	if server == nil || transport == nil {
		t.Fatal("expected server and transport")
	}
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

	first, firstClient, _, _, firstStop := startServerClient(t, connectorAddr, "", false)
	defer firstStop()
	if first == nil || firstClient == nil {
		t.Fatal("expected first runtime")
	}

	secondServer, secondClient, secondTransport, _, secondStop := startServerClient(t, connectorAddr, "", false)
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

func TestPassthroughSupportsReloadOverlap(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	passthroughAddr := testutil.FreeTCPAddr(t)

	first, firstClient, _, _, firstStop := startServerClient(t, connectorAddr, passthroughAddr, true)
	defer firstStop()
	if first == nil || firstClient == nil {
		t.Fatal("expected first runtime")
	}

	secondServer, secondClient, secondTransport, secondOriginCA, secondStop := startServerClient(t, connectorAddr, passthroughAddr, true)
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

	_ = secondTransport
	httpsResponse := testutil.WaitHTTPSResponse(t, passthroughAddr, "home.example.com", secondOriginCA)
	assertOKResponse(t, httpsResponse)
}

func startServerClient(t *testing.T, connectorAddr string, passthroughAddr string, passthrough bool) (*runtime.Server, *runtime.Client, http.RoundTripper, []byte, func()) {
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
	if passthrough {
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
	if passthrough {
		serverConfig.Passthrough = config.Passthrough{
			Listen: passthroughAddr,
			Routes: []config.SNIRoute{
				{ServerName: "home.example.com", Endpoint: "home"},
			},
		}
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
