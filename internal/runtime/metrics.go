package runtime

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/tunely-eu/bifrost"
)

type CaddyObserver struct {
	activeSessions  *prometheus.GaugeVec
	streamsStarted  *prometheus.CounterVec
	streamsEnded    *prometheus.CounterVec
	streamsRejected *prometheus.CounterVec
	streamBytes     *prometheus.CounterVec
}

var _ bifrost.Observer = (*CaddyObserver)(nil)

func NewCaddyObserver(registry *prometheus.Registry) (*CaddyObserver, error) {
	if registry == nil {
		return nil, fmt.Errorf("caddy metrics registry is required")
	}
	activeSessions, err := registerGaugeVec(registry, prometheus.GaugeOpts{
		Namespace: "bifrost",
		Subsystem: "endpoint",
		Name:      "active_sessions",
		Help:      "Current number of active Bifrost sessions by endpoint.",
	}, []string{"endpoint_key"})
	if err != nil {
		return nil, err
	}
	streamsStarted, err := registerCounterVec(registry, prometheus.CounterOpts{
		Namespace: "bifrost",
		Subsystem: "endpoint",
		Name:      "streams_started_total",
		Help:      "Total number of Bifrost streams started by endpoint.",
	}, []string{"endpoint_key"})
	if err != nil {
		return nil, err
	}
	streamsEnded, err := registerCounterVec(registry, prometheus.CounterOpts{
		Namespace: "bifrost",
		Subsystem: "endpoint",
		Name:      "streams_ended_total",
		Help:      "Total number of Bifrost streams ended by endpoint.",
	}, []string{"endpoint_key"})
	if err != nil {
		return nil, err
	}
	streamsRejected, err := registerCounterVec(registry, prometheus.CounterOpts{
		Namespace: "bifrost",
		Subsystem: "endpoint",
		Name:      "streams_rejected_total",
		Help:      "Total number of Bifrost streams rejected by endpoint and reason.",
	}, []string{"endpoint_key", "reason"})
	if err != nil {
		return nil, err
	}
	streamBytes, err := registerCounterVec(registry, prometheus.CounterOpts{
		Namespace: "bifrost",
		Subsystem: "endpoint",
		Name:      "stream_bytes_total",
		Help:      "Total bytes copied through Bifrost streams by endpoint and direction.",
	}, []string{"endpoint_key", "direction"})
	if err != nil {
		return nil, err
	}
	return &CaddyObserver{
		activeSessions:  activeSessions,
		streamsStarted:  streamsStarted,
		streamsEnded:    streamsEnded,
		streamsRejected: streamsRejected,
		streamBytes:     streamBytes,
	}, nil
}

func (o *CaddyObserver) Ready(bool) {}

func (o *CaddyObserver) ConnectionAttempted() {}

func (o *CaddyObserver) ConnectionRejected(string) {}

func (o *CaddyObserver) SessionStarted(endpointKey string) {
	if o == nil || o.activeSessions == nil || endpointKey == "" {
		return
	}
	o.activeSessions.WithLabelValues(endpointKey).Inc()
}

func (o *CaddyObserver) SessionEnded(endpointKey string) {
	if o == nil || o.activeSessions == nil || endpointKey == "" {
		return
	}
	o.activeSessions.WithLabelValues(endpointKey).Dec()
}

func (o *CaddyObserver) StreamStarted(endpointKey string) bifrost.StreamObserver {
	if o == nil || o.streamsStarted == nil || o.streamsEnded == nil || o.streamBytes == nil || endpointKey == "" {
		return bifrost.NoopStreamObserver{}
	}
	o.streamsStarted.WithLabelValues(endpointKey).Inc()
	return &caddyStreamObserver{
		ended:             o.streamsEnded.WithLabelValues(endpointKey),
		ingressToEndpoint: o.streamBytes.WithLabelValues(endpointKey, string(bifrost.DirectionIngressToEndpoint)),
		endpointToIngress: o.streamBytes.WithLabelValues(endpointKey, string(bifrost.DirectionEndpointToIngress)),
	}
}

func (o *CaddyObserver) StreamRejected(endpointKey string, reason string) {
	if o == nil || o.streamsRejected == nil || endpointKey == "" {
		return
	}
	o.streamsRejected.WithLabelValues(endpointKey, safeReason(reason)).Inc()
}

type caddyStreamObserver struct {
	ended             prometheus.Counter
	ingressToEndpoint prometheus.Counter
	endpointToIngress prometheus.Counter
	once              sync.Once
}

var _ bifrost.StreamObserver = (*caddyStreamObserver)(nil)

func (o *caddyStreamObserver) AddBytes(direction bifrost.Direction, n int64) {
	if o == nil || n <= 0 {
		return
	}
	switch direction {
	case bifrost.DirectionIngressToEndpoint:
		if o.ingressToEndpoint != nil {
			o.ingressToEndpoint.Add(float64(n))
		}
	case bifrost.DirectionEndpointToIngress:
		if o.endpointToIngress != nil {
			o.endpointToIngress.Add(float64(n))
		}
	}
}

func (o *caddyStreamObserver) End() {
	if o == nil || o.ended == nil {
		return
	}
	o.once.Do(func() {
		o.ended.Inc()
	})
}

func registerGaugeVec(registry *prometheus.Registry, opts prometheus.GaugeOpts, labels []string) (*prometheus.GaugeVec, error) {
	collector := prometheus.NewGaugeVec(opts, labels)
	if err := registry.Register(collector); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			existing, ok := already.ExistingCollector.(*prometheus.GaugeVec)
			if ok {
				return existing, nil
			}
		}
		return nil, err
	}
	return collector, nil
}

func registerCounterVec(registry *prometheus.Registry, opts prometheus.CounterOpts, labels []string) (*prometheus.CounterVec, error) {
	collector := prometheus.NewCounterVec(opts, labels)
	if err := registry.Register(collector); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			existing, ok := already.ExistingCollector.(*prometheus.CounterVec)
			if ok {
				return existing, nil
			}
		}
		return nil, err
	}
	return collector, nil
}

func safeReason(reason string) string {
	reason = strings.TrimSpace(strings.ToLower(reason))
	reason = strings.ReplaceAll(reason, "-", "_")
	reason = strings.ReplaceAll(reason, " ", "_")
	if reason == "" {
		return "unknown"
	}
	return reason
}
