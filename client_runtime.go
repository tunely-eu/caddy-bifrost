package caddybifrost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tunely-eu/bifrost"
	"go.uber.org/zap"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
	"github.com/tunely-eu/caddy-bifrost/internal/runtime"
)

type clientRuntimeLease struct {
	cfg      *config.Client
	logger   *zap.Logger
	observer bifrost.Observer
	identity clientRuntimeIdentity

	mu      sync.Mutex
	release func() error
}

type clientRuntimeIdentity struct {
	Connect               string
	Forward               string
	Token                 string
	TLSCAFile             string
	TLSServerName         string
	TLSInsecureSkipVerify bool
}

type clientRuntimeRegistry struct {
	mu      sync.Mutex
	entries map[clientRuntimeIdentity]*clientRuntimeEntry
}

type clientRuntimeEntry struct {
	runtime *runtime.Client
	refs    int
}

var globalClientRuntimeRegistry = newClientRuntimeRegistry()

func newClientRuntimeLease(cfg *config.Client, logger *zap.Logger, observer bifrost.Observer) (*clientRuntimeLease, error) {
	normalized, identity, err := normalizedClientRuntimeIdentity(cfg)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &clientRuntimeLease{
		cfg:      normalized,
		logger:   logger,
		observer: observer,
		identity: identity,
	}, nil
}

func (l *clientRuntimeLease) Start() error {
	if l == nil {
		return fmt.Errorf("bifrost client runtime is not configured")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.release != nil {
		return nil
	}
	release, reused, refs, err := globalClientRuntimeRegistry.acquire(l.identity, func() (*runtime.Client, error) {
		client, err := runtime.NewClient(l.cfg, l.logger, runtime.WithClientObserver(l.observer))
		if err != nil {
			return nil, err
		}
		if err := client.Start(); err != nil {
			return nil, err
		}
		return client, nil
	})
	if err != nil {
		return err
	}
	l.release = release
	if reused {
		l.logger.Info("reusing bifrost client runtime",
			zap.String("runtime_fingerprint", l.identity.Fingerprint()),
			zap.Int("leases", refs),
		)
	} else {
		l.logger.Info("started bifrost client runtime",
			zap.String("runtime_fingerprint", l.identity.Fingerprint()),
			zap.Int("leases", refs),
		)
	}
	return nil
}

func (l *clientRuntimeLease) Stop() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	release := l.release
	l.release = nil
	l.mu.Unlock()
	if release == nil {
		return nil
	}
	return release()
}

func newClientRuntimeRegistry() *clientRuntimeRegistry {
	return &clientRuntimeRegistry{entries: make(map[clientRuntimeIdentity]*clientRuntimeEntry)}
}

func (r *clientRuntimeRegistry) acquire(identity clientRuntimeIdentity, factory func() (*runtime.Client, error)) (func() error, bool, int, error) {
	if r == nil {
		return nil, false, 0, fmt.Errorf("bifrost client runtime registry is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry := r.entries[identity]; entry != nil {
		entry.refs++
		refs := entry.refs
		return func() error {
			return r.release(identity)
		}, true, refs, nil
	}
	client, err := factory()
	if err != nil {
		return nil, false, 0, err
	}
	r.entries[identity] = &clientRuntimeEntry{runtime: client, refs: 1}
	return func() error {
		return r.release(identity)
	}, false, 1, nil
}

func (r *clientRuntimeRegistry) release(identity clientRuntimeIdentity) error {
	r.mu.Lock()
	entry := r.entries[identity]
	if entry == nil {
		r.mu.Unlock()
		return nil
	}
	entry.refs--
	refs := entry.refs
	if refs > 0 {
		r.mu.Unlock()
		return nil
	}
	delete(r.entries, identity)
	client := entry.runtime
	r.mu.Unlock()
	if client != nil {
		return client.Stop()
	}
	return nil
}

func normalizedClientRuntimeIdentity(cfg *config.Client) (*config.Client, clientRuntimeIdentity, error) {
	if cfg == nil {
		return nil, clientRuntimeIdentity{}, fmt.Errorf("client runtime is required")
	}
	normalized := *cfg
	if err := normalized.Validate(); err != nil {
		return nil, clientRuntimeIdentity{}, err
	}
	return &normalized, clientRuntimeIdentity{
		Connect:               normalized.Connect,
		Forward:               normalized.Forward,
		Token:                 normalized.Token,
		TLSCAFile:             normalized.TLSCAFile,
		TLSServerName:         normalized.TLSServerName,
		TLSInsecureSkipVerify: normalized.TLSInsecureSkipVerify,
	}, nil
}

func (i clientRuntimeIdentity) Fingerprint() string {
	data, err := json.Marshal(struct {
		Connect               string `json:"connect"`
		Forward               string `json:"forward"`
		TokenFingerprint      string `json:"token_fingerprint"`
		TLSCAFile             string `json:"tls_ca_file,omitempty"`
		TLSServerName         string `json:"tls_server_name,omitempty"`
		TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify,omitempty"`
	}{
		Connect:               i.Connect,
		Forward:               i.Forward,
		TokenFingerprint:      fingerprintSecret(i.Token),
		TLSCAFile:             i.TLSCAFile,
		TLSServerName:         i.TLSServerName,
		TLSInsecureSkipVerify: i.TLSInsecureSkipVerify,
	})
	if err != nil {
		return "invalid"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

func fingerprintSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
