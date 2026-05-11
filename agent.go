package caddybifrost

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"

	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"
)

type Agent struct {
	Connect               string `json:"connect,omitempty"`
	Token                 string `json:"token,omitempty"`
	Endpoint              string `json:"endpoint,omitempty"`
	Forward               string `json:"forward,omitempty"`
	TLSCAFile             string `json:"tls_ca_file,omitempty"`
	TLSServerName         string `json:"tls_server_name,omitempty"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify,omitempty"`

	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func (a *Agent) prepare() error {
	if strings.TrimSpace(a.Connect) == "" {
		return fmt.Errorf("connect is required")
	}
	if strings.TrimSpace(a.Token) == "" {
		return fmt.Errorf("token is required")
	}
	if a.Forward == "" {
		a.Forward = "unix//run/tunely/agent-https.sock"
	}
	return nil
}

func (a *Agent) Start() error {
	if a.logger == nil {
		a.logger = zap.NewNop()
	}
	if err := a.prepare(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	a.cancel = cancel

	headers := map[string]string{bifrost.TokenHeader: a.Token}
	if a.Endpoint != "" {
		headers["x-bifrost-endpoint"] = a.Endpoint
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		err := bifrost.RunClient(ctx, bifrost.ClientConfig{
			ServerURL:             a.Connect,
			Headers:               headers,
			TLSCAFile:             a.TLSCAFile,
			TLSServerName:         a.TLSServerName,
			TLSInsecureSkipVerify: a.TLSInsecureSkipVerify,
		}, bifrost.ClientOptions{
			Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
			StreamHandler: a.handleStream,
		})
		if err != nil && ctx.Err() == nil {
			a.logger.Error("bifrost client stopped", zap.Error(err))
		}
	}()
	return nil
}

func (a *Agent) Stop() error {
	a.stopOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		a.wg.Wait()
	})
	return nil
}

func (a *Agent) handleStream(ctx context.Context, stream net.Conn) {
	done := closeOnContext(ctx, stream)
	defer done()

	target, err := dialAddress(ctx, a.Forward)
	if err != nil {
		a.logger.Warn("dial internal caddy ingress failed", zap.String("forward", a.Forward), zap.Error(err))
		_ = stream.Close()
		return
	}
	bifrost.Copy(ctx, stream, target, bifrost.CopyOptions{})
}
