package caddybifrost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
)

func init() {
	caddy.RegisterModule(new(testAcceptProviderModule))
}

type testAcceptProviderModule struct {
	Endpoint string `json:"endpoint,omitempty"`
}

func (*testAcceptProviderModule) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "bifrost.accept_providers.test",
		New: func() caddy.Module { return new(testAcceptProviderModule) },
	}
}

func (p *testAcceptProviderModule) Accept(_ context.Context, _ bifrost.AcceptRequest) (bifrost.AcceptDecision, error) {
	return bifrost.AcceptDecision{
		Allow:       true,
		EndpointKey: p.Endpoint,
		Limits:      bifrost.PlanLimits{MaxStreams: 10},
	}, nil
}

func TestAppProvisionLoadsAcceptProviderModule(t *testing.T) {
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
		AcceptProviderRaw: json.RawMessage(`{"provider":"test","endpoint":"home"}`),
	}
	if err := app.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if app.runtime == nil {
		t.Fatal("expected runtime")
	}
}

var (
	_ AcceptProviderModule = (*testAcceptProviderModule)(nil)
)
