// Package config contains the JSON and Caddyfile-adapted configuration model
// used by the caddy-bifrost modules.
package config

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"
)

const (
	// DefaultConnectorListen is the public connector listen address used when no
	// listen address is configured.
	DefaultConnectorListen = ":8443"

	// DefaultConnectorPort is added to client connect addresses that omit a port.
	DefaultConnectorPort = "8443"

	// DefaultAppName is the Caddy app name used by transports and listener
	// wrappers when no app name is configured.
	DefaultAppName = "bifrost"
)

// Server is the JSON configuration for the public Bifrost server runtime.
type Server struct {
	// Connector configures the TLS connector listener and static endpoints.
	Connector Connector `json:"connector,omitempty"`

	// Guardrails configures server-wide ceilings enforced after every admission
	// decision.
	Guardrails Guardrails `json:"guardrails,omitempty"`

	// Runtime configures low-level tunnel runtime timeouts and buffers.
	Runtime Runtime `json:"runtime,omitempty"`
}

// Connector configures the public listener that private clients dial.
type Connector struct {
	// Listen is the connector listener address. It defaults to :8443.
	Listen string `json:"listen,omitempty"`

	// TLSSubject is the certificate subject Caddy should manage for connector
	// TLS.
	TLSSubject string `json:"tls_subject,omitempty"`

	// Endpoints are static token-backed endpoint definitions. They must be empty
	// when a custom accept provider is configured.
	Endpoints []Endpoint `json:"endpoints,omitempty"`
}

// Endpoint configures one static Bifrost endpoint accepted by token.
type Endpoint struct {
	// Key is the endpoint identity used by reverse_proxy upstreams and SNI routes.
	Key string `json:"key,omitempty"`

	// Token is the shared secret that admits the private connector session.
	Token string `json:"token,omitempty"`

	// Policy controls reconnect behavior. Supported values are reject_if_exists,
	// replace_existing, and allow_parallel.
	Policy string `json:"policy,omitempty"`

	// MaxParallel bounds active sessions when Policy is allow_parallel.
	MaxParallel int `json:"max_parallel,omitempty"`

	// Limits configures per-session limits for this endpoint.
	Limits EndpointLimits `json:"limits,omitempty"`
}

// EndpointLimits configures per-session stream, bandwidth, and idle-time limits.
type EndpointLimits struct {
	// MaxStreams limits concurrent streams in one connector session.
	MaxStreams int `json:"max_streams,omitempty"`

	// MaxBandwidthBPS limits aggregate session bandwidth in bytes per second.
	MaxBandwidthBPS int64 `json:"max_bandwidth_bps,omitempty"`

	// StreamIdleTimeout closes streams after this duration with no copied bytes.
	StreamIdleTimeout caddy.Duration `json:"stream_idle_timeout,omitempty"`
}

// Guardrails configures server-wide ceilings for accepted endpoint limits and
// connector hello metadata.
type Guardrails struct {
	// MaxSessions limits active connector sessions on the server.
	MaxSessions int `json:"max_sessions,omitempty"`

	// MaxStreamsPerSession is the highest per-endpoint max_streams value allowed.
	MaxStreamsPerSession int `json:"max_streams_per_session,omitempty"`

	// MaxBandwidthBPSPerSession is the highest per-endpoint bandwidth limit
	// allowed.
	MaxBandwidthBPSPerSession int64 `json:"max_bandwidth_bps_per_session,omitempty"`

	// MinStreamIdleTimeout is the shortest stream idle timeout an endpoint may
	// request.
	MinStreamIdleTimeout caddy.Duration `json:"min_stream_idle_timeout,omitempty"`

	// MaxStreamIdleTimeout is the longest stream idle timeout an endpoint may
	// request.
	MaxStreamIdleTimeout caddy.Duration `json:"max_stream_idle_timeout,omitempty"`

	// MaxHeaders limits connector hello headers.
	MaxHeaders int `json:"max_headers,omitempty"`

	// MaxHeaderBytes limits combined connector hello header bytes.
	MaxHeaderBytes int `json:"max_header_bytes,omitempty"`
}

// Runtime configures low-level Bifrost transport behavior.
type Runtime struct {
	// HandshakeTimeout bounds TLS and Bifrost hello negotiation.
	HandshakeTimeout caddy.Duration `json:"handshake_timeout,omitempty"`

	// StreamCopyBufferBytes sets the proxy copy buffer size.
	StreamCopyBufferBytes int `json:"stream_copy_buffer_bytes,omitempty"`

	// TunnelKeepAliveInterval controls yamux keepalive frequency.
	TunnelKeepAliveInterval caddy.Duration `json:"tunnel_keepalive_interval,omitempty"`

	// TunnelKeepAliveTimeout closes a tunnel when keepalive responses stop.
	TunnelKeepAliveTimeout caddy.Duration `json:"tunnel_keepalive_timeout,omitempty"`
}

// Client is the JSON configuration for the private Bifrost client runtime.
type Client struct {
	// Connect is the public connector address. Port 8443 is added when omitted.
	Connect string `json:"connect,omitempty"`

	// Token is the shared secret sent to the matching server endpoint.
	Token string `json:"token,omitempty"`

	// Forward is the private TCP target reached for each accepted stream.
	Forward string `json:"forward,omitempty"`

	// TLSCAFile optionally points at a CA bundle for private or self-signed
	// connector certificates.
	TLSCAFile string `json:"tls_ca_file,omitempty"`

	// TLSServerName overrides the connector TLS server name.
	TLSServerName string `json:"tls_server_name,omitempty"`

	// TLSInsecureSkipVerify disables connector certificate verification. It is
	// development-only.
	TLSInsecureSkipVerify bool `json:"tls_insecure_skip_verify,omitempty"`
}

// Transport is the JSON configuration for the reverse_proxy Bifrost transport.
type Transport struct {
	// App is the Caddy app name that owns the Bifrost server runtime. It defaults
	// to "bifrost".
	App string `json:"app,omitempty"`

	// DialTimeout bounds waiting for a stream to an endpoint.
	DialTimeout caddy.Duration `json:"dial_timeout,omitempty"`
}

// Normalize trims string fields and fills implicit defaults.
func (s *Server) Normalize() {
	s.Connector.Listen = strings.TrimSpace(s.Connector.Listen)
	s.Connector.TLSSubject = strings.TrimSpace(s.Connector.TLSSubject)
	if s.Connector.Listen == "" {
		s.Connector.Listen = DefaultConnectorListen
	}
	for i := range s.Connector.Endpoints {
		s.Connector.Endpoints[i].Key = strings.TrimSpace(s.Connector.Endpoints[i].Key)
		s.Connector.Endpoints[i].Token = strings.TrimSpace(s.Connector.Endpoints[i].Token)
		s.Connector.Endpoints[i].Policy = strings.TrimSpace(s.Connector.Endpoints[i].Policy)
	}
}

// Validate validates a static server configuration.
func (s *Server) Validate() error {
	return s.ValidateWithProvider(false)
}

// ValidateWithProvider validates server config and accounts for whether a
// custom accept provider is configured.
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

// Validate validates the private client runtime configuration.
func (c *Client) Validate() error {
	if c == nil {
		return fmt.Errorf("client runtime is required")
	}
	c.Normalize()
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

// Normalize trims client fields and adds the default connector port when needed.
func (c *Client) Normalize() {
	c.Connect = normalizeConnectAddress(c.Connect)
	c.Token = strings.TrimSpace(c.Token)
	c.Forward = strings.TrimSpace(c.Forward)
	c.TLSCAFile = strings.TrimSpace(c.TLSCAFile)
	c.TLSServerName = strings.TrimSpace(c.TLSServerName)
}

func normalizeConnectAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" || strings.Contains(address, "://") {
		return address
	}
	if host, port, err := net.SplitHostPort(address); err == nil {
		if port == "" {
			return net.JoinHostPort(host, DefaultConnectorPort)
		}
		return address
	}
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		return net.JoinHostPort(strings.TrimSuffix(strings.TrimPrefix(address, "["), "]"), DefaultConnectorPort)
	}
	if strings.Count(address, ":") == 1 && strings.HasSuffix(address, ":") {
		return net.JoinHostPort(strings.TrimSuffix(address, ":"), DefaultConnectorPort)
	}
	if strings.Count(address, ":") > 1 {
		return net.JoinHostPort(address, DefaultConnectorPort)
	}
	return net.JoinHostPort(address, DefaultConnectorPort)
}

// Normalize trims the transport config and fills the default app name.
func (t *Transport) Normalize() {
	t.App = strings.TrimSpace(t.App)
	if t.App == "" {
		t.App = DefaultAppName
	}
}

// Validate validates transport config.
func (t *Transport) Validate() error {
	if t == nil {
		return fmt.Errorf("transport config is required")
	}
	t.Normalize()
	return nil
}

// StaticClients converts configured endpoints into core Bifrost static clients.
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

// StaticClient converts one endpoint into a core Bifrost static client.
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

// Validate checks endpoint limits before conversion to core Bifrost limits.
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

// PlanLimits converts Caddy duration-based limits into core Bifrost plan limits.
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

// Validate checks server guardrail values.
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

// BifrostGuardrails converts Caddy guardrails into core Bifrost guardrails.
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

// Validate checks runtime tuning values.
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

// BifrostRuntime converts Caddy runtime tuning into core Bifrost runtime config.
func (r Runtime) BifrostRuntime() bifrost.Runtime {
	return bifrost.Runtime{
		HandshakeTimeout:        time.Duration(r.HandshakeTimeout),
		StreamCopyBufferBytes:   r.StreamCopyBufferBytes,
		TunnelKeepAliveInterval: time.Duration(r.TunnelKeepAliveInterval),
		TunnelKeepAliveTimeout:  time.Duration(r.TunnelKeepAliveTimeout),
	}
}
