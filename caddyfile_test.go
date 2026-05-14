package caddybifrost

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"github.com/tunely-eu/bifrost"
)

func TestAppUnmarshalCaddyfileServer(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	server {
		connectors :8443 {
			tls public.example.com
			client home {
				token secret
				policy replace_existing
				max_streams 100
			}
		}
		passthrough :443 {
			route_sni home.example.com home
			route_sni files.example.com home
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
	if server.ConnectorListen != ":8443" {
		t.Fatalf("connector listen = %q", server.ConnectorListen)
	}
	if server.TLSSubject != "public.example.com" {
		t.Fatalf("tls subject = %q", server.TLSSubject)
	}
	if server.Passthrough != ":443" {
		t.Fatalf("passthrough = %q", server.Passthrough)
	}
	if len(server.Clients) != 1 {
		t.Fatalf("clients = %d", len(server.Clients))
	}
	if server.Clients[0].Endpoint != "home" || server.Clients[0].Token != "secret" {
		t.Fatalf("client = %#v", server.Clients[0])
	}
	if server.Clients[0].Policy != "replace_existing" || server.Clients[0].MaxStreams != 100 {
		t.Fatalf("client policy/limits = %#v", server.Clients[0])
	}
	if len(server.Routes) != 2 {
		t.Fatalf("routes = %d", len(server.Routes))
	}
}

func TestAppUnmarshalCaddyfileClient(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	client {
		connect public.example.com:8443
		token secret
		endpoint home
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
	if client.Token != "secret" || client.Endpoint != "home" {
		t.Fatalf("identity = %q %q", client.Token, client.Endpoint)
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

func TestCaddyfileAdaptServerAppAndTransport(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	bifrost {
		server {
			connectors :8443 {
				tls public.example.com
				client home {
					token secret
					policy replace_existing
					max_streams 100
				}
			}
			passthrough :443 {
				route_sni home.example.com home
			}
		}
	}
}

home.example.com {
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
	if cfg.Apps.Bifrost.Server.Clients[0].Endpoint != "home" {
		t.Fatalf("server client = %#v", cfg.Apps.Bifrost.Server.Clients[0])
	}
	if !bytes.Contains(configJSON, []byte(`"protocol":"bifrost"`)) {
		t.Fatalf("adapted config does not include bifrost transport: %s", configJSON)
	}
}

func TestCaddyfileAdaptClientApp(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	bifrost {
		client {
			connect public.example.com:8443
			token secret
			endpoint home
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

func TestAppRejectsRemovedRuntimeNames(t *testing.T) {
	for _, input := range []string{
		`bifrost { edge {} }`,
		`bifrost { agent {} }`,
	} {
		d := caddyfile.NewTestDispenser(input)
		var app App
		if err := app.UnmarshalCaddyfile(d); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
}

func TestAppRuntimeValidation(t *testing.T) {
	if _, err := (&App{}).runtime(); err == nil {
		t.Fatal("expected missing runtime error")
	}
	if _, err := (&App{Server: &Server{}, Client: &Client{}}).runtime(); err == nil {
		t.Fatal("expected multiple runtime error")
	}
}

func TestPrepareValidationErrors(t *testing.T) {
	if err := (&Server{}).prepare(caddy.Context{}); err == nil {
		t.Fatal("expected server validation error")
	}
	if err := (&Client{}).prepare(); err == nil {
		t.Fatal("expected client validation error")
	}
	server := &Server{
		TLSConfig:   &tls.Config{},
		Clients:     []ClientAuth{{Endpoint: "home", Token: "secret"}},
		Passthrough: ":443",
		Routes: []SNIRoute{
			{ServerName: "home.example.com", Endpoint: "home"},
			{ServerName: "HOME.EXAMPLE.COM.", Endpoint: "home"},
		},
	}
	if err := server.prepare(caddy.Context{}); err == nil {
		t.Fatal("expected duplicate route error")
	}
}

func TestServerPrepareUsesCaddyTLSApp(t *testing.T) {
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
	server := &Server{
		ConnectorListen: "127.0.0.1:0",
		TLSSubject:      "localhost",
		Clients:         []ClientAuth{{Endpoint: "home", Token: "secret"}},
	}
	if err := server.prepare(ctx); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if server.TLSConfig == nil {
		t.Fatal("expected caddy-managed TLS config")
	}
	if !containsString(server.TLSConfig.NextProtos, bifrost.ALPN) {
		t.Fatalf("connector ALPN missing from %v", server.TLSConfig.NextProtos)
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
	if bytes.Contains(configJSON, []byte(`"edge":`)) || bytes.Contains(configJSON, []byte(`"agent":`)) || bytes.Contains(configJSON, []byte(`"ingress":`)) {
		t.Fatalf("adapted config contains removed names: %s", configJSON)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
