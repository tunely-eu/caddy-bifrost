package caddybifrost

import (
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

type App struct {
	Server *config.Server `json:"server,omitempty"`
	Client *config.Client `json:"client,omitempty"`

	logger  *zap.Logger
	runtime appRuntime
}

type appRuntime interface {
	Start() error
	Stop() error
}

func (*App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "bifrost",
		New: func() caddy.Module { return new(App) },
	}
}

func (a *App) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger(a)
	runtimeName, err := a.runtimeName()
	if err != nil {
		return err
	}
	switch runtimeName {
	case "server":
		server, err := runtime.NewServer(ctx, a.Server, a.logger.Named("server"))
		if err != nil {
			return err
		}
		a.runtime = server
		return nil
	case "client":
		client, err := runtime.NewClient(a.Client, a.logger.Named("client"))
		if err != nil {
			return err
		}
		a.runtime = client
		return nil
	default:
		return fmt.Errorf("unsupported bifrost runtime %q", runtimeName)
	}
}

func (a *App) Start() error {
	if a.runtime == nil {
		return fmt.Errorf("bifrost app is not provisioned")
	}
	return a.runtime.Start()
}

func (a *App) Stop() error {
	if a.runtime != nil {
		return a.runtime.Stop()
	}
	return nil
}

func (a *App) runtimeName() (string, error) {
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
