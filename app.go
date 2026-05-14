package caddybifrost

import (
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

type App struct {
	Server *Server `json:"server,omitempty"`
	Client *Client `json:"client,omitempty"`

	logger *zap.Logger
}

func (*App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "bifrost",
		New: func() caddy.Module { return new(App) },
	}
}

func (a *App) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger(a)
	runtime, err := a.runtime()
	if err != nil {
		return err
	}
	switch runtime {
	case "server":
		a.Server.logger = a.logger.Named("server")
		return a.Server.prepare(ctx)
	case "client":
		a.Client.logger = a.logger.Named("client")
		return a.Client.prepare()
	default:
		return fmt.Errorf("unsupported bifrost runtime %q", runtime)
	}
}

func (a *App) Start() error {
	runtime, err := a.runtime()
	if err != nil {
		return err
	}
	switch runtime {
	case "server":
		if a.Server.logger == nil {
			a.Server.logger = zap.NewNop()
		}
		return a.Server.Start()
	case "client":
		if a.Client.logger == nil {
			a.Client.logger = zap.NewNop()
		}
		return a.Client.Start()
	default:
		return fmt.Errorf("unsupported bifrost runtime %q", runtime)
	}
}

func (a *App) Stop() error {
	if a.Server != nil {
		return a.Server.Stop()
	}
	if a.Client != nil {
		return a.Client.Stop()
	}
	return nil
}

func (a *App) runtime() (string, error) {
	hasServer := a.Server != nil
	hasClient := a.Client != nil
	switch {
	case hasServer && hasClient:
		return "", fmt.Errorf("configure exactly one bifrost runtime: server or client")
	case hasServer:
		return "server", nil
	case hasClient:
		return "client", nil
	default:
		return "", fmt.Errorf("configure exactly one bifrost runtime: server or client")
	}
}
