package caddybifrost

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/tunely-eu/bifrost"
)

// PassiveLatencyState is the controlled state for passive endpoint latency.
type PassiveLatencyState string

const (
	// PassiveLatencyOK means a fresh passive latency observation is available.
	PassiveLatencyOK PassiveLatencyState = "ok"
	// PassiveLatencyUnknown means no passive latency observation is available.
	PassiveLatencyUnknown PassiveLatencyState = "unknown"
	// PassiveLatencyStale means the latest passive observation is older than the
	// Bifrost freshness window.
	PassiveLatencyStale PassiveLatencyState = "stale"
)

// PassiveLatencyObservation is a bounded endpoint-keyed passive latency value.
//
// It intentionally excludes SNI hostnames, route hostnames, remote addresses,
// HTTP data, participant data, tokens, token hashes, and private keys.
type PassiveLatencyObservation struct {
	EndpointKey string              `json:"endpoint_key"`
	LatencyMS   *int64              `json:"latency_ms,omitempty"`
	ObservedAt  *time.Time          `json:"observed_at,omitempty"`
	State       PassiveLatencyState `json:"state"`
}

// PassiveLatencySnapshotter exposes Bifrost passive latency state through the
// Caddy app boundary for embedding modules.
type PassiveLatencySnapshotter interface {
	PassiveLatencyObservation(endpointKey string, now time.Time) PassiveLatencyObservation
	PassiveLatencySnapshot(now time.Time) []PassiveLatencyObservation
}

// PassiveLatencyObserver receives fresh passive latency observations.
type PassiveLatencyObserver interface {
	ObservePassiveLatency(ctx context.Context, observation PassiveLatencyObservation)
}

// PassiveLatencyObserverModule is implemented by Caddy modules that observe
// endpoint-keyed passive latency without depending on Bifrost internals.
type PassiveLatencyObserverModule interface {
	caddy.Module
	PassiveLatencyObserver
}

type passiveLatencyRuntime interface {
	PassiveLatencyObservation(endpointKey string, now time.Time) bifrost.PassiveLatencyObservation
	PassiveLatencySnapshot(now time.Time) []bifrost.PassiveLatencyObservation
}

// PassiveLatencyObservation returns the latest passive latency state for one
// endpoint. Unknown is returned when no server runtime or observation exists.
func (a *App) PassiveLatencyObservation(endpointKey string, now time.Time) PassiveLatencyObservation {
	if a == nil {
		return unknownPassiveLatencyObservation(endpointKey)
	}
	runtime, ok := a.runtime.(passiveLatencyRuntime)
	if !ok {
		return unknownPassiveLatencyObservation(endpointKey)
	}
	return convertPassiveLatencyObservation(runtime.PassiveLatencyObservation(endpointKey, now))
}

// PassiveLatencySnapshot returns the latest passive latency state for every
// observed endpoint. It returns an empty snapshot when the bridge is unavailable
// or no endpoint has a passive observation.
func (a *App) PassiveLatencySnapshot(now time.Time) []PassiveLatencyObservation {
	if a == nil {
		return nil
	}
	runtime, ok := a.runtime.(passiveLatencyRuntime)
	if !ok {
		return nil
	}
	snapshot := runtime.PassiveLatencySnapshot(now)
	if len(snapshot) == 0 {
		return nil
	}
	out := make([]PassiveLatencyObservation, 0, len(snapshot))
	for _, observation := range snapshot {
		out = append(out, convertPassiveLatencyObservation(observation))
	}
	return out
}

func (a *App) loadPassiveLatencyObserver(ctx caddy.Context) (PassiveLatencyObserver, error) {
	if len(a.PassiveLatencyObserverRaw) == 0 {
		return nil, nil
	}
	mod, err := ctx.LoadModule(a, "PassiveLatencyObserverRaw")
	if err != nil {
		return nil, fmt.Errorf("loading bifrost passive latency observer: %w", err)
	}
	observer, ok := mod.(PassiveLatencyObserver)
	if !ok {
		return nil, fmt.Errorf("bifrost passive latency observer module has unexpected type %T", mod)
	}
	return observer, nil
}

func convertPassiveLatencyObservation(observation bifrost.PassiveLatencyObservation) PassiveLatencyObservation {
	state := convertPassiveLatencyState(observation.State)
	if state == PassiveLatencyUnknown {
		return unknownPassiveLatencyObservation(observation.EndpointKey)
	}
	if observation.LatencyMS == nil || observation.ObservedAt == nil {
		return unknownPassiveLatencyObservation(observation.EndpointKey)
	}
	latencyMS := *observation.LatencyMS
	observedAt := observation.ObservedAt.UTC()
	return PassiveLatencyObservation{
		EndpointKey: strings.TrimSpace(observation.EndpointKey),
		LatencyMS:   &latencyMS,
		ObservedAt:  &observedAt,
		State:       state,
	}
}

func convertPassiveLatencyState(state bifrost.PassiveLatencyState) PassiveLatencyState {
	switch state {
	case bifrost.PassiveLatencyOK:
		return PassiveLatencyOK
	case bifrost.PassiveLatencyStale:
		return PassiveLatencyStale
	default:
		return PassiveLatencyUnknown
	}
}

func unknownPassiveLatencyObservation(endpointKey string) PassiveLatencyObservation {
	return PassiveLatencyObservation{
		EndpointKey: strings.TrimSpace(endpointKey),
		State:       PassiveLatencyUnknown,
	}
}

type passiveLatencyObserverAdapter struct {
	ctx      context.Context
	observer PassiveLatencyObserver
}

var _ bifrost.PassiveLatencyObserver = (*passiveLatencyObserverAdapter)(nil)

func newPassiveLatencyObserverAdapter(ctx context.Context, observer PassiveLatencyObserver) bifrost.PassiveLatencyObserver {
	if observer == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &passiveLatencyObserverAdapter{ctx: ctx, observer: observer}
}

func (o *passiveLatencyObserverAdapter) ObserveLatency(endpointKey string, latency time.Duration, observedAt time.Time) {
	if o == nil || o.observer == nil || strings.TrimSpace(endpointKey) == "" || latency < 0 {
		return
	}
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	observedAt = observedAt.UTC()
	latencyMS := passiveLatencyMilliseconds(latency)
	o.observer.ObservePassiveLatency(o.ctx, PassiveLatencyObservation{
		EndpointKey: strings.TrimSpace(endpointKey),
		LatencyMS:   &latencyMS,
		ObservedAt:  &observedAt,
		State:       PassiveLatencyOK,
	})
}

func passiveLatencyMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	ms := value / time.Millisecond
	if value%time.Millisecond != 0 {
		ms++
	}
	if ms == 0 {
		return 1
	}
	return int64(ms)
}
