package caddybifrost

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/tunely-eu/bifrost"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

type passthroughStreamLifecycle struct {
	ctx        context.Context
	observer   PassthroughStreamObserver
	resolution runtime.PassthroughResolution

	mu      sync.Mutex
	started bool
	ended   bool
}

func newPassthroughStreamLifecycle(ctx context.Context, observer PassthroughStreamObserver, resolution runtime.PassthroughResolution) *passthroughStreamLifecycle {
	if ctx == nil {
		ctx = context.Background()
	}
	return &passthroughStreamLifecycle{
		ctx:        ctx,
		observer:   observer,
		resolution: resolution,
	}
}

func (l *passthroughStreamLifecycle) start() {
	observation, ok := l.markStarted()
	if ok {
		l.observer.ObservePassthroughStream(l.ctx, observation)
	}
}

func (l *passthroughStreamLifecycle) end() {
	observation, ok := l.markEnded()
	if ok {
		l.observer.ObservePassthroughStream(l.ctx, observation)
	}
}

func (l *passthroughStreamLifecycle) reject(reason PassthroughStreamReason) bool {
	observation, ok := l.markRejected(reason)
	if ok {
		l.observer.ObservePassthroughStream(l.ctx, observation)
	}
	return ok
}

func (l *passthroughStreamLifecycle) markStarted() (PassthroughStreamObservation, bool) {
	if l == nil {
		return PassthroughStreamObservation{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.observer == nil || l.started || l.ended {
		return PassthroughStreamObservation{}, false
	}
	l.started = true
	return l.observation(PassthroughStreamStarted, PassthroughStreamResultStarted, PassthroughStreamReasonNone), true
}

func (l *passthroughStreamLifecycle) AddBytes(direction bifrost.Direction, n int64) {
	if l == nil || n <= 0 {
		return
	}
	var ingressToEndpoint int64
	var endpointToIngress int64
	switch direction {
	case bifrost.DirectionIngressToEndpoint:
		ingressToEndpoint = n
	case bifrost.DirectionEndpointToIngress:
		endpointToIngress = n
	default:
		return
	}
	for _, observation := range l.markUsageDelta(ingressToEndpoint, endpointToIngress) {
		l.observer.ObservePassthroughStream(l.ctx, observation)
	}
}

func (l *passthroughStreamLifecycle) End() {
	l.end()
}

func (l *passthroughStreamLifecycle) markUsageDelta(ingressToEndpoint int64, endpointToIngress int64) []PassthroughStreamObservation {
	if l == nil || (ingressToEndpoint <= 0 && endpointToIngress <= 0) {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.observer == nil || l.ended {
		return nil
	}
	observedAt := time.Now().UTC()
	observations := make([]PassthroughStreamObservation, 0, 2)
	if !l.started {
		l.started = true
		observations = append(observations, l.observationAt(PassthroughStreamStarted, PassthroughStreamResultStarted, PassthroughStreamReasonNone, observedAt))
	}
	usage := l.observationAt(PassthroughStreamUsageDelta, "", PassthroughStreamReasonNone, observedAt)
	usage.BytesIngressToEndpoint = ingressToEndpoint
	usage.BytesEndpointToIngress = endpointToIngress
	observations = append(observations, usage)
	return observations
}

func (l *passthroughStreamLifecycle) markEnded() (PassthroughStreamObservation, bool) {
	if l == nil {
		return PassthroughStreamObservation{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.observer == nil || !l.started || l.ended {
		return PassthroughStreamObservation{}, false
	}
	l.ended = true
	return l.observation(PassthroughStreamEnded, PassthroughStreamResultEnded, PassthroughStreamReasonNone), true
}

func (l *passthroughStreamLifecycle) markRejected(reason PassthroughStreamReason) (PassthroughStreamObservation, bool) {
	if l == nil {
		return PassthroughStreamObservation{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.observer == nil || l.started || l.ended {
		return PassthroughStreamObservation{}, false
	}
	if reason == "" {
		reason = PassthroughStreamReasonStreamOpenFailed
	}
	l.ended = true
	return l.observation(PassthroughStreamRejected, PassthroughStreamResultRejected, reason), true
}

func (l *passthroughStreamLifecycle) observation(event PassthroughStreamEventType, result PassthroughStreamResult, reason PassthroughStreamReason) PassthroughStreamObservation {
	return l.observationAt(event, result, reason, time.Now().UTC())
}

func (l *passthroughStreamLifecycle) observationAt(event PassthroughStreamEventType, result PassthroughStreamResult, reason PassthroughStreamReason, observedAt time.Time) PassthroughStreamObservation {
	return PassthroughStreamObservation{
		EndpointKey:    l.resolution.EndpointKey,
		EventType:      event,
		ObservedAt:     observedAt,
		Result:         result,
		Reason:         reason,
		ObservationKey: l.resolution.ObservationKey,
	}
}

var _ bifrost.StreamObserver = (*passthroughStreamLifecycle)(nil)

func (w *ListenerWrapper) observePassthroughRejected(ctx context.Context, resolution runtime.PassthroughResolution, reason PassthroughStreamReason) {
	lifecycle := newPassthroughStreamLifecycle(ctx, w.observer, resolution)
	lifecycle.reject(reason)
}

func classifyPassthroughStreamReject(err error) PassthroughStreamReason {
	if err == nil {
		return PassthroughStreamReasonStreamOpenFailed
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "bifrost server is not running"):
		return PassthroughStreamReasonServerUnavailable
	case strings.Contains(msg, "no active session"):
		return PassthroughStreamReasonNoSession
	case strings.Contains(msg, "session closed before it was ready"), strings.Contains(msg, "session is not ready"):
		return PassthroughStreamReasonSessionNotReady
	case strings.Contains(msg, "reached stream limit"):
		return PassthroughStreamReasonStreamLimit
	default:
		return PassthroughStreamReasonStreamOpenFailed
	}
}
