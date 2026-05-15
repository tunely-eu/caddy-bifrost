package config

import (
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"
)

func TestServerValidateRequiresEndpointAndToken(t *testing.T) {
	server := &Server{
		Connector: Connector{
			TLSSubject: "public.example.com",
			Endpoints:  []Endpoint{{Key: "home"}},
		},
	}
	if err := server.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestServerValidateWithProviderAllowsMissingEndpoints(t *testing.T) {
	server := &Server{
		Connector: Connector{
			TLSSubject: "public.example.com",
		},
	}
	if err := server.ValidateWithProvider(true); err != nil {
		t.Fatalf("ValidateWithProvider: %v", err)
	}
}

func TestServerValidateWithProviderRejectsStaticEndpoints(t *testing.T) {
	server := &Server{
		Connector: Connector{
			TLSSubject: "public.example.com",
			Endpoints:  []Endpoint{{Key: "home", Token: "secret"}},
		},
	}
	if err := server.ValidateWithProvider(true); err == nil {
		t.Fatal("expected static endpoint conflict")
	}
}

func TestServerStaticClientsMapLimitsAndPolicy(t *testing.T) {
	server := &Server{
		Connector: Connector{
			Listen:     ":8443",
			TLSSubject: "public.example.com",
			Endpoints: []Endpoint{
				{
					Key:         "home",
					Token:       "secret",
					Policy:      bifrost.PolicyAllowParallel,
					MaxParallel: 2,
					Limits: EndpointLimits{
						MaxStreams:        42,
						MaxBandwidthBPS:   12345,
						StreamIdleTimeout: caddy.Duration(90 * time.Second),
					},
				},
			},
		},
	}
	clients, err := server.StaticClients()
	if err != nil {
		t.Fatalf("StaticClients: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("clients = %d", len(clients))
	}
	client := clients[0]
	if client.EndpointKey != "home" || client.Token != "secret" {
		t.Fatalf("identity = %#v", client)
	}
	if client.ConnectionPolicy.Mode != bifrost.PolicyAllowParallel || client.ConnectionPolicy.MaxParallel != 2 {
		t.Fatalf("policy = %#v", client.ConnectionPolicy)
	}
	if client.Limits.MaxStreams != 42 || client.Limits.MaxBandwidthBPS != 12345 || client.Limits.StreamIdleTimeoutSeconds != 90 {
		t.Fatalf("limits = %#v", client.Limits)
	}
}

func TestClientForwardIsRequired(t *testing.T) {
	client := &Client{Connect: "public.example.com:8443", Token: "secret"}
	if err := client.Validate(); err == nil {
		t.Fatal("expected missing forward error")
	}
}

func TestClientValidateDefaultsConnectorPort(t *testing.T) {
	client := &Client{Connect: "public.example.com", Token: "secret", Forward: "127.0.0.1:8080"}
	if err := client.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if client.Connect != "public.example.com:8443" {
		t.Fatalf("connect = %q", client.Connect)
	}
}

func TestClientValidateKeepsExplicitConnectorPort(t *testing.T) {
	client := &Client{Connect: "public.example.com:9443", Token: "secret", Forward: "127.0.0.1:8080"}
	if err := client.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if client.Connect != "public.example.com:9443" {
		t.Fatalf("connect = %q", client.Connect)
	}
}

func TestGuardrailsAndRuntimeMapToBifrost(t *testing.T) {
	server := &Server{
		Connector: Connector{
			TLSSubject: "public.example.com",
			Endpoints:  []Endpoint{{Key: "home", Token: "secret"}},
		},
		Guardrails: Guardrails{
			MaxSessions:               10,
			MaxStreamsPerSession:      20,
			MaxBandwidthBPSPerSession: 30,
			MinStreamIdleTimeout:      caddy.Duration(40 * time.Second),
			MaxStreamIdleTimeout:      caddy.Duration(50 * time.Second),
			MaxHeaders:                60,
			MaxHeaderBytes:            70,
		},
		Runtime: Runtime{
			HandshakeTimeout:        caddy.Duration(8 * time.Second),
			StreamCopyBufferBytes:   4096,
			TunnelKeepAliveInterval: caddy.Duration(9 * time.Second),
			TunnelKeepAliveTimeout:  caddy.Duration(10 * time.Second),
		},
	}
	if err := server.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	guardrails := server.Guardrails.BifrostGuardrails()
	if guardrails.MaxSessions != 10 || guardrails.MaxHeaderBytes != 70 || guardrails.MinStreamIdleTimeout != 40*time.Second {
		t.Fatalf("guardrails = %#v", guardrails)
	}
	runtime := server.Runtime.BifrostRuntime()
	if runtime.HandshakeTimeout != 8*time.Second || runtime.StreamCopyBufferBytes != 4096 {
		t.Fatalf("runtime = %#v", runtime)
	}
}
