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
	cfg        *config.Server
	logger     *zap.Logger
	tlsConfig  *tls.Config
	accept     bifrost.AcceptProvider
	observer   bifrost.Observer
	latency    bifrost.PassiveLatencyObserver
	resolverMu sync.RWMutex
	resolver   PassthroughResolver

	server    *bifrost.Server
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	stopOnce  sync.Once
	startedAt time.Time
}

type PassthroughResolver interface {
	ResolvePassthrough(ctx context.Context, serverName string) (endpoint string, ok bool, err error)
}

// PassthroughResolution is a bounded passthrough route decision.
type PassthroughResolution struct {
	EndpointKey    string
	ObservationKey string
}

// PassthroughObservationResolver is an optional extension for resolvers that
// attach an opaque observation key to the route decision.
type PassthroughObservationResolver interface {
	ResolvePassthroughObservation(ctx context.Context, serverName string) (PassthroughResolution, bool, error)
}

type ServerOptions struct {
	AcceptProvider         bifrost.AcceptProvider
	Observer               bifrost.Observer
	PassiveLatencyObserver bifrost.PassiveLatencyObserver
}

type ServerOption func(*ServerOptions)

func WithAcceptProvider(provider bifrost.AcceptProvider) ServerOption {
	return func(opts *ServerOptions) {
		opts.AcceptProvider = provider
	}
}

func WithObserver(observer bifrost.Observer) ServerOption {
	return func(opts *ServerOptions) {
		opts.Observer = observer
	}
}

func WithPassiveLatencyObserver(observer bifrost.PassiveLatencyObserver) ServerOption {
	return func(opts *ServerOptions) {
		opts.PassiveLatencyObserver = observer
	}
}

type StaticPassthroughResolver struct {
	routes config.RouteTable
}

func NewStaticPassthroughResolver(routes []config.SNIRoute) (*StaticPassthroughResolver, error) {
	table, err := config.NewRouteTable(routes)
	if err != nil {
		return nil, err
	}
	return &StaticPassthroughResolver{routes: table}, nil
}

func (r *StaticPassthroughResolver) ResolvePassthrough(_ context.Context, serverName string) (string, bool, error) {
	if r == nil {
		return "", false, nil
	}
	endpoint, ok := r.routes.Resolve(serverName)
	return endpoint, ok, nil
}

func (r *StaticPassthroughResolver) ResolvePassthroughObservation(ctx context.Context, serverName string) (PassthroughResolution, bool, error) {
	endpoint, ok, err := r.ResolvePassthrough(ctx, serverName)
	if err != nil || !ok {
		return PassthroughResolution{}, ok, err
	}
	return PassthroughResolution{EndpointKey: endpoint}, true, nil
}

func NewServer(ctx caddy.Context, cfg *config.Server, logger *zap.Logger, options ...ServerOption) (*Server, error) {
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
	return NewServerWithTLSConfig(cfg, tlsConfig, logger, options...)
}

func NewServerWithTLSConfig(cfg *config.Server, tlsConfig *tls.Config, logger *zap.Logger, options ...ServerOption) (*Server, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if tlsConfig == nil {
		return nil, fmt.Errorf("connector tls config is required")
	}
	opts := applyServerOptions(options)
	if err := cfg.ValidateWithProvider(opts.AcceptProvider != nil); err != nil {
		return nil, err
	}
	acceptProvider := opts.AcceptProvider
	if acceptProvider == nil {
		clients, err := cfg.StaticClients()
		if err != nil {
			return nil, err
		}
		acceptProvider, err = bifrost.NewStaticAcceptProvider(clients)
		if err != nil {
			return nil, err
		}
	}
	return &Server{
		cfg:       cfg,
		logger:    logger,
		tlsConfig: tlsConfig,
		accept:    acceptProvider,
		observer:  opts.Observer,
		latency:   opts.PassiveLatencyObserver,
	}, nil
}

func applyServerOptions(options []ServerOption) ServerOptions {
	var opts ServerOptions
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return opts
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
		Guardrails: s.cfg.Guardrails.BifrostGuardrails(),
		Runtime:    s.cfg.Runtime.BifrostRuntime(),
	}, bifrost.ServerOptions{
		AcceptProvider:         s.accept,
		Observer:               s.observer,
		PassiveLatencyObserver: s.latency,
		Listener:               connectorListener,
		Logger:                 logging.Slog(s.logger.Named("bifrost")),
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

func (s *Server) ProxyStream(ctx context.Context, endpoint string, conn net.Conn) error {
	if s == nil || s.server == nil {
		_ = conn.Close()
		return fmt.Errorf("bifrost server is not running")
	}
	return s.server.ProxyStream(ctx, endpoint, conn)
}

func (s *Server) ProxyStreamWithObserver(ctx context.Context, endpoint string, conn net.Conn, observer bifrost.StreamObserver) error {
	if s == nil || s.server == nil {
		_ = conn.Close()
		return fmt.Errorf("bifrost server is not running")
	}
	return s.server.ProxyStreamWithOptions(ctx, endpoint, conn, bifrost.ProxyStreamOptions{
		Observer: observer,
	})
}

func (s *Server) PassiveLatencyObservation(endpointKey string, now time.Time) bifrost.PassiveLatencyObservation {
	endpointKey = strings.TrimSpace(endpointKey)
	if s == nil || s.server == nil {
		return bifrost.PassiveLatencyObservation{
			EndpointKey: endpointKey,
			State:       bifrost.PassiveLatencyUnknown,
		}
	}
	return s.server.PassiveLatencyObservation(endpointKey, now)
}

func (s *Server) PassiveLatencySnapshot(now time.Time) []bifrost.PassiveLatencyObservation {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.PassiveLatencySnapshot(now)
}

func (s *Server) SetPassthroughResolver(resolver PassthroughResolver) {
	if s == nil {
		return
	}
	s.resolverMu.Lock()
	defer s.resolverMu.Unlock()
	s.resolver = resolver
}

func (s *Server) ResolvePassthrough(ctx context.Context, serverName string) (PassthroughResolution, bool, error) {
	if s == nil {
		return PassthroughResolution{}, false, fmt.Errorf("bifrost server runtime is not configured")
	}
	s.resolverMu.RLock()
	resolver := s.resolver
	s.resolverMu.RUnlock()
	if resolver == nil {
		return PassthroughResolution{}, false, nil
	}
	if observationResolver, ok := resolver.(PassthroughObservationResolver); ok {
		return observationResolver.ResolvePassthroughObservation(ctx, serverName)
	}
	endpoint, ok, err := resolver.ResolvePassthrough(ctx, serverName)
	if err != nil || !ok {
		return PassthroughResolution{}, ok, err
	}
	return PassthroughResolution{EndpointKey: endpoint}, true, nil
}
