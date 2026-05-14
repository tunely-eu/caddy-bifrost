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

type Client struct {
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

func (c *Client) prepare() error {
	if strings.TrimSpace(c.Connect) == "" {
		return fmt.Errorf("connect is required")
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("token is required")
	}
	if c.Forward == "" {
		c.Forward = "127.0.0.1:8080"
	}
	return nil
}

func (c *Client) Start() error {
	if c.logger == nil {
		c.logger = zap.NewNop()
	}
	if err := c.prepare(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	c.cancel = cancel

	headers := map[string]string{bifrost.TokenHeader: c.Token}
	if c.Endpoint != "" {
		headers["x-bifrost-endpoint"] = c.Endpoint
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		err := bifrost.RunClient(ctx, bifrost.ClientConfig{
			ServerURL:             c.Connect,
			Headers:               headers,
			TLSCAFile:             c.TLSCAFile,
			TLSServerName:         c.TLSServerName,
			TLSInsecureSkipVerify: c.TLSInsecureSkipVerify,
		}, bifrost.ClientOptions{
			Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
			StreamHandler: c.handleStream,
		})
		if err != nil && ctx.Err() == nil {
			c.logger.Error("bifrost client stopped", zap.Error(err))
		}
	}()
	return nil
}

func (c *Client) Stop() error {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		c.wg.Wait()
	})
	return nil
}

func (c *Client) handleStream(ctx context.Context, stream net.Conn) {
	done := closeOnContext(ctx, stream)
	defer done()

	target, err := dialAddress(ctx, c.Forward)
	if err != nil {
		c.logger.Warn("dial internal caddy target failed", zap.String("forward", c.Forward), zap.Error(err))
		_ = stream.Close()
		return
	}
	bifrost.Copy(ctx, stream, target, bifrost.CopyOptions{})
}
