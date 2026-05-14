package caddybifrost

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddytls"
	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"
)

type Server struct {
	ConnectorListen string       `json:"connector_listen,omitempty"`
	TLSSubject      string       `json:"tls_subject,omitempty"`
	Passthrough     string       `json:"passthrough,omitempty"`
	Clients         []ClientAuth `json:"clients,omitempty"`
	Routes          []SNIRoute   `json:"routes,omitempty"`
	TLSConfig       *tls.Config  `json:"-"`

	logger    *zap.Logger
	server    *bifrost.Server
	routes    RouteTable
	listener  net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	stopOnce  sync.Once
	prepared  bool
	startedAt time.Time
}

type ClientAuth struct {
	Endpoint   string `json:"endpoint,omitempty"`
	Token      string `json:"token,omitempty"`
	Policy     string `json:"policy,omitempty"`
	MaxStreams int    `json:"max_streams,omitempty"`
}

func (s *Server) prepare(ctx caddy.Context) error {
	if s.prepared {
		return nil
	}
	if s.ConnectorListen == "" {
		s.ConnectorListen = ":8443"
	}
	if s.TLSConfig == nil {
		tlsConfig, err := s.caddyTLSConfig(ctx)
		if err != nil {
			return err
		}
		s.TLSConfig = tlsConfig
	}
	clients, err := s.staticClients()
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		return fmt.Errorf("at least one client is required")
	}
	if s.Passthrough != "" || len(s.Routes) > 0 {
		if s.Passthrough == "" {
			s.Passthrough = ":443"
		}
		routes, err := NewRouteTable(s.Routes)
		if err != nil {
			return err
		}
		s.routes = routes
	}
	s.prepared = true
	return nil
}

func (s *Server) caddyTLSConfig(ctx caddy.Context) (*tls.Config, error) {
	if strings.TrimSpace(s.TLSSubject) == "" {
		return nil, fmt.Errorf("connectors tls subject is required")
	}
	tlsAppIface, err := ctx.App("tls")
	if err != nil {
		return nil, fmt.Errorf("getting tls app: %w", err)
	}
	tlsApp := tlsAppIface.(*caddytls.TLS)
	tlsApp.RegisterServerNames([]string{s.TLSSubject})
	if err := tlsApp.Manage(map[string]struct{}{s.TLSSubject: {}}); err != nil {
		return nil, fmt.Errorf("managing connector certificate %q: %w", s.TLSSubject, err)
	}
	policies := caddytls.ConnectionPolicies{
		&caddytls.ConnectionPolicy{
			ALPN:       []string{bifrost.ALPN},
			DefaultSNI: s.TLSSubject,
		},
	}
	if err := policies.Provision(ctx); err != nil {
		return nil, fmt.Errorf("provision connector tls policy: %w", err)
	}
	tlsConfig := policies.TLSConfig(ctx)
	tlsConfig.NextProtos = appendALPN(tlsConfig.NextProtos, bifrost.ALPN)
	return tlsConfig, nil
}

func appendALPN(nextProtos []string, proto string) []string {
	for _, existing := range nextProtos {
		if existing == proto {
			return nextProtos
		}
	}
	return append(nextProtos, proto)
}

func (s *Server) Start() error {
	if s.logger == nil {
		s.logger = zap.NewNop()
	}
	if err := s.prepare(caddy.Context{}); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	ready := make(chan net.Addr, 1)
	serverErr := make(chan error, 1)

	server, err := bifrost.NewServer(bifrost.ServerConfig{
		Listen:    s.ConnectorListen,
		TLSConfig: s.TLSConfig,
		Clients:   mustStaticClients(s.Clients),
	}, bifrost.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Ready: func(addr net.Addr) {
			ready <- addr
		},
	})
	if err != nil {
		cancel()
		return err
	}
	s.server = server

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := server.Run(ctx); err != nil && ctx.Err() == nil {
			serverErr <- err
		}
	}()

	select {
	case <-ready:
	case err := <-serverErr:
		cancel()
		return err
	case <-time.After(5 * time.Second):
		cancel()
		return fmt.Errorf("bifrost connector listener did not become ready")
	}

	if s.Passthrough != "" {
		listener, err := listenAddress(s.Passthrough)
		if err != nil {
			cancel()
			return err
		}
		s.listener = listener

		s.wg.Add(1)
		go s.acceptPassthrough()
	}
	s.startedAt = time.Now()
	return nil
}

func (s *Server) Stop() error {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.wg.Wait()
	})
	return nil
}

func (s *Server) OpenStream(ctx context.Context, endpoint string) (net.Conn, error) {
	if s.server == nil {
		return nil, fmt.Errorf("bifrost server is not running")
	}
	return s.server.OpenStream(ctx, endpoint)
}

func (s *Server) acceptPassthrough() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.ctx != nil && s.ctx.Err() != nil {
				return
			}
			s.logger.Warn("accept passthrough failed", zap.Error(err))
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handlePassthrough(conn)
		}()
	}
}

func (s *Server) handlePassthrough(conn net.Conn) {
	done := closeOnContext(s.ctx, conn)
	defer done()

	serverName, replayConn, err := peekClientHelloServerName(conn)
	if err != nil {
		s.logger.Warn("read client hello failed", zap.Error(err))
		_ = conn.Close()
		return
	}
	endpoint, ok := s.routes.Resolve(serverName)
	if !ok {
		s.logger.Warn("no bifrost route for sni", zap.String("server_name", serverName))
		_ = replayConn.Close()
		return
	}
	if err := s.server.ProxyStream(s.ctx, endpoint, replayConn); err != nil {
		s.logger.Warn("proxy bifrost stream failed", zap.String("endpoint", endpoint), zap.Error(err))
	}
}

func (s *Server) staticClients() ([]bifrost.StaticClient, error) {
	clients := make([]bifrost.StaticClient, 0, len(s.Clients))
	for index, client := range s.Clients {
		static, err := clientAuthToStatic(client)
		if err != nil {
			return nil, fmt.Errorf("clients[%d]: %w", index, err)
		}
		clients = append(clients, static)
	}
	return clients, nil
}

func mustStaticClients(clients []ClientAuth) []bifrost.StaticClient {
	out := make([]bifrost.StaticClient, 0, len(clients))
	for _, client := range clients {
		static, _ := clientAuthToStatic(client)
		out = append(out, static)
	}
	return out
}

func clientAuthToStatic(client ClientAuth) (bifrost.StaticClient, error) {
	endpoint := strings.TrimSpace(client.Endpoint)
	token := strings.TrimSpace(client.Token)
	if endpoint == "" {
		return bifrost.StaticClient{}, fmt.Errorf("endpoint is required")
	}
	if token == "" {
		return bifrost.StaticClient{}, fmt.Errorf("token is required")
	}
	policy := strings.TrimSpace(client.Policy)
	if policy == "" {
		policy = bifrost.PolicyRejectIfExists
	}
	static := bifrost.StaticClient{
		Token:       token,
		EndpointKey: endpoint,
		ConnectionPolicy: bifrost.ConnectionPolicy{
			Mode: policy,
		},
	}
	if client.MaxStreams > 0 {
		static.Limits.MaxStreams = client.MaxStreams
	}
	return static, nil
}
