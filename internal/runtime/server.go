package runtime

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddytls"
	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/logging"
	"github.com/tunely-eu/caddy-bifrost/internal/netutil"
)

type Server struct {
	cfg           *config.Server
	logger        *zap.Logger
	tlsConfig     *tls.Config
	staticClients []bifrost.StaticClient
	routes        config.RouteTable

	server    *bifrost.Server
	listener  net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	stopOnce  sync.Once
	startedAt time.Time
}

func NewServer(ctx caddy.Context, cfg *config.Server, logger *zap.Logger) (*Server, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg == nil {
		return nil, fmt.Errorf("server config is required")
	}
	tlsConfig, err := caddyTLSConfig(ctx, cfg.Connector.TLSSubject)
	if err != nil {
		return nil, err
	}
	return NewServerWithTLSConfig(cfg, tlsConfig, logger)
}

func NewServerWithTLSConfig(cfg *config.Server, tlsConfig *tls.Config, logger *zap.Logger) (*Server, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if tlsConfig == nil {
		return nil, fmt.Errorf("connector tls config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	clients, err := cfg.StaticClients()
	if err != nil {
		return nil, err
	}
	var routes config.RouteTable
	if len(cfg.Passthrough.Routes) > 0 {
		routes, err = config.NewRouteTable(cfg.Passthrough.Routes)
		if err != nil {
			return nil, err
		}
	}
	return &Server{
		cfg:           cfg,
		logger:        logger,
		tlsConfig:     tlsConfig,
		staticClients: clients,
		routes:        routes,
	}, nil
}

func caddyTLSConfig(ctx caddy.Context, subject string) (*tls.Config, error) {
	if strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("connector tls subject is required")
	}
	tlsAppIface, err := ctx.App("tls")
	if err != nil {
		return nil, fmt.Errorf("getting tls app: %w", err)
	}
	tlsApp, ok := tlsAppIface.(*caddytls.TLS)
	if !ok {
		return nil, fmt.Errorf("tls app has unexpected type %T", tlsAppIface)
	}
	tlsApp.RegisterServerNames([]string{subject}, []string{bifrost.ALPN})
	if err := tlsApp.Manage(map[string]struct{}{subject: {}}); err != nil {
		return nil, fmt.Errorf("managing connector certificate %q: %w", subject, err)
	}
	policies := caddytls.ConnectionPolicies{
		&caddytls.ConnectionPolicy{
			ALPN:       []string{bifrost.ALPN},
			DefaultSNI: subject,
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
	if s == nil {
		return fmt.Errorf("bifrost server runtime is not configured")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	ready := make(chan net.Addr, 1)
	serverErr := make(chan error, 1)

	connectorListener, err := netutil.ListenCaddy(ctx, s.cfg.Connector.Listen)
	if err != nil {
		cancel()
		return err
	}

	server, err := bifrost.NewServer(bifrost.ServerConfig{
		Listen:     s.cfg.Connector.Listen,
		TLSConfig:  s.tlsConfig,
		Clients:    s.staticClients,
		Guardrails: s.cfg.Guardrails.BifrostGuardrails(),
		Runtime:    s.cfg.Runtime.BifrostRuntime(),
	}, bifrost.ServerOptions{
		Listener: connectorListener,
		Logger:   logging.Slog(s.logger.Named("bifrost")),
		Ready: func(addr net.Addr) {
			select {
			case ready <- addr:
			default:
			}
		},
	})
	if err != nil {
		_ = connectorListener.Close()
		cancel()
		return err
	}
	s.server = server

	failStart := func(err error) error {
		cancel()
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.wg.Wait()
		return err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := server.Run(ctx); err != nil && ctx.Err() == nil {
			select {
			case serverErr <- err:
			default:
				s.logger.Error("bifrost server stopped", zap.Error(err))
			}
		}
	}()

	select {
	case <-ready:
	case err := <-serverErr:
		return failStart(err)
	case <-time.After(5 * time.Second):
		return failStart(fmt.Errorf("bifrost connector listener did not become ready"))
	}

	if s.cfg.Passthrough.Listen != "" {
		listener, err := netutil.ListenCaddy(ctx, s.cfg.Passthrough.Listen)
		if err != nil {
			return failStart(err)
		}
		s.listener = listener

		s.wg.Add(1)
		go s.acceptPassthrough()
	}
	s.startedAt = time.Now()
	return nil
}

func (s *Server) Stop() error {
	if s == nil {
		return nil
	}
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
	if s == nil || s.server == nil {
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
	done := netutil.CloseOnContext(s.ctx, conn)
	defer done()

	serverName, replayConn, err := netutil.PeekClientHelloServerName(conn)
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
