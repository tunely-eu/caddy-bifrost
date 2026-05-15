package caddybifrost

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
)

func TestAppUnmarshalCaddyfileServer(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	server {
		connector :8443 {
			tls public.example.com
			endpoint home {
				token secret
				policy allow_parallel
				max_parallel 2
				limits {
					max_streams 100
					max_bandwidth_bps 25000000
					stream_idle_timeout 5m
				}
			}
		}
		guardrails {
			max_sessions 1000
			max_streams_per_session 512
			max_bandwidth_bps_per_session 100000000
			min_stream_idle_timeout 30s
			max_stream_idle_timeout 1h
			max_headers 32
			max_header_bytes 8192
		}
		runtime {
			handshake_timeout 10s
			stream_copy_buffer_bytes 32768
			tunnel_keepalive_interval 30s
			tunnel_keepalive_timeout 10s
		}
	}
}`)
	var app App
	if err := app.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if app.Server == nil {
		t.Fatal("expected server runtime")
	}
	server := app.Server
	if server.Connector.Listen != ":8443" {
		t.Fatalf("connector listen = %q", server.Connector.Listen)
	}
	if server.Connector.TLSSubject != "public.example.com" {
		t.Fatalf("tls subject = %q", server.Connector.TLSSubject)
	}
	if len(server.Connector.Endpoints) != 1 {
		t.Fatalf("endpoints = %d", len(server.Connector.Endpoints))
	}
	endpoint := server.Connector.Endpoints[0]
	if endpoint.Key != "home" || endpoint.Token != "secret" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
	if endpoint.Policy != "allow_parallel" || endpoint.MaxParallel != 2 {
		t.Fatalf("endpoint policy = %#v", endpoint)
	}
	if endpoint.Limits.MaxStreams != 100 || endpoint.Limits.MaxBandwidthBPS != 25000000 {
		t.Fatalf("endpoint limits = %#v", endpoint.Limits)
	}
	if time.Duration(endpoint.Limits.StreamIdleTimeout) != 5*time.Minute {
		t.Fatalf("stream idle timeout = %s", time.Duration(endpoint.Limits.StreamIdleTimeout))
	}
	if server.Guardrails.MaxHeaderBytes != 8192 || time.Duration(server.Guardrails.MaxStreamIdleTimeout) != time.Hour {
		t.Fatalf("guardrails = %#v", server.Guardrails)
	}
	if server.Runtime.StreamCopyBufferBytes != 32768 || time.Duration(server.Runtime.HandshakeTimeout) != 10*time.Second {
		t.Fatalf("runtime = %#v", server.Runtime)
	}
}

func TestAppUnmarshalCaddyfileClient(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	client {
		connect public.example.com:8443
		token secret
		forward 127.0.0.1:8080
		tls_ca_file /certs/ca.crt
		tls_server_name public.example.com
		tls_insecure_skip_verify
	}
}`)
	var app App
	if err := app.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if app.Client == nil {
		t.Fatal("expected client runtime")
	}
	client := app.Client
	if client.Connect != "public.example.com:8443" {
		t.Fatalf("connect = %q", client.Connect)
	}
	if client.Token != "secret" {
		t.Fatalf("token = %q", client.Token)
	}
	if client.Forward != "127.0.0.1:8080" {
		t.Fatalf("forward = %q", client.Forward)
	}
	if client.TLSCAFile != "/certs/ca.crt" || client.TLSServerName != "public.example.com" {
		t.Fatalf("tls = %q %q", client.TLSCAFile, client.TLSServerName)
	}
	if !client.TLSInsecureSkipVerify {
		t.Fatal("expected insecure skip verify")
	}
}

func TestTransportUnmarshalCaddyfile(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	endpoint home
	app bifrost
	dial_timeout 2s
}`)
	var transport Transport
	if err := transport.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if transport.Endpoint != "home" || transport.App != "bifrost" {
		t.Fatalf("transport = %#v", transport)
	}
	if time.Duration(transport.DialTimeout) != 2*time.Second {
		t.Fatalf("dial timeout = %s", time.Duration(transport.DialTimeout))
	}
}

func TestTransportUnmarshalCaddyfileRejectsPassthrough(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	endpoint home
	passthrough enable
}`)
	var transport Transport
	if err := transport.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected passthrough transport option to be rejected")
	}
}

func TestAppUnmarshalCaddyfileRejectsLegacyPassthrough(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	server {
		connector :8443 {
			tls public.example.com
			endpoint home {
				token secret
			}
		}
		passthrough :443 {
			route_sni home.example.com home
		}
	}
}`)
	var app App
	if err := app.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected legacy app passthrough option to be rejected")
	}
}

func TestCaddyfileAdaptServerAppAndTransport(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	servers :443 {
		listener_wrappers {
			bifrost {
				route_sni home.example.com home
			}
			tls
		}
	}
	bifrost {
		server {
			connector :8443 {
				tls public.example.com
				endpoint home {
					token secret
					policy replace_existing
					limits {
						max_streams 100
						max_bandwidth_bps 25000000
						stream_idle_timeout 5m
					}
				}
			}
		}
	}
}

media.example.com {
	reverse_proxy http://home {
		transport bifrost {
			endpoint home
		}
	}
}`)
	assertBifrostConfig(t, configJSON)
	var cfg struct {
		Apps struct {
			Bifrost App `json:"bifrost"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal adapted config: %v", err)
	}
	if cfg.Apps.Bifrost.Server == nil || cfg.Apps.Bifrost.Client != nil {
		t.Fatalf("runtime = %#v", cfg.Apps.Bifrost)
	}
	if cfg.Apps.Bifrost.Server.Connector.Endpoints[0].Key != "home" {
		t.Fatalf("server endpoint = %#v", cfg.Apps.Bifrost.Server.Connector.Endpoints[0])
	}
	if !bytes.Contains(configJSON, []byte(`"protocol":"bifrost"`)) {
		t.Fatalf("adapted config does not include bifrost transport: %s", configJSON)
	}
	if !bytes.Contains(configJSON, []byte(`"server_name":"home.example.com"`)) ||
		!bytes.Contains(configJSON, []byte(`"endpoint":"home"`)) {
		t.Fatalf("adapted config does not include bifrost listener routes: %s", configJSON)
	}
	if !bytes.Contains(configJSON, []byte(`"wrapper":"bifrost"`)) || !bytes.Contains(configJSON, []byte(`"wrapper":"tls"`)) {
		t.Fatalf("adapted config does not include listener wrappers: %s", configJSON)
	}
}

func TestCaddyfileAdaptClientApp(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	bifrost {
		client {
			connect public.example.com:8443
			token secret
			forward 127.0.0.1:8080
		}
	}
}`)
	assertBifrostConfig(t, configJSON)
	var cfg struct {
		Apps struct {
			Bifrost App `json:"bifrost"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal adapted config: %v", err)
	}
	if cfg.Apps.Bifrost.Client == nil || cfg.Apps.Bifrost.Server != nil {
		t.Fatalf("runtime = %#v", cfg.Apps.Bifrost)
	}
	if cfg.Apps.Bifrost.Client.Token != "secret" {
		t.Fatalf("client = %#v", cfg.Apps.Bifrost.Client)
	}
}

func TestRemovedDirectivesAreRejected(t *testing.T) {
	for _, input := range []string{
		`bifrost { server { connectors :8443 {} } }`,
		`bifrost { server { connector :8443 { client home { token secret } } } }`,
		`bifrost { client { endpoint home } }`,
	} {
		d := caddyfile.NewTestDispenser(input)
		var app App
		if err := app.UnmarshalCaddyfile(d); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
}

func TestAppRejectsMultipleRuntimes(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	server {}
client {}
}`)
	var app App
	if err := app.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected multiple runtime error")
	}
}

func TestAppRuntimeValidation(t *testing.T) {
	if _, err := (&App{}).runtimeName(); err == nil {
		t.Fatal("expected missing runtime error")
	}
	if _, err := (&App{Server: appServerConfig(), Client: appClientConfig()}).runtimeName(); err == nil {
		t.Fatal("expected multiple runtime error")
	}
}

func TestPrepareValidationErrors(t *testing.T) {
	if err := appServerConfig().Validate(); err != nil {
		t.Fatalf("valid server config: %v", err)
	}
	if err := (&App{Server: appServerConfig()}).Start(); err == nil {
		t.Fatal("expected start before provision error")
	}
	if err := appClientConfig().Validate(); err != nil {
		t.Fatalf("valid client config: %v", err)
	}
}

func TestServerProvisionUsesCaddyTLSApp(t *testing.T) {
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
	app := &App{Server: appServerConfig()}
	if err := app.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if app.runtime == nil {
		t.Fatal("expected runtime")
	}
}

func adaptCaddyfile(t *testing.T, input string) []byte {
	t.Helper()
	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		t.Fatal("caddyfile adapter is not registered")
	}
	configJSON, _, err := adapter.Adapt([]byte(input), nil)
	if err != nil {
		t.Fatalf("adapt caddyfile: %v", err)
	}
	return configJSON
}

func assertBifrostConfig(t *testing.T, configJSON []byte) {
	t.Helper()
	if !bytes.Contains(configJSON, []byte(`"bifrost"`)) {
		t.Fatalf("adapted config does not include bifrost app: %s", configJSON)
	}
	if bytes.Contains(configJSON, []byte(`"edge"`)) || bytes.Contains(configJSON, []byte(`"agent"`)) {
		t.Fatalf("adapted config contains removed names: %s", configJSON)
	}
}

func appServerConfig() *config.Server {
	return &config.Server{
		Connector: config.Connector{
			TLSSubject: "localhost",
			Endpoints:  []config.Endpoint{{Key: "home", Token: "secret"}},
		},
	}
}

func appClientConfig() *config.Client {
	return &config.Client{
		Connect: "public.example.com:8443",
		Token:   "secret",
		Forward: "127.0.0.1:8080",
	}
}
