package caddybifrost

import (
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

type App struct {
	Edge  *Edge  `json:"edge,omitempty"`
	Agent *Agent `json:"agent,omitempty"`

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
	case "edge":
		a.Edge.logger = a.logger.Named("edge")
		return a.Edge.prepare()
	case "agent":
		a.Agent.logger = a.logger.Named("agent")
		return a.Agent.prepare()
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
	case "edge":
		if a.Edge.logger == nil {
			a.Edge.logger = zap.NewNop()
		}
		return a.Edge.Start()
	case "agent":
		if a.Agent.logger == nil {
			a.Agent.logger = zap.NewNop()
		}
		return a.Agent.Start()
	default:
		return fmt.Errorf("unsupported bifrost runtime %q", runtime)
	}
}

func (a *App) Stop() error {
	if a.Edge != nil {
		return a.Edge.Stop()
	}
	if a.Agent != nil {
		return a.Agent.Stop()
	}
	return nil
}

func (a *App) runtime() (string, error) {
	hasEdge := a.Edge != nil
	hasAgent := a.Agent != nil
	switch {
	case hasEdge && hasAgent:
		return "", fmt.Errorf("configure exactly one bifrost runtime: edge or agent")
	case hasEdge:
		return "edge", nil
	case hasAgent:
		return "agent", nil
	default:
		return "", fmt.Errorf("configure exactly one bifrost runtime: edge or agent")
	}
}
