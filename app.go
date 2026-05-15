package caddybifrost

import (
	"encoding/json"
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

type App struct {
	Server            *config.Server  `json:"server,omitempty"`
	Client            *config.Client  `json:"client,omitempty"`
	AcceptProviderRaw json.RawMessage `json:"accept_provider,omitempty" caddy:"namespace=bifrost.accept_providers inline_key=provider"`

	logger  *zap.Logger
	runtime appRuntime
}

type AcceptProviderModule interface {
	caddy.Module
	bifrost.AcceptProvider
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
	if runtimeName != "server" && len(a.AcceptProviderRaw) > 0 {
		return fmt.Errorf("bifrost accept_provider requires server runtime")
	}
	switch runtimeName {
	case "server":
		acceptProvider, err := a.loadAcceptProvider(ctx)
		if err != nil {
			return err
		}
		var options []runtime.ServerOption
		if acceptProvider != nil {
			options = append(options, runtime.WithAcceptProvider(acceptProvider))
		}
		server, err := runtime.NewServer(ctx, a.Server, a.logger.Named("server"), options...)
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

func (a *App) loadAcceptProvider(ctx caddy.Context) (bifrost.AcceptProvider, error) {
	if len(a.AcceptProviderRaw) == 0 {
		return nil, nil
	}
	mod, err := ctx.LoadModule(a, "AcceptProviderRaw")
	if err != nil {
		return nil, fmt.Errorf("loading bifrost accept provider: %w", err)
	}
	provider, ok := mod.(bifrost.AcceptProvider)
	if !ok {
		return nil, fmt.Errorf("bifrost accept provider module has unexpected type %T", mod)
	}
	return provider, nil
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
