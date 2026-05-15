package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"
)

const (
	DefaultConnectorListen = ":8443"
	DefaultAppName         = "bifrost"
)

type Server struct {
	Connector  Connector  `json:"connector,omitempty"`
	Guardrails Guardrails `json:"guardrails,omitempty"`
	Runtime    Runtime    `json:"runtime,omitempty"`
}

type Connector struct {
	Listen     string     `json:"listen,omitempty"`
	TLSSubject string     `json:"tls_subject,omitempty"`
	Endpoints  []Endpoint `json:"endpoints,omitempty"`
}

type Endpoint struct {
	Key         string         `json:"key,omitempty"`
	Token       string         `json:"token,omitempty"`
	Policy      string         `json:"policy,omitempty"`
	MaxParallel int            `json:"max_parallel,omitempty"`
	Limits      EndpointLimits `json:"limits,omitempty"`
}

type EndpointLimits struct {
	MaxStreams        int            `json:"max_streams,omitempty"`
	MaxBandwidthBPS   int64          `json:"max_bandwidth_bps,omitempty"`
	StreamIdleTimeout caddy.Duration `json:"stream_idle_timeout,omitempty"`
}

type Guardrails struct {
	MaxSessions               int            `json:"max_sessions,omitempty"`
	MaxStreamsPerSession      int            `json:"max_streams_per_session,omitempty"`
	MaxBandwidthBPSPerSession int64          `json:"max_bandwidth_bps_per_session,omitempty"`
	MinStreamIdleTimeout      caddy.Duration `json:"min_stream_idle_timeout,omitempty"`
	MaxStreamIdleTimeout      caddy.Duration `json:"max_stream_idle_timeout,omitempty"`
	MaxHeaders                int            `json:"max_headers,omitempty"`
	MaxHeaderBytes            int            `json:"max_header_bytes,omitempty"`
}

type Runtime struct {
	HandshakeTimeout        caddy.Duration `json:"handshake_timeout,omitempty"`
	StreamCopyBufferBytes   int            `json:"stream_copy_buffer_bytes,omitempty"`
	TunnelKeepAliveInterval caddy.Duration `json:"tunnel_keepalive_interval,omitempty"`
	TunnelKeepAliveTimeout  caddy.Duration `json:"tunnel_keepalive_timeout,omitempty"`
}

type Client struct {
	Connect               string `json:"connect,omitempty"`
	Token                 string `json:"token,omitempty"`
	Forward               string `json:"forward,omitempty"`
	TLSCAFile             string `json:"tls_ca_file,omitempty"`
	TLSServerName         string `json:"tls_server_name,omitempty"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify,omitempty"`
}

type Transport struct {
	Endpoint    string         `json:"endpoint,omitempty"`
	App         string         `json:"app,omitempty"`
	DialTimeout caddy.Duration `json:"dial_timeout,omitempty"`
}

func (s *Server) Normalize() {
	if s.Connector.Listen == "" {
		s.Connector.Listen = DefaultConnectorListen
	}
	for i := range s.Connector.Endpoints {
		s.Connector.Endpoints[i].Key = strings.TrimSpace(s.Connector.Endpoints[i].Key)
		s.Connector.Endpoints[i].Token = strings.TrimSpace(s.Connector.Endpoints[i].Token)
		s.Connector.Endpoints[i].Policy = strings.TrimSpace(s.Connector.Endpoints[i].Policy)
	}
}

func (s *Server) Validate() error {
	return s.ValidateWithProvider(false)
}

func (s *Server) ValidateWithProvider(providerConfigured bool) error {
	if s == nil {
		return fmt.Errorf("server runtime is required")
	}
	s.Normalize()
	if strings.TrimSpace(s.Connector.Listen) == "" {
		return fmt.Errorf("server.connector.listen is required")
	}
	if strings.TrimSpace(s.Connector.TLSSubject) == "" {
		return fmt.Errorf("server.connector.tls_subject is required")
	}
	if len(s.Connector.Endpoints) == 0 {
		if !providerConfigured {
			return fmt.Errorf("server.connector.endpoints is required")
		}
	} else if providerConfigured {
		return fmt.Errorf("server.connector.endpoints cannot be combined with custom accept provider")
	}
	seenEndpoints := make(map[string]struct{}, len(s.Connector.Endpoints))
	for index, endpoint := range s.Connector.Endpoints {
		if endpoint.Key == "" {
			return fmt.Errorf("server.connector.endpoints[%d].key is required", index)
		}
		if endpoint.Token == "" {
			return fmt.Errorf("server.connector.endpoints[%d].token is required", index)
		}
		if _, exists := seenEndpoints[endpoint.Key]; exists {
			return fmt.Errorf("server.connector.endpoints[%d].key duplicates an earlier endpoint", index)
		}
		seenEndpoints[endpoint.Key] = struct{}{}
		if err := endpoint.Limits.Validate(); err != nil {
			return fmt.Errorf("server.connector.endpoints[%d].limits: %w", index, err)
		}
	}
	if !providerConfigured {
		if _, err := s.StaticClients(); err != nil {
			return err
		}
	}
	if err := s.Guardrails.Validate(); err != nil {
		return fmt.Errorf("server.guardrails: %w", err)
	}
	if err := s.Runtime.Validate(); err != nil {
		return fmt.Errorf("server.runtime: %w", err)
	}
	return nil
}

func (c *Client) Validate() error {
	if c == nil {
		return fmt.Errorf("client runtime is required")
	}
	if strings.TrimSpace(c.Connect) == "" {
		return fmt.Errorf("client.connect is required")
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("client.token is required")
	}
	if strings.TrimSpace(c.Forward) == "" {
		return fmt.Errorf("client.forward is required")
	}
	return nil
}

func (t *Transport) Normalize() {
	t.Endpoint = strings.TrimSpace(t.Endpoint)
	t.App = strings.TrimSpace(t.App)
	if t.App == "" {
		t.App = DefaultAppName
	}
}

func (t *Transport) Validate() error {
	if t == nil {
		return fmt.Errorf("transport config is required")
	}
	t.Normalize()
	if t.Endpoint == "" {
		return fmt.Errorf("transport.endpoint is required")
	}
	return nil
}

func (s *Server) StaticClients() ([]bifrost.StaticClient, error) {
	clients := make([]bifrost.StaticClient, 0, len(s.Connector.Endpoints))
	for index, endpoint := range s.Connector.Endpoints {
		static, err := endpoint.StaticClient()
		if err != nil {
			return nil, fmt.Errorf("server.connector.endpoints[%d]: %w", index, err)
		}
		clients = append(clients, static)
	}
	if _, err := bifrost.NewStaticAcceptProvider(clients); err != nil {
		return nil, err
	}
	return clients, nil
}

func (e Endpoint) StaticClient() (bifrost.StaticClient, error) {
	limits, err := e.Limits.PlanLimits()
	if err != nil {
		return bifrost.StaticClient{}, err
	}
	return bifrost.StaticClient{
		Token:       strings.TrimSpace(e.Token),
		EndpointKey: strings.TrimSpace(e.Key),
		ConnectionPolicy: bifrost.ConnectionPolicy{
			Mode:        strings.TrimSpace(e.Policy),
			MaxParallel: e.MaxParallel,
		},
		Limits: limits,
	}, nil
}

func (l EndpointLimits) Validate() error {
	if l.MaxStreams < 0 {
		return fmt.Errorf("max_streams must be positive")
	}
	if l.MaxBandwidthBPS < 0 {
		return fmt.Errorf("max_bandwidth_bps must be positive")
	}
	if l.StreamIdleTimeout < 0 {
		return fmt.Errorf("stream_idle_timeout must be positive")
	}
	_, err := l.PlanLimits()
	return err
}

func (l EndpointLimits) PlanLimits() (bifrost.PlanLimits, error) {
	var idleSeconds int
	if l.StreamIdleTimeout > 0 {
		duration := time.Duration(l.StreamIdleTimeout)
		if duration%time.Second != 0 {
			return bifrost.PlanLimits{}, fmt.Errorf("stream_idle_timeout must use whole-second precision")
		}
		idleSeconds = int(duration / time.Second)
	}
	return bifrost.PlanLimits{
		MaxStreams:               l.MaxStreams,
		MaxBandwidthBPS:          l.MaxBandwidthBPS,
		StreamIdleTimeoutSeconds: idleSeconds,
	}, nil
}

func (g Guardrails) Validate() error {
	if g.MaxSessions < 0 {
		return fmt.Errorf("max_sessions must be positive")
	}
	if g.MaxStreamsPerSession < 0 {
		return fmt.Errorf("max_streams_per_session must be positive")
	}
	if g.MaxBandwidthBPSPerSession < 0 {
		return fmt.Errorf("max_bandwidth_bps_per_session must be positive")
	}
	if g.MinStreamIdleTimeout < 0 {
		return fmt.Errorf("min_stream_idle_timeout must be positive")
	}
	if g.MaxStreamIdleTimeout < 0 {
		return fmt.Errorf("max_stream_idle_timeout must be positive")
	}
	if g.MinStreamIdleTimeout > 0 && g.MaxStreamIdleTimeout > 0 && g.MaxStreamIdleTimeout < g.MinStreamIdleTimeout {
		return fmt.Errorf("max_stream_idle_timeout must be >= min_stream_idle_timeout")
	}
	if g.MaxHeaders < 0 {
		return fmt.Errorf("max_headers must be positive")
	}
	if g.MaxHeaderBytes < 0 {
		return fmt.Errorf("max_header_bytes must be positive")
	}
	return nil
}

func (g Guardrails) BifrostGuardrails() bifrost.Guardrails {
	return bifrost.Guardrails{
		MaxSessions:               g.MaxSessions,
		MaxStreamsPerSession:      g.MaxStreamsPerSession,
		MaxBandwidthBPSPerSession: g.MaxBandwidthBPSPerSession,
		MinStreamIdleTimeout:      time.Duration(g.MinStreamIdleTimeout),
		MaxStreamIdleTimeout:      time.Duration(g.MaxStreamIdleTimeout),
		MaxHeaders:                g.MaxHeaders,
		MaxHeaderBytes:            g.MaxHeaderBytes,
	}
}

func (r Runtime) Validate() error {
	if r.HandshakeTimeout < 0 {
		return fmt.Errorf("handshake_timeout must be positive")
	}
	if r.StreamCopyBufferBytes < 0 {
		return fmt.Errorf("stream_copy_buffer_bytes must be positive")
	}
	if r.TunnelKeepAliveInterval < 0 {
		return fmt.Errorf("tunnel_keepalive_interval must be positive")
	}
	if r.TunnelKeepAliveTimeout < 0 {
		return fmt.Errorf("tunnel_keepalive_timeout must be positive")
	}
	return nil
}

func (r Runtime) BifrostRuntime() bifrost.Runtime {
	return bifrost.Runtime{
		HandshakeTimeout:        time.Duration(r.HandshakeTimeout),
		StreamCopyBufferBytes:   r.StreamCopyBufferBytes,
		TunnelKeepAliveInterval: time.Duration(r.TunnelKeepAliveInterval),
		TunnelKeepAliveTimeout:  time.Duration(r.TunnelKeepAliveTimeout),
	}
}
