package caddybifrost

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
	"github.com/tunely-eu/caddy-bifrost/internal/testutil"
)

func init() {
	caddy.RegisterModule(new(testPassiveLatencyObserverModule))
}

type testPassiveLatencyObserverModule struct{}

func (*testPassiveLatencyObserverModule) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "bifrost.passive_latency_observers.test",
		New: func() caddy.Module { return new(testPassiveLatencyObserverModule) },
	}
}

func (*testPassiveLatencyObserverModule) ObservePassiveLatency(context.Context, PassiveLatencyObservation) {
}

func TestPassiveLatencyObservationHasOnlyBoundedFields(t *testing.T) {
	allowed := map[string]struct{}{
		"EndpointKey": {},
		"LatencyMS":   {},
		"ObservedAt":  {},
		"State":       {},
	}
	observationType := reflect.TypeOf(PassiveLatencyObservation{})
	for i := 0; i < observationType.NumField(); i++ {
		field := observationType.Field(i)
		if _, ok := allowed[field.Name]; !ok {
			t.Fatalf("unexpected observation field %q", field.Name)
		}
	}
	if observationType.NumField() != len(allowed) {
		t.Fatalf("observation fields = %d, want %d", observationType.NumField(), len(allowed))
	}

	latencyMS := int64(18)
	observedAt := time.Date(2026, 6, 15, 10, 15, 30, 0, time.UTC)
	body, err := json.Marshal(PassiveLatencyObservation{
		EndpointKey: "home",
		LatencyMS:   &latencyMS,
		ObservedAt:  &observedAt,
		State:       PassiveLatencyOK,
	})
	if err != nil {
		t.Fatalf("marshal observation: %v", err)
	}
	for _, forbidden := range []string{
		"server_name",
		"sni",
		"hostname",
		"remote",
		"http",
		"header",
		"cookie",
		"body",
		"token",
		"private_key",
		"participant",
	} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("observation JSON contains forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestPassiveLatencyBridgeUnknownAndUnavailable(t *testing.T) {
	var nilApp *App
	nilObservation := nilApp.PassiveLatencyObservation(" home ", time.Now())
	assertPassiveLatencyUnknown(t, nilObservation, "home")
	if snapshot := nilApp.PassiveLatencySnapshot(time.Now()); len(snapshot) != 0 {
		t.Fatalf("nil app snapshot = %#v, want empty", snapshot)
	}

	unstarted := &App{runtime: &runtime.Server{}}
	unavailable := unstarted.PassiveLatencyObservation("home", time.Now())
	assertPassiveLatencyUnknown(t, unavailable, "home")
	if snapshot := unstarted.PassiveLatencySnapshot(time.Now()); len(snapshot) != 0 {
		t.Fatalf("unavailable snapshot = %#v, want empty", snapshot)
	}

	clientApp := &App{runtime: noopAppRuntime{}}
	clientObservation := clientApp.PassiveLatencyObservation("home", time.Now())
	assertPassiveLatencyUnknown(t, clientObservation, "home")
}

func TestPassiveLatencyBridgeFreshStaleAndUnknownObservation(t *testing.T) {
	_, app, stop := startPassiveLatencyServerClient(t, nil)
	defer stop()

	fresh := waitPassiveLatencyOK(t, app, "home")
	if fresh.EndpointKey != "home" {
		t.Fatalf("endpoint key = %q", fresh.EndpointKey)
	}
	if fresh.LatencyMS == nil || *fresh.LatencyMS < 0 {
		t.Fatalf("latency_ms = %#v", fresh.LatencyMS)
	}
	if fresh.ObservedAt == nil || fresh.ObservedAt.IsZero() {
		t.Fatalf("observed_at = %#v", fresh.ObservedAt)
	}

	snapshot := app.PassiveLatencySnapshot(time.Now())
	if len(snapshot) != 1 {
		t.Fatalf("snapshot = %#v, want one observation", snapshot)
	}
	if snapshot[0].EndpointKey != "home" || snapshot[0].State != PassiveLatencyOK {
		t.Fatalf("snapshot[0] = %#v", snapshot[0])
	}

	staleAt := fresh.ObservedAt.Add(2 * time.Second)
	stale := app.PassiveLatencyObservation("home", staleAt)
	if stale.State != PassiveLatencyStale {
		t.Fatalf("stale state = %q, want %q", stale.State, PassiveLatencyStale)
	}
	if stale.LatencyMS == nil || *stale.LatencyMS != *fresh.LatencyMS {
		t.Fatalf("stale latency_ms = %#v, fresh = %#v", stale.LatencyMS, fresh.LatencyMS)
	}
	if stale.ObservedAt == nil || !stale.ObservedAt.Equal(*fresh.ObservedAt) {
		t.Fatalf("stale observed_at = %#v, fresh = %#v", stale.ObservedAt, fresh.ObservedAt)
	}

	unknown := app.PassiveLatencyObservation("missing", time.Now())
	assertPassiveLatencyUnknown(t, unknown, "missing")
}

func TestPassiveLatencyObserverReceivesFreshObservation(t *testing.T) {
	observer := newRecordingPassiveLatencyObserver()
	_, _, stop := startPassiveLatencyServerClient(t, observer)
	defer stop()

	observations := waitPassiveLatencyObserver(t, observer, 1)
	observation := observations[0]
	if observation.EndpointKey != "home" {
		t.Fatalf("endpoint key = %q", observation.EndpointKey)
	}
	if observation.State != PassiveLatencyOK {
		t.Fatalf("state = %q, want %q", observation.State, PassiveLatencyOK)
	}
	if observation.LatencyMS == nil || *observation.LatencyMS < 0 {
		t.Fatalf("latency_ms = %#v", observation.LatencyMS)
	}
	if observation.ObservedAt == nil || observation.ObservedAt.IsZero() {
		t.Fatalf("observed_at = %#v", observation.ObservedAt)
	}
}

func TestPassiveLatencyEmbeddingModuleAccess(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	local_certs

	bifrost {
		server localhost {
			listen 127.0.0.1:0
			endpoint home {
				token secret
			}
		}
	}
}`)
	var cfg caddy.Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal caddy config: %v", err)
	}
	ctx, err := caddy.ProvisionContext(&cfg)
	if err != nil {
		t.Fatalf("ProvisionContext: %v", err)
	}

	consumer := &testPassiveLatencyConsumerModule{}
	if err := consumer.Provision(ctx); err != nil {
		t.Fatalf("consumer Provision: %v", err)
	}
	if consumer.snapshotter == nil {
		t.Fatal("expected passive latency snapshotter")
	}
	observation := consumer.snapshotter.PassiveLatencyObservation("home", time.Now())
	assertPassiveLatencyUnknown(t, observation, "home")
}

func TestAppProvisionLoadsPassiveLatencyObserverModule(t *testing.T) {
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

	app := &App{
		Server: &config.Server{
			Connector: config.Connector{
				Listen:     "127.0.0.1:0",
				TLSSubject: "localhost",
			},
		},
		AcceptProviderRaw:         json.RawMessage(`{"provider":"test","endpoint":"home"}`),
		PassiveLatencyObserverRaw: json.RawMessage(`{"observer":"test"}`),
	}
	if err := app.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if app.runtime == nil {
		t.Fatal("expected runtime")
	}
}

func TestAppProvisionRejectsPassiveLatencyObserverOnClientRuntime(t *testing.T) {
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

	app := &App{
		Client: &config.Client{
			Connect: "public.example.com",
			Token:   "secret",
			Forward: "127.0.0.1:8080",
		},
		PassiveLatencyObserverRaw: json.RawMessage(`{"observer":"test"}`),
	}
	err = app.Provision(ctx)
	if err == nil {
		t.Fatal("expected passive latency observer server-runtime error")
	}
	if !strings.Contains(err.Error(), "requires server runtime") {
		t.Fatalf("error = %v", err)
	}
}

func TestPassiveLatencyBridgeUnusedCompatibility(t *testing.T) {
	connectorAddr := testutil.FreeTCPAddr(t)
	server, _, transport, _, stop := startServerClient(t, connectorAddr, false)
	defer stop()

	app := &App{runtime: server}
	if snapshot := app.PassiveLatencySnapshot(time.Now()); len(snapshot) != 0 {
		t.Fatalf("initial passive latency snapshot = %#v, want empty", snapshot)
	}

	response := testutil.WaitHTTPTransportResponse(t, transport, "http://home/")
	assertOKResponse(t, response)
}

type testPassiveLatencyConsumerModule struct {
	snapshotter PassiveLatencySnapshotter
}

func (*testPassiveLatencyConsumerModule) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "bifrost.passive_latency_consumers.test",
		New: func() caddy.Module { return new(testPassiveLatencyConsumerModule) },
	}
}

func (m *testPassiveLatencyConsumerModule) Provision(ctx caddy.Context) error {
	app, err := ctx.App("bifrost")
	if err != nil {
		return err
	}
	snapshotter, ok := app.(PassiveLatencySnapshotter)
	if !ok {
		return fmt.Errorf("bifrost app has unexpected passive latency type %T", app)
	}
	m.snapshotter = snapshotter
	return nil
}

type recordingPassiveLatencyObserver struct {
	mu           sync.Mutex
	observations []PassiveLatencyObservation
	notify       chan struct{}
}

func newRecordingPassiveLatencyObserver() *recordingPassiveLatencyObserver {
	return &recordingPassiveLatencyObserver{notify: make(chan struct{}, 16)}
}

func (o *recordingPassiveLatencyObserver) ObservePassiveLatency(_ context.Context, observation PassiveLatencyObservation) {
	o.mu.Lock()
	o.observations = append(o.observations, observation)
	o.mu.Unlock()
	select {
	case o.notify <- struct{}{}:
	default:
	}
}

func (o *recordingPassiveLatencyObserver) snapshot() []PassiveLatencyObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]PassiveLatencyObservation, len(o.observations))
	copy(out, o.observations)
	return out
}

func waitPassiveLatencyObserver(t *testing.T, observer *recordingPassiveLatencyObserver, count int) []PassiveLatencyObservation {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		observations := observer.snapshot()
		if len(observations) >= count {
			return observations
		}
		select {
		case <-observer.notify:
		case <-deadline:
			t.Fatalf("timed out waiting for %d passive latency observations, got %#v", count, observations)
		}
	}
}

func waitPassiveLatencyOK(t *testing.T, app *App, endpoint string) PassiveLatencyObservation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last PassiveLatencyObservation
	for time.Now().Before(deadline) {
		last = app.PassiveLatencyObservation(endpoint, time.Now())
		if last.State == PassiveLatencyOK && last.LatencyMS != nil && last.ObservedAt != nil {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for passive latency observation, last = %#v", last)
	return PassiveLatencyObservation{}
}

func startPassiveLatencyServerClient(t *testing.T, observer PassiveLatencyObserver) (*runtime.Server, *App, func()) {
	t.Helper()
	connectorAddr := testutil.FreeTCPAddr(t)
	originAddr, stopOrigin := testutil.StartHTTPOrigin(t)
	dir := t.TempDir()
	bifrostCert, bifrostKey, bifrostCA := testutil.WriteTestCertFiles(t, dir, "localhost")
	bifrostKeyPair, err := tls.LoadX509KeyPair(bifrostCert, bifrostKey)
	if err != nil {
		stopOrigin()
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
		Runtime: config.Runtime{
			TunnelKeepAliveInterval: caddy.Duration(20 * time.Millisecond),
			TunnelKeepAliveTimeout:  caddy.Duration(500 * time.Millisecond),
		},
	}
	options := []runtime.ServerOption(nil)
	if observer != nil {
		options = append(options, runtime.WithPassiveLatencyObserver(newPassiveLatencyObserverAdapter(context.Background(), observer)))
	}
	server, err := runtime.NewServerWithTLSConfig(serverConfig, &tls.Config{Certificates: []tls.Certificate{bifrostKeyPair}}, zap.NewNop(), options...)
	if err != nil {
		stopOrigin()
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err != nil {
		stopOrigin()
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
		_ = server.Stop()
		stopOrigin()
		t.Fatalf("new client: %v", err)
	}
	if err := client.Start(); err != nil {
		_ = server.Stop()
		stopOrigin()
		t.Fatalf("client start: %v", err)
	}

	app := &App{runtime: server}
	return server, app, func() {
		_ = client.Stop()
		_ = server.Stop()
		stopOrigin()
	}
}

func assertPassiveLatencyUnknown(t *testing.T, observation PassiveLatencyObservation, endpointKey string) {
	t.Helper()
	if observation.EndpointKey != endpointKey {
		t.Fatalf("endpoint key = %q, want %q", observation.EndpointKey, endpointKey)
	}
	if observation.State != PassiveLatencyUnknown {
		t.Fatalf("state = %q, want %q", observation.State, PassiveLatencyUnknown)
	}
	if observation.LatencyMS != nil {
		t.Fatalf("unknown latency_ms = %#v, want nil", observation.LatencyMS)
	}
	if observation.ObservedAt != nil {
		t.Fatalf("unknown observed_at = %#v, want nil", observation.ObservedAt)
	}
}

type noopAppRuntime struct{}

func (noopAppRuntime) Start() error { return nil }
func (noopAppRuntime) Stop() error  { return nil }

var (
	_ PassiveLatencyObserverModule = (*testPassiveLatencyObserverModule)(nil)
	_ caddy.Provisioner            = (*testPassiveLatencyConsumerModule)(nil)
)
