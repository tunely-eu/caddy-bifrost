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

// App is the Caddy application module that runs one Bifrost runtime.
//
// Configure exactly one of Server or Client. A server runtime runs on the public
// Caddy instance and accepts outbound connector sessions. A client runtime runs
// on the private side and dials the public connector listener.
type App struct {
	// Server configures the public connector server runtime.
	Server *config.Server `json:"server,omitempty"`

	// Client configures the private connector client runtime.
	Client *config.Client `json:"client,omitempty"`

	// AcceptProviderRaw optionally loads a custom Caddy module from the
	// bifrost.accept_providers namespace. It is only valid with the server
	// runtime and replaces static endpoint token config.
	AcceptProviderRaw json.RawMessage `json:"accept_provider,omitempty" caddy:"namespace=bifrost.accept_providers inline_key=provider"`

	logger  *zap.Logger
	runtime appRuntime
}

// AcceptProviderModule is the interface implemented by Caddy modules that want
// to provide dynamic Bifrost admission decisions.
type AcceptProviderModule interface {
	caddy.Module
	bifrost.AcceptProvider
}

type appRuntime interface {
	Start() error
	Stop() error
}

// CaddyModule returns the module registration for the Bifrost app.
func (*App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "bifrost",
		New: func() caddy.Module { return new(App) },
	}
}

// Provision validates app config, wires metrics, loads optional admission
// providers, and prepares the selected runtime.
func (a *App) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger(a)
	runtimeName, err := a.runtimeName()
	if err != nil {
		return err
	}
	if runtimeName != "server" && len(a.AcceptProviderRaw) > 0 {
		return fmt.Errorf("bifrost accept_provider requires server runtime")
	}
	observer, err := runtime.NewCaddyObserver(ctx.GetMetricsRegistry())
	if err != nil {
		return err
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
		options = append(options, runtime.WithObserver(observer))
		server, err := runtime.NewServer(ctx, a.Server, a.logger.Named("server"), options...)
		if err != nil {
			return err
		}
		a.runtime = server
		return nil
	case "client":
		client, err := runtime.NewClient(a.Client, a.logger.Named("client"), runtime.WithClientObserver(observer))
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

// Start starts the provisioned Bifrost server or client runtime.
func (a *App) Start() error {
	if a.runtime == nil {
		return fmt.Errorf("bifrost app is not provisioned")
	}
	return a.runtime.Start()
}

// Stop stops the running Bifrost runtime. It is safe to call on an unstarted app.
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
