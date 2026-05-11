package caddybifrost

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"
)

type Edge struct {
	ConnectorListen string       `json:"connector_listen,omitempty"`
	IngressListen   string       `json:"ingress_listen,omitempty"`
	TLSCertFile     string       `json:"tls_cert_file,omitempty"`
	TLSKeyFile      string       `json:"tls_key_file,omitempty"`
	Clients         []EdgeClient `json:"clients,omitempty"`
	Routes          []SNIRoute   `json:"routes,omitempty"`

	logger    *zap.Logger
	server    *bifrost.Server
	routes    RouteTable
	ingress   net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	stopOnce  sync.Once
	startedAt time.Time
}

type EdgeClient struct {
	Endpoint   string `json:"endpoint,omitempty"`
	Token      string `json:"token,omitempty"`
	Policy     string `json:"policy,omitempty"`
	MaxStreams int    `json:"max_streams,omitempty"`
}

func (e *Edge) prepare() error {
	if e.ConnectorListen == "" {
		e.ConnectorListen = ":8443"
	}
	if e.IngressListen == "" {
		e.IngressListen = ":443"
	}
	if e.TLSCertFile == "" {
		return fmt.Errorf("tls_cert_file is required")
	}
	if e.TLSKeyFile == "" {
		return fmt.Errorf("tls_key_file is required")
	}
	clients, err := e.staticClients()
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		return fmt.Errorf("at least one client is required")
	}
	routes, err := NewRouteTable(e.Routes)
	if err != nil {
		return err
	}
	e.routes = routes
	return nil
}

func (e *Edge) Start() error {
	if e.logger == nil {
		e.logger = zap.NewNop()
	}
	if e.routes.byServerName == nil {
		if err := e.prepare(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel
	ready := make(chan net.Addr, 1)
	serverErr := make(chan error, 1)

	server, err := bifrost.NewServer(bifrost.ServerConfig{
		Listen:      e.ConnectorListen,
		TLSCertFile: e.TLSCertFile,
		TLSKeyFile:  e.TLSKeyFile,
		Clients:     mustStaticClients(e.Clients),
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
	e.server = server

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
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

	ingress, err := listenAddress(e.IngressListen)
	if err != nil {
		cancel()
		return err
	}
	e.ingress = ingress
	e.startedAt = time.Now()

	e.wg.Add(1)
	go e.acceptIngress()
	return nil
}

func (e *Edge) Stop() error {
	e.stopOnce.Do(func() {
		if e.cancel != nil {
			e.cancel()
		}
		if e.ingress != nil {
			_ = e.ingress.Close()
		}
		e.wg.Wait()
	})
	return nil
}

func (e *Edge) acceptIngress() {
	defer e.wg.Done()
	for {
		conn, err := e.ingress.Accept()
		if err != nil {
			if e.ctx != nil && e.ctx.Err() != nil {
				return
			}
			e.logger.Warn("accept ingress failed", zap.Error(err))
			return
		}
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.handleIngress(conn)
		}()
	}
}

func (e *Edge) handleIngress(conn net.Conn) {
	done := closeOnContext(e.ctx, conn)
	defer done()

	serverName, replayConn, err := peekClientHelloServerName(conn)
	if err != nil {
		e.logger.Warn("read client hello failed", zap.Error(err))
		_ = conn.Close()
		return
	}
	endpoint, ok := e.routes.Resolve(serverName)
	if !ok {
		e.logger.Warn("no bifrost route for sni", zap.String("server_name", serverName))
		_ = replayConn.Close()
		return
	}
	if err := e.server.ProxyStream(e.ctx, endpoint, replayConn); err != nil {
		e.logger.Warn("proxy bifrost stream failed", zap.String("endpoint", endpoint), zap.Error(err))
	}
}

func (e *Edge) staticClients() ([]bifrost.StaticClient, error) {
	clients := make([]bifrost.StaticClient, 0, len(e.Clients))
	for index, client := range e.Clients {
		static, err := edgeClientToStatic(client)
		if err != nil {
			return nil, fmt.Errorf("clients[%d]: %w", index, err)
		}
		clients = append(clients, static)
	}
	return clients, nil
}

func mustStaticClients(clients []EdgeClient) []bifrost.StaticClient {
	out := make([]bifrost.StaticClient, 0, len(clients))
	for _, client := range clients {
		static, _ := edgeClientToStatic(client)
		out = append(out, static)
	}
	return out
}

func edgeClientToStatic(client EdgeClient) (bifrost.StaticClient, error) {
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
