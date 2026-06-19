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
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
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

func TestTransportRecordsEndpointByteMetrics(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	registry := prometheus.NewRegistry()
	observer, err := runtime.NewCaddyObserver(registry)
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}
	server, _, transport, _, stop := startServerClient(t, connectorAddr, false, runtime.WithObserver(observer))
	defer stop()

	if server == nil || transport == nil {
		t.Fatal("expected server and transport")
	}
	response := testutil.WaitHTTPTransportResponse(t, transport, "http://home/")
	assertOKResponse(t, response)
	waitPrometheusMetricPositive(t, registry, "bifrost_endpoint_stream_bytes_total", map[string]string{
		"endpoint_key": "home",
		"direction":    string(bifrost.DirectionIngressToEndpoint),
	})
	waitPrometheusMetricPositive(t, registry, "bifrost_endpoint_stream_bytes_total", map[string]string{
		"endpoint_key": "home",
		"direction":    string(bifrost.DirectionEndpointToIngress),
	})
}

func TestPassthroughRecordsEndpointByteMetrics(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	passthroughAddr := testutil.FreeTCPAddr(t)
	registry := prometheus.NewRegistry()
	observer, err := runtime.NewCaddyObserver(registry)
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}
	server, client, _, originCA, stop := startServerClient(t, connectorAddr, true, runtime.WithObserver(observer))
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
	waitPrometheusMetricPositive(t, registry, "bifrost_endpoint_stream_bytes_total", map[string]string{
		"endpoint_key": "home",
		"direction":    string(bifrost.DirectionIngressToEndpoint),
	})
	waitPrometheusMetricPositive(t, registry, "bifrost_endpoint_stream_bytes_total", map[string]string{
		"endpoint_key": "home",
		"direction":    string(bifrost.DirectionEndpointToIngress),
	})
}

func TestPassthroughStreamObserverRecordsStartAndEnd(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	passthroughAddr := testutil.FreeTCPAddr(t)
	server, client, _, originCA, stop := startServerClient(t, connectorAddr, true)
	defer stop()

	if server == nil || client == nil {
		t.Fatal("expected server and client")
	}
	stream := waitBifrostStream(t, server, "home")
	_ = stream.Close()
	observer := newRecordingPassthroughStreamObserver()
	resolver := &recordingPassthroughResolver{endpoint: "home", ok: true, key: "route-home"}
	server.SetPassthroughResolver(resolver)
	listener, err := net.Listen("tcp", passthroughAddr)
	if err != nil {
		t.Fatalf("listen passthrough test listener: %v", err)
	}
	defer listener.Close()
	wrapper := &ListenerWrapper{
		App:        "bifrost",
		ctx:        context.Background(),
		logger:     zap.NewNop(),
		bifrostApp: &App{runtime: server},
		observer:   observer,
	}
	serveWrappedPassthroughListener(listener, wrapper)

	response := testutil.WaitHTTPSResponse(t, passthroughAddr, "home.example.com", originCA)
	assertOKResponse(t, response)

	observations := waitPassthroughObservationEvent(t, observer, PassthroughStreamEnded)
	assertPassthroughObservation(t, observations[0], PassthroughStreamStarted, PassthroughStreamResultStarted, PassthroughStreamReasonNone)
	if len(observations) < 3 {
		t.Fatalf("expected usage delta between start and end, got %#v", observations)
	}
	var usageBytes int64
	for _, observation := range observations[1 : len(observations)-1] {
		if observation.EventType != PassthroughStreamUsageDelta {
			t.Fatalf("unexpected middle observation = %#v", observation)
		}
		usageBytes += observation.BytesIngressToEndpoint + observation.BytesEndpointToIngress
	}
	if usageBytes <= 0 {
		t.Fatalf("expected positive usage delta bytes, got %#v", observations)
	}
	assertPassthroughObservation(t, observations[len(observations)-1], PassthroughStreamEnded, PassthroughStreamResultEnded, PassthroughStreamReasonNone)
	if !observations[0].ObservedAt.Before(observations[1].ObservedAt) && !observations[0].ObservedAt.Equal(observations[1].ObservedAt) {
		t.Fatalf("observations out of order: %#v", observations)
	}
}

func TestPassthroughStreamObserverRecordsControlledReject(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	server, stop := startServerOnly(t, connectorAddr)
	defer stop()

	observer := newRecordingPassthroughStreamObserver()
	resolver := &recordingPassthroughResolver{endpoint: "home", ok: true, key: "route-home"}
	server.SetPassthroughResolver(resolver)
	wrapper := &ListenerWrapper{
		App:        "bifrost",
		ctx:        context.Background(),
		logger:     zap.NewNop(),
		bifrostApp: &App{runtime: server},
		observer:   observer,
	}
	serverConn, handshakeDone := tlsClientHelloConn(t, "home.example.com")

	replayConn, handled := wrapper.handleConnection(serverConn)
	if !handled {
		t.Fatal("expected connection to be handled by Bifrost")
	}
	if replayConn != nil {
		t.Fatal("expected no replay connection")
	}

	observations := waitPassthroughObservations(t, observer, 1)
	assertPassthroughObservation(t, observations[0], PassthroughStreamRejected, PassthroughStreamResultRejected, PassthroughStreamReasonNoSession)
	waitHandshakeError(t, handshakeDone)
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

	transport, err := runtime.NewTransport(config.Transport{}, server)
	if err != nil {
		t.Fatalf("new transport: %v", err)
	}
	defer transport.Cleanup()

	response := testutil.WaitHTTPTransportResponse(t, transport, "http://home/")
	assertOKResponse(t, response)
}

func TestTransportReturnsErrorWithoutActiveEndpoint(t *testing.T) {
	transport, err := runtime.NewTransport(config.Transport{}, nil)
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
	transport, err := runtime.NewTransport(config.Transport{}, nil)
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

func startServerClient(t *testing.T, connectorAddr string, originTLS bool, options ...runtime.ServerOption) (*runtime.Server, *runtime.Client, http.RoundTripper, []byte, func()) {
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
	server, err := runtime.NewServerWithTLSConfig(serverConfig, &tls.Config{Certificates: []tls.Certificate{bifrostKeyPair}}, zap.NewNop(), options...)
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

	transport, err := runtime.NewTransport(config.Transport{}, server)
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

func startServerOnly(t *testing.T, connectorAddr string, options ...runtime.ServerOption) (*runtime.Server, func()) {
	t.Helper()
	dir := t.TempDir()
	bifrostCert, bifrostKey, _ := testutil.WriteTestCertFiles(t, dir, "localhost")
	bifrostKeyPair, err := tls.LoadX509KeyPair(bifrostCert, bifrostKey)
	if err != nil {
		t.Fatalf("load bifrost cert: %v", err)
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
	server, err := runtime.NewServerWithTLSConfig(serverConfig, &tls.Config{Certificates: []tls.Certificate{bifrostKeyPair}}, zap.NewNop(), options...)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	return server, func() {
		_ = server.Stop()
	}
}

func serveWrappedPassthroughListener(listener net.Listener, wrapper *ListenerWrapper) {
	wrapped := wrapper.WrapListener(listener)
	go func() {
		for {
			conn, err := wrapped.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
}

func assertPassthroughObservation(t *testing.T, observation PassthroughStreamObservation, event PassthroughStreamEventType, result PassthroughStreamResult, reason PassthroughStreamReason) {
	t.Helper()
	if observation.EndpointKey != "home" {
		t.Fatalf("endpoint key = %q", observation.EndpointKey)
	}
	if observation.ObservationKey != "route-home" {
		t.Fatalf("observation key = %q", observation.ObservationKey)
	}
	if observation.EventType != event {
		t.Fatalf("event type = %q, want %q", observation.EventType, event)
	}
	if observation.Result != result {
		t.Fatalf("result = %q, want %q", observation.Result, result)
	}
	if observation.Reason != reason {
		t.Fatalf("reason = %q, want %q", observation.Reason, reason)
	}
	if observation.ObservedAt.IsZero() {
		t.Fatal("observed_at is zero")
	}
}

func assertPassthroughUsageDelta(t *testing.T, observation PassthroughStreamObservation, ingressToEndpoint int64, endpointToIngress int64) {
	t.Helper()
	assertPassthroughObservation(t, observation, PassthroughStreamUsageDelta, "", PassthroughStreamReasonNone)
	if observation.BytesIngressToEndpoint != ingressToEndpoint {
		t.Fatalf("bytes_ingress_to_endpoint = %d, want %d", observation.BytesIngressToEndpoint, ingressToEndpoint)
	}
	if observation.BytesEndpointToIngress != endpointToIngress {
		t.Fatalf("bytes_endpoint_to_ingress = %d, want %d", observation.BytesEndpointToIngress, endpointToIngress)
	}
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
		server bifrost.example.com {
			listen %s
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
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			_ = listener.Close()
			return
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected %s to be released: %v", addr, lastErr)
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

func waitPrometheusMetricPositive(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastValue float64
	for time.Now().Before(deadline) {
		value, ok, err := prometheusMetricValue(registry, name, labels)
		if err != nil {
			t.Fatalf("gather metrics: %v", err)
		}
		if ok {
			lastValue = value
			if value > 0 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("metric %s%v = %v, want > 0", name, labels, lastValue)
}

func prometheusMetricValue(registry *prometheus.Registry, name string, labels map[string]string) (float64, bool, error) {
	families, err := registry.Gather()
	if err != nil {
		return 0, false, err
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if !prometheusLabelsEqual(metric, labels) {
				continue
			}
			if metric.Gauge != nil {
				return metric.Gauge.GetValue(), true, nil
			}
			if metric.Counter != nil {
				return metric.Counter.GetValue(), true, nil
			}
		}
	}
	return 0, false, nil
}

func prometheusLabelsEqual(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}
	for _, label := range metric.GetLabel() {
		if labels[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
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
