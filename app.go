package caddybifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

	// PassiveLatencyObserverRaw optionally loads a custom Caddy module from the
	// bifrost.passive_latency_observers namespace. It is only valid with the
	// server runtime and receives bounded endpoint-keyed passive latency
	// observations.
	PassiveLatencyObserverRaw json.RawMessage `json:"passive_latency_observer,omitempty" caddy:"namespace=bifrost.passive_latency_observers inline_key=observer"`

	logger  *zap.Logger
	runtime appRuntime
}

// AcceptProviderModule is the interface implemented by Caddy modules that want
// to provide dynamic Bifrost admission decisions.
type AcceptProviderModule interface {
	caddy.Module
	bifrost.AcceptProvider
}

// PassthroughResolverModule is implemented by Caddy modules that resolve raw
// TLS ClientHello SNI names to Bifrost endpoint keys.
type PassthroughResolverModule interface {
	caddy.Module
	PassthroughResolver
}

// PassthroughStreamObserverModule is implemented by Caddy modules that observe
// bounded SNI passthrough stream lifecycle events.
type PassthroughStreamObserverModule interface {
	caddy.Module
	PassthroughStreamObserver
}

// PassthroughResolver maps one inbound TLS SNI name to a Bifrost endpoint.
type PassthroughResolver interface {
	ResolvePassthrough(ctx context.Context, serverName string) (endpoint string, ok bool, err error)
}

// PassthroughResolution is a bounded passthrough route decision.
type PassthroughResolution = runtime.PassthroughResolution

// PassthroughObservationResolver is an optional extension for resolvers that
// want to attach an opaque observation key to the route decision.
type PassthroughObservationResolver interface {
	ResolvePassthroughObservation(ctx context.Context, serverName string) (PassthroughResolution, bool, error)
}

// PassthroughStreamEventType is a controlled passthrough stream lifecycle event
// name.
type PassthroughStreamEventType string

const (
	PassthroughStreamStarted    PassthroughStreamEventType = "stream_started"
	PassthroughStreamEnded      PassthroughStreamEventType = "stream_ended"
	PassthroughStreamRejected   PassthroughStreamEventType = "stream_rejected"
	PassthroughStreamUsageDelta PassthroughStreamEventType = "stream_usage_delta"
)

// PassthroughStreamResult is a controlled passthrough stream lifecycle result.
type PassthroughStreamResult string

const (
	PassthroughStreamResultStarted  PassthroughStreamResult = "started"
	PassthroughStreamResultEnded    PassthroughStreamResult = "ended"
	PassthroughStreamResultRejected PassthroughStreamResult = "rejected"
)

// PassthroughStreamReason is a controlled passthrough stream lifecycle reason.
type PassthroughStreamReason string

const (
	PassthroughStreamReasonNone              PassthroughStreamReason = "none"
	PassthroughStreamReasonResolverError     PassthroughStreamReason = "resolver_error"
	PassthroughStreamReasonEmptyEndpoint     PassthroughStreamReason = "empty_endpoint"
	PassthroughStreamReasonServerUnavailable PassthroughStreamReason = "server_unavailable"
	PassthroughStreamReasonNoSession         PassthroughStreamReason = "no_session"
	PassthroughStreamReasonSessionNotReady   PassthroughStreamReason = "session_not_ready"
	PassthroughStreamReasonStreamLimit       PassthroughStreamReason = "stream_limit"
	PassthroughStreamReasonStreamOpenFailed  PassthroughStreamReason = "stream_open_failed"
)

// PassthroughStreamObservation carries bounded stream lifecycle metadata.
//
// It intentionally excludes SNI hostnames, route hostnames, remote addresses,
// HTTP data, participant data, tokens, and private keys. ObservationKey is an
// opaque value supplied by the resolver and is not interpreted by caddy-bifrost.
type PassthroughStreamObservation struct {
	EndpointKey            string                     `json:"endpoint_key,omitempty"`
	EventType              PassthroughStreamEventType `json:"event_type,omitempty"`
	ObservedAt             time.Time                  `json:"observed_at,omitempty"`
	Result                 PassthroughStreamResult    `json:"result,omitempty"`
	Reason                 PassthroughStreamReason    `json:"reason,omitempty"`
	ObservationKey         string                     `json:"observation_key,omitempty"`
	BytesIngressToEndpoint int64                      `json:"bytes_ingress_to_endpoint,omitempty"`
	BytesEndpointToIngress int64                      `json:"bytes_endpoint_to_ingress,omitempty"`
}

// PassthroughStreamObserver receives bounded passthrough stream lifecycle
// observations. Implementations should return quickly and must not treat the
// opaque ObservationKey as a Caddy metric label.
type PassthroughStreamObserver interface {
	ObservePassthroughStream(ctx context.Context, observation PassthroughStreamObservation)
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
	if runtimeName != "server" && len(a.PassiveLatencyObserverRaw) > 0 {
		return fmt.Errorf("bifrost passive_latency_observer requires server runtime")
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
		passiveLatencyObserver, err := a.loadPassiveLatencyObserver(ctx)
		if err != nil {
			return err
		}
		var options []runtime.ServerOption
		if acceptProvider != nil {
			options = append(options, runtime.WithAcceptProvider(acceptProvider))
		}
		if passiveLatencyObserver != nil {
			options = append(options, runtime.WithPassiveLatencyObserver(newPassiveLatencyObserverAdapter(ctx.Context, passiveLatencyObserver)))
		}
		options = append(options, runtime.WithObserver(observer))
		server, err := runtime.NewServer(ctx, a.Server, a.logger.Named("server"), options...)
		if err != nil {
			return err
		}
		a.runtime = server
		return nil
	case "client":
		client, err := newClientRuntimeLease(a.Client, a.logger.Named("client"), observer)
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
