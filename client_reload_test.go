package caddybifrost

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
	"github.com/tunely-eu/caddy-bifrost/internal/testutil"
)

func TestCaddyReloadReusesUnchangedClientRuntimeAndPreservesStream(t *testing.T) {
	_ = caddy.Stop()
	t.Cleanup(func() {
		_ = caddy.Stop()
	})

	connectorAddr := testutil.FreeTCPAddr(t)
	httpAddr := testutil.FreeTCPAddr(t)
	originAddr, stopOrigin := startEchoOrigin(t)
	defer stopOrigin()

	server, observer, caFile, stopServer := startReloadTestServer(t, connectorAddr, map[string]string{"test-token": "home"})
	defer stopServer()

	storageDir := t.TempDir()
	firstConfig := caddyReloadClientConfig(t, connectorAddr, httpAddr, storageDir, originAddr, caFile, "test-token", "first")
	secondConfig := caddyReloadClientConfig(t, connectorAddr, httpAddr, storageDir, originAddr, caFile, "test-token", "second")

	if err := caddy.Load(firstConfig, true); err != nil {
		t.Fatalf("first caddy load: %v", err)
	}
	stream := waitBifrostStream(t, server, "home")
	defer stream.Close()
	assertEcho(t, stream, "before reload")
	observer.WaitSessionStarts(t, 1)

	if err := caddy.Load(secondConfig, true); err != nil {
		t.Fatalf("caddy reload with unchanged client identity: %v", err)
	}
	assertEcho(t, stream, "after reload")

	observer.AssertSessionCounts(t, 1, 0)
}

func TestCaddyReloadReconnectsWhenClientTokenChanges(t *testing.T) {
	_ = caddy.Stop()
	t.Cleanup(func() {
		_ = caddy.Stop()
	})

	connectorAddr := testutil.FreeTCPAddr(t)
	httpAddr := testutil.FreeTCPAddr(t)
	originAddr, stopOrigin := startEchoOrigin(t)
	defer stopOrigin()

	server, observer, caFile, stopServer := startReloadTestServer(t, connectorAddr, map[string]string{
		"test-token-one": "home",
		"test-token-two": "home",
	})
	defer stopServer()

	storageDir := t.TempDir()
	firstConfig := caddyReloadClientConfig(t, connectorAddr, httpAddr, storageDir, originAddr, caFile, "test-token-one", "first")
	secondConfig := caddyReloadClientConfig(t, connectorAddr, httpAddr, storageDir, originAddr, caFile, "test-token-two", "second")

	if err := caddy.Load(firstConfig, true); err != nil {
		t.Fatalf("first caddy load: %v", err)
	}
	oldStream := waitBifrostStream(t, server, "home")
	defer oldStream.Close()
	assertEcho(t, oldStream, "before token rotation")
	observer.WaitSessionStarts(t, 1)

	if err := caddy.Load(secondConfig, true); err != nil {
		t.Fatalf("caddy reload with changed client token: %v", err)
	}
	observer.WaitSessionStarts(t, 2)
	observer.WaitSessionEnds(t, 1)

	newStream := waitBifrostStream(t, server, "home")
	defer newStream.Close()
	assertEcho(t, newStream, "after token rotation")
	assertStreamClosed(t, oldStream)
}

func TestClientRuntimeIdentityKeepsTokenInMemoryOnly(t *testing.T) {
	_, identity, err := normalizedClientRuntimeIdentity(&config.Client{
		Connect:       "public.example.com",
		Token:         "test-token-value",
		Forward:       "127.0.0.1:8080",
		TLSCAFile:     "/certs/ca.crt",
		TLSServerName: "public.example.com",
	})
	if err != nil {
		t.Fatalf("normalizedClientRuntimeIdentity: %v", err)
	}
	if identity.Token != "test-token-value" {
		t.Fatalf("identity token = %q", identity.Token)
	}
	if identity.Fingerprint() == "test-token-value" {
		t.Fatal("runtime fingerprint exposed raw token")
	}
}

func TestClientRuntimeIdentityAllowsDefaultTLSConfig(t *testing.T) {
	_, identity, err := normalizedClientRuntimeIdentity(&config.Client{
		Connect: "public.example.com",
		Token:   "test-token-value",
		Forward: "127.0.0.1:8080",
	})
	if err != nil {
		t.Fatalf("normalizedClientRuntimeIdentity: %v", err)
	}
	if identity.Connect != "public.example.com:8443" {
		t.Fatalf("connect = %q", identity.Connect)
	}
	if identity.TLSCAFile != "" || identity.TLSServerName != "" || identity.TLSInsecureSkipVerify {
		t.Fatalf("tls identity fields = %#v", identity)
	}
}

func TestClientRuntimeIdentityCoversRestartAffectingFields(t *testing.T) {
	base := clientIdentityFingerprint(t, &config.Client{
		Connect:       "public.example.com",
		Token:         "test-token-value",
		Forward:       "127.0.0.1:8080",
		TLSCAFile:     "/certs/ca.crt",
		TLSServerName: "public.example.com",
	})
	explicitDefaultPort := clientIdentityFingerprint(t, &config.Client{
		Connect:       "public.example.com:8443",
		Token:         "test-token-value",
		Forward:       "127.0.0.1:8080",
		TLSCAFile:     "/certs/ca.crt",
		TLSServerName: "public.example.com",
	})
	if base != explicitDefaultPort {
		t.Fatalf("default port identity = %q, explicit port identity = %q", base, explicitDefaultPort)
	}

	for name, cfg := range map[string]*config.Client{
		"connect": {
			Connect:       "other.example.com",
			Token:         "test-token-value",
			Forward:       "127.0.0.1:8080",
			TLSCAFile:     "/certs/ca.crt",
			TLSServerName: "public.example.com",
		},
		"token": {
			Connect:       "public.example.com",
			Token:         "test-token-rotated",
			Forward:       "127.0.0.1:8080",
			TLSCAFile:     "/certs/ca.crt",
			TLSServerName: "public.example.com",
		},
		"forward": {
			Connect:       "public.example.com",
			Token:         "test-token-value",
			Forward:       "127.0.0.1:9090",
			TLSCAFile:     "/certs/ca.crt",
			TLSServerName: "public.example.com",
		},
		"tls_ca_file": {
			Connect:       "public.example.com",
			Token:         "test-token-value",
			Forward:       "127.0.0.1:8080",
			TLSCAFile:     "/certs/other-ca.crt",
			TLSServerName: "public.example.com",
		},
		"tls_server_name": {
			Connect:       "public.example.com",
			Token:         "test-token-value",
			Forward:       "127.0.0.1:8080",
			TLSCAFile:     "/certs/ca.crt",
			TLSServerName: "connector.example.com",
		},
		"tls_insecure_skip_verify": {
			Connect:               "public.example.com",
			Token:                 "test-token-value",
			Forward:               "127.0.0.1:8080",
			TLSCAFile:             "/certs/ca.crt",
			TLSServerName:         "public.example.com",
			TLSInsecureSkipVerify: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := clientIdentityFingerprint(t, cfg); got == base {
				t.Fatalf("identity fingerprint did not change for %s", name)
			}
		})
	}
}

func TestClientRuntimeRegistryAcquireFailureKeepsExistingRuntime(t *testing.T) {
	registry := newClientRuntimeRegistry()
	existing := clientRuntimeIdentity{Connect: "public.example.com:8443", Forward: "127.0.0.1:8080", Token: "test-token"}
	release, _, _, err := registry.acquire(existing, func() (*runtime.Client, error) {
		return &runtime.Client{}, nil
	})
	if err != nil {
		t.Fatalf("acquire existing runtime: %v", err)
	}
	defer release()

	replacement := clientRuntimeIdentity{Connect: "other.example.com:8443", Forward: "127.0.0.1:8080", Token: "test-token"}
	if _, _, _, err := registry.acquire(replacement, func() (*runtime.Client, error) {
		return nil, errors.New("replacement start failed")
	}); err == nil {
		t.Fatal("expected replacement acquire failure")
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.entries[existing] == nil || registry.entries[existing].refs != 1 {
		t.Fatalf("existing runtime entry = %#v", registry.entries[existing])
	}
	if registry.entries[replacement] != nil {
		t.Fatalf("replacement runtime entry = %#v", registry.entries[replacement])
	}
}

func clientIdentityFingerprint(t *testing.T, cfg *config.Client) string {
	t.Helper()
	_, identity, err := normalizedClientRuntimeIdentity(cfg)
	if err != nil {
		t.Fatalf("normalizedClientRuntimeIdentity: %v", err)
	}
	return identity.Fingerprint()
}

func startReloadTestServer(t *testing.T, connectorAddr string, tokens map[string]string) (*runtime.Server, *reloadSessionObserver, string, func()) {
	t.Helper()
	dir := t.TempDir()
	bifrostCert, bifrostKey, bifrostCA := testutil.WriteTestCertFiles(t, dir, "localhost")
	bifrostKeyPair, err := tls.LoadX509KeyPair(bifrostCert, bifrostKey)
	if err != nil {
		t.Fatalf("load bifrost cert: %v", err)
	}

	observer := newReloadSessionObserver()
	server, err := runtime.NewServerWithTLSConfig(
		&config.Server{
			Connector: config.Connector{
				Listen:     connectorAddr,
				TLSSubject: "localhost",
			},
		},
		&tls.Config{Certificates: []tls.Certificate{bifrostKeyPair}},
		zap.NewNop(),
		runtime.WithAcceptProvider(tokenMapAcceptProvider{tokens: tokens}),
		runtime.WithObserver(observer),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	return server, observer, bifrostCA, func() {
		_ = server.Stop()
	}
}

func caddyReloadClientConfig(t *testing.T, connectorAddr, httpAddr, storageDir, originAddr, caFile, token, response string) []byte {
	t.Helper()
	tlsBlock := ""
	if caFile != "" {
		tlsBlock = fmt.Sprintf(`
			tls {
				ca_file %s
				server_name localhost
			}`, caFile)
	}
	return adaptCaddyfile(t, fmt.Sprintf(`{
	admin off
	storage file_system {
		root %s
	}
	bifrost {
		client %s {
			token %s
			forward %s%s
		}
	}
}

http://reload.example.com:%s {
	bind 127.0.0.1
	respond %q
}`, storageDir, connectorAddr, token, originAddr, tlsBlock, tcpPort(t, httpAddr), response))
}

func waitBifrostStream(t *testing.T, server *runtime.Server, endpoint string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		stream, err := server.OpenStream(ctx, endpoint)
		cancel()
		if err == nil {
			return stream
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("wait bifrost stream for %q: %v", endpoint, lastErr)
	return nil
}

func assertEcho(t *testing.T, conn net.Conn, message string) {
	t.Helper()
	payload := []byte(message)
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write echo payload: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo payload: %v", err)
	}
	if string(got) != message {
		t.Fatalf("echo = %q, want %q", string(got), message)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("clear deadline: %v", err)
	}
}

func assertStreamClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		_ = conn.SetDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := conn.Write([]byte("old")); err != nil {
			_ = conn.SetDeadline(time.Time{})
			return
		}
		buf := make([]byte, 3)
		_, err := io.ReadFull(conn, buf)
		if err != nil {
			_ = conn.SetDeadline(time.Time{})
			return
		}
		lastErr = errors.New("old stream still echoed data")
		time.Sleep(50 * time.Millisecond)
	}
	_ = conn.SetDeadline(time.Time{})
	t.Fatalf("expected old stream to close after reconnect: %v", lastErr)
}

func startEchoOrigin(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer conn.Close()
				done := make(chan struct{})
				go func() {
					select {
					case <-ctx.Done():
						_ = conn.Close()
					case <-done:
					}
				}()
				_, _ = io.Copy(conn, conn)
				close(done)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		cancel()
		_ = ln.Close()
		wg.Wait()
	}
}

type tokenMapAcceptProvider struct {
	tokens map[string]string
}

func (p tokenMapAcceptProvider) Accept(_ context.Context, req bifrost.AcceptRequest) (bifrost.AcceptDecision, error) {
	endpoint, ok := p.tokens[req.Headers[bifrost.TokenHeader]]
	if !ok {
		return bifrost.AcceptDecision{Allow: false, Reason: "test provider rejected token"}, nil
	}
	return bifrost.AcceptDecision{
		Allow:       true,
		EndpointKey: endpoint,
		ConnectionPolicy: bifrost.ConnectionPolicy{
			Mode: bifrost.PolicyReplaceExisting,
		},
		Limits: bifrost.PlanLimits{MaxStreams: 10},
	}, nil
}

type reloadSessionObserver struct {
	mu            sync.Mutex
	sessionStarts int
	sessionEnds   int
}

func newReloadSessionObserver() *reloadSessionObserver {
	return &reloadSessionObserver{}
}

func (o *reloadSessionObserver) Ready(bool) {}

func (o *reloadSessionObserver) ConnectionAttempted() {}

func (o *reloadSessionObserver) ConnectionRejected(string) {}

func (o *reloadSessionObserver) SessionStarted(string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessionStarts++
}

func (o *reloadSessionObserver) SessionEnded(string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessionEnds++
}

func (o *reloadSessionObserver) StreamStarted(string) bifrost.StreamObserver {
	return bifrost.NoopStreamObserver{}
}

func (o *reloadSessionObserver) StreamRejected(string, string) {}

func (o *reloadSessionObserver) WaitSessionStarts(t *testing.T, want int) {
	t.Helper()
	o.wait(t, func(starts, _ int) bool { return starts >= want }, "session starts", want)
}

func (o *reloadSessionObserver) WaitSessionEnds(t *testing.T, want int) {
	t.Helper()
	o.wait(t, func(_, ends int) bool { return ends >= want }, "session ends", want)
}

func (o *reloadSessionObserver) AssertSessionCounts(t *testing.T, wantStarts, wantEnds int) {
	t.Helper()
	starts, ends := o.counts()
	if starts != wantStarts || ends != wantEnds {
		t.Fatalf("sessions started=%d ended=%d, want started=%d ended=%d", starts, ends, wantStarts, wantEnds)
	}
}

func (o *reloadSessionObserver) wait(t *testing.T, ready func(starts, ends int) bool, label string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var starts, ends int
	for time.Now().Before(deadline) {
		starts, ends = o.counts()
		if ready(starts, ends) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("wait %s >= %d timed out: starts=%d ends=%d", label, want, starts, ends)
}

func (o *reloadSessionObserver) counts() (int, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sessionStarts, o.sessionEnds
}

var _ bifrost.Observer = (*reloadSessionObserver)(nil)
