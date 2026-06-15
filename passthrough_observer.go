package caddybifrost

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

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
	return PassthroughStreamObservation{
		EndpointKey:    l.resolution.EndpointKey,
		EventType:      event,
		ObservedAt:     time.Now().UTC(),
		Result:         result,
		Reason:         reason,
		ObservationKey: l.resolution.ObservationKey,
	}
}

type passthroughObservedConn struct {
	net.Conn
	lifecycle *passthroughStreamLifecycle
}

func (c *passthroughObservedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && c.lifecycle != nil {
		c.lifecycle.start()
	}
	return n, err
}

func (c *passthroughObservedConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 && c.lifecycle != nil {
		c.lifecycle.start()
	}
	return n, err
}

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
