package caddybifrost

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestAppUnmarshalCaddyfileEdge(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	edge {
		connectors :8443 {
			tls /certs/server.crt /certs/server.key
			client home {
				token secret
				policy replace_existing
				max_streams 100
			}
		}
		ingress :443 {
			route_sni home.example.com home
			route_sni files.example.com home
		}
	}
}`)
	var app App
	if err := app.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if app.Edge == nil {
		t.Fatal("expected edge runtime")
	}
	edge := app.Edge
	if edge.ConnectorListen != ":8443" {
		t.Fatalf("connector listen = %q", edge.ConnectorListen)
	}
	if edge.TLSCertFile != "/certs/server.crt" || edge.TLSKeyFile != "/certs/server.key" {
		t.Fatalf("tls files = %q %q", edge.TLSCertFile, edge.TLSKeyFile)
	}
	if len(edge.Clients) != 1 {
		t.Fatalf("clients = %d", len(edge.Clients))
	}
	if edge.Clients[0].Endpoint != "home" || edge.Clients[0].Token != "secret" {
		t.Fatalf("client = %#v", edge.Clients[0])
	}
	if edge.Clients[0].Policy != "replace_existing" || edge.Clients[0].MaxStreams != 100 {
		t.Fatalf("client policy/limits = %#v", edge.Clients[0])
	}
	if len(edge.Routes) != 2 {
		t.Fatalf("routes = %d", len(edge.Routes))
	}
}

func TestAppUnmarshalCaddyfileAgent(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	agent {
		connect edge.example.com:8443
		token secret
		endpoint home
		forward unix//run/tunely/agent-https.sock
		tls_ca_file /certs/ca.crt
		tls_server_name edge.example.com
		tls_insecure_skip_verify
	}
}`)
	var app App
	if err := app.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if app.Agent == nil {
		t.Fatal("expected agent runtime")
	}
	agent := app.Agent
	if agent.Connect != "edge.example.com:8443" {
		t.Fatalf("connect = %q", agent.Connect)
	}
	if agent.Token != "secret" || agent.Endpoint != "home" {
		t.Fatalf("identity = %q %q", agent.Token, agent.Endpoint)
	}
	if agent.Forward != "unix//run/tunely/agent-https.sock" {
		t.Fatalf("forward = %q", agent.Forward)
	}
	if agent.TLSCAFile != "/certs/ca.crt" || agent.TLSServerName != "edge.example.com" {
		t.Fatalf("tls = %q %q", agent.TLSCAFile, agent.TLSServerName)
	}
	if !agent.TLSInsecureSkipVerify {
		t.Fatal("expected insecure skip verify")
	}
}

func TestCaddyfileAdaptEdgeApp(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	bifrost {
		edge {
			connectors :8443 {
				tls /certs/server.crt /certs/server.key
				client home {
					token secret
					policy replace_existing
					max_streams 100
				}
			}
			ingress :443 {
				route_sni home.example.com home
			}
		}
	}
}`)
	assertBifrostAppOnly(t, configJSON)
	var cfg struct {
		Apps struct {
			Bifrost App `json:"bifrost"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal adapted config: %v", err)
	}
	if cfg.Apps.Bifrost.Edge == nil || cfg.Apps.Bifrost.Agent != nil {
		t.Fatalf("runtime = %#v", cfg.Apps.Bifrost)
	}
	if cfg.Apps.Bifrost.Edge.Clients[0].Endpoint != "home" {
		t.Fatalf("edge client = %#v", cfg.Apps.Bifrost.Edge.Clients[0])
	}
}

func TestCaddyfileAdaptAgentApp(t *testing.T) {
	configJSON := adaptCaddyfile(t, `{
	bifrost {
		agent {
			connect edge.example.com:8443
			token secret
			endpoint home
			forward unix//run/tunely/agent-https.sock
		}
	}
}`)
	assertBifrostAppOnly(t, configJSON)
	var cfg struct {
		Apps struct {
			Bifrost App `json:"bifrost"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal adapted config: %v", err)
	}
	if cfg.Apps.Bifrost.Agent == nil || cfg.Apps.Bifrost.Edge != nil {
		t.Fatalf("runtime = %#v", cfg.Apps.Bifrost)
	}
	if cfg.Apps.Bifrost.Agent.Token != "secret" {
		t.Fatalf("agent = %#v", cfg.Apps.Bifrost.Agent)
	}
}

func TestAppRejectsMultipleRuntimes(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	edge {}
	agent {}
}`)
	var app App
	if err := app.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected multiple runtime error")
	}
}

func TestAppRuntimeValidation(t *testing.T) {
	if _, err := (&App{}).runtime(); err == nil {
		t.Fatal("expected missing runtime error")
	}
	if _, err := (&App{Edge: &Edge{}, Agent: &Agent{}}).runtime(); err == nil {
		t.Fatal("expected multiple runtime error")
	}
}

func TestPrepareValidationErrors(t *testing.T) {
	if err := (&Edge{}).prepare(); err == nil {
		t.Fatal("expected edge validation error")
	}
	if err := (&Agent{}).prepare(); err == nil {
		t.Fatal("expected agent validation error")
	}
	edge := &Edge{
		TLSCertFile: "/certs/server.crt",
		TLSKeyFile:  "/certs/server.key",
		Clients:     []EdgeClient{{Endpoint: "home", Token: "secret"}},
		Routes: []SNIRoute{
			{ServerName: "home.example.com", Endpoint: "home"},
			{ServerName: "HOME.EXAMPLE.COM.", Endpoint: "home"},
		},
	}
	if err := edge.prepare(); err == nil {
		t.Fatal("expected duplicate route error")
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

func assertBifrostAppOnly(t *testing.T, configJSON []byte) {
	t.Helper()
	if !bytes.Contains(configJSON, []byte(`"bifrost"`)) {
		t.Fatalf("adapted config does not include bifrost app: %s", configJSON)
	}
	if bytes.Contains(configJSON, []byte("bifrost_edge")) || bytes.Contains(configJSON, []byte("bifrost_agent")) {
		t.Fatalf("adapted config contains old app names: %s", configJSON)
	}
}
