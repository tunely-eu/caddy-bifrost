package runtime

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/logging"
	"github.com/tunely-eu/caddy-bifrost/internal/netutil"
)

type Client struct {
	cfg      *config.Client
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func NewClient(cfg *config.Client, logger *zap.Logger) (*Client, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, logger: logger}, nil
}

func (c *Client) Start() error {
	if c == nil {
		return fmt.Errorf("bifrost client runtime is not configured")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	c.cancel = cancel

	headers := map[string]string{bifrost.TokenHeader: c.cfg.Token}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		err := bifrost.RunClient(ctx, bifrost.ClientConfig{
			ServerURL:             c.cfg.Connect,
			Headers:               headers,
			TLSCAFile:             c.cfg.TLSCAFile,
			TLSServerName:         c.cfg.TLSServerName,
			TLSInsecureSkipVerify: c.cfg.TLSInsecureSkipVerify,
		}, bifrost.ClientOptions{
			Logger:        logging.Slog(c.logger.Named("bifrost")),
			StreamHandler: c.handleStream,
		})
		if err != nil && ctx.Err() == nil {
			c.logger.Error("bifrost client stopped", zap.Error(err))
		}
	}()
	return nil
}

func (c *Client) Stop() error {
	if c == nil {
		return nil
	}
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		c.wg.Wait()
	})
	return nil
}

func (c *Client) handleStream(ctx context.Context, stream net.Conn) {
	done := netutil.CloseOnContext(ctx, stream)
	defer done()

	target, err := netutil.DialAddress(ctx, c.cfg.Forward)
	if err != nil {
		c.logger.Warn("dial internal caddy target failed", zap.String("forward", c.cfg.Forward), zap.Error(err))
		_ = stream.Close()
		return
	}
	bifrost.Copy(ctx, stream, target, bifrost.CopyOptions{})
}
