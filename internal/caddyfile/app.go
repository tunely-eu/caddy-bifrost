package caddyfile

import (
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
	caddyfilepkg "github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"

	"github.com/tunely-eu/caddy-bifrost/internal/config"
)

func ParseApp(d *caddyfilepkg.Dispenser) (*config.Server, *config.Client, error) {
	var server *config.Server
	var client *config.Client
	for d.Next() {
		if d.NextArg() {
			return nil, nil, d.ArgErr()
		}
		nesting := d.Nesting()
		for d.NextBlock(nesting) {
			switch d.Val() {
			case "server":
				if server != nil || client != nil {
					return nil, nil, d.Errf("configure exactly one bifrost runtime: server or client")
				}
				if !d.NextArg() {
					return nil, nil, d.ArgErr()
				}
				tlsSubject := d.Val()
				if d.NextArg() {
					return nil, nil, d.ArgErr()
				}
				cfg := new(config.Server)
				cfg.Connector.TLSSubject = tlsSubject
				if err := parseServerBody(d, cfg, d.Nesting()); err != nil {
					return nil, nil, err
				}
				cfg.Normalize()
				server = cfg
			case "client":
				if server != nil || client != nil {
					return nil, nil, d.Errf("configure exactly one bifrost runtime: server or client")
				}
				if !d.NextArg() {
					return nil, nil, d.ArgErr()
				}
				connect := d.Val()
				if d.NextArg() {
					return nil, nil, d.ArgErr()
				}
				cfg := new(config.Client)
				cfg.Connect = connect
				if err := parseClientBody(d, cfg, d.Nesting()); err != nil {
					return nil, nil, err
				}
				cfg.Normalize()
				client = cfg
			default:
				return nil, nil, d.ArgErr()
			}
		}
	}
	return server, client, nil
}

func ParseServer(d *caddyfilepkg.Dispenser) (*config.Server, error) {
	cfg := new(config.Server)
	for d.Next() {
		if !d.NextArg() {
			return nil, d.ArgErr()
		}
		cfg.Connector.TLSSubject = d.Val()
		if d.NextArg() {
			return nil, d.ArgErr()
		}
		err := parseServerBody(d, cfg, d.Nesting())
		cfg.Normalize()
		return cfg, err
	}
	return cfg, nil
}

func parseServerBody(d *caddyfilepkg.Dispenser, s *config.Server, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "listen":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			s.Connector.Listen = value
		case "endpoint":
			endpoint, err := parseEndpoint(d)
			if err != nil {
				return err
			}
			s.Connector.Endpoints = append(s.Connector.Endpoints, endpoint)
		case "guardrails":
			if d.NextArg() {
				return d.ArgErr()
			}
			if err := parseGuardrails(d, &s.Guardrails, d.Nesting()); err != nil {
				return err
			}
		case "runtime":
			if d.NextArg() {
				return d.ArgErr()
			}
			if err := parseRuntime(d, &s.Runtime, d.Nesting()); err != nil {
				return err
			}
		case "connector":
			return d.Err("connector is no longer supported; use server <tls_subject> with listen and endpoint directly")
		case "tls":
			return d.Err("tls is no longer supported inside bifrost server; pass the TLS subject as server <tls_subject>")
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func parseEndpoint(d *caddyfilepkg.Dispenser) (config.Endpoint, error) {
	key, err := singleArg(d)
	if err != nil {
		return config.Endpoint{}, err
	}
	endpoint := config.Endpoint{Key: key}
	nesting := d.Nesting()
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "token":
			value, err := singleArg(d)
			if err != nil {
				return config.Endpoint{}, err
			}
			endpoint.Token = value
		case "policy":
			value, err := singleArg(d)
			if err != nil {
				return config.Endpoint{}, err
			}
			endpoint.Policy = value
		case "max_parallel":
			value, err := singleInt(d)
			if err != nil {
				return config.Endpoint{}, err
			}
			endpoint.MaxParallel = value
		case "limits":
			if d.NextArg() {
				return config.Endpoint{}, d.ArgErr()
			}
			if err := parseLimits(d, &endpoint.Limits, d.Nesting()); err != nil {
				return config.Endpoint{}, err
			}
		default:
			return config.Endpoint{}, d.ArgErr()
		}
	}
	return endpoint, nil
}

func parseLimits(d *caddyfilepkg.Dispenser, limits *config.EndpointLimits, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "max_streams":
			value, err := singleInt(d)
			if err != nil {
				return err
			}
			limits.MaxStreams = value
		case "max_bandwidth_bps":
			value, err := singleInt64(d)
			if err != nil {
				return err
			}
			limits.MaxBandwidthBPS = value
		case "stream_idle_timeout":
			value, err := singleDuration(d)
			if err != nil {
				return err
			}
			limits.StreamIdleTimeout = value
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func parseGuardrails(d *caddyfilepkg.Dispenser, g *config.Guardrails, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "max_sessions":
			value, err := singleInt(d)
			if err != nil {
				return err
			}
			g.MaxSessions = value
		case "max_streams_per_session":
			value, err := singleInt(d)
			if err != nil {
				return err
			}
			g.MaxStreamsPerSession = value
		case "max_bandwidth_bps_per_session":
			value, err := singleInt64(d)
			if err != nil {
				return err
			}
			g.MaxBandwidthBPSPerSession = value
		case "min_stream_idle_timeout":
			value, err := singleDuration(d)
			if err != nil {
				return err
			}
			g.MinStreamIdleTimeout = value
		case "max_stream_idle_timeout":
			value, err := singleDuration(d)
			if err != nil {
				return err
			}
			g.MaxStreamIdleTimeout = value
		case "max_headers":
			value, err := singleInt(d)
			if err != nil {
				return err
			}
			g.MaxHeaders = value
		case "max_header_bytes":
			value, err := singleInt(d)
			if err != nil {
				return err
			}
			g.MaxHeaderBytes = value
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func parseRuntime(d *caddyfilepkg.Dispenser, r *config.Runtime, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "handshake_timeout":
			value, err := singleDuration(d)
			if err != nil {
				return err
			}
			r.HandshakeTimeout = value
		case "stream_copy_buffer_bytes":
			value, err := singleInt(d)
			if err != nil {
				return err
			}
			r.StreamCopyBufferBytes = value
		case "tunnel_keepalive_interval":
			value, err := singleDuration(d)
			if err != nil {
				return err
			}
			r.TunnelKeepAliveInterval = value
		case "tunnel_keepalive_timeout":
			value, err := singleDuration(d)
			if err != nil {
				return err
			}
			r.TunnelKeepAliveTimeout = value
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func ParseClient(d *caddyfilepkg.Dispenser) (*config.Client, error) {
	cfg := new(config.Client)
	for d.Next() {
		if !d.NextArg() {
			return nil, d.ArgErr()
		}
		cfg.Connect = d.Val()
		if d.NextArg() {
			return nil, d.ArgErr()
		}
		err := parseClientBody(d, cfg, d.Nesting())
		cfg.Normalize()
		return cfg, err
	}
	return cfg, nil
}

func parseClientBody(d *caddyfilepkg.Dispenser, c *config.Client, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "connect":
			return d.Err("connect is no longer supported inside bifrost client; pass the connector address as client <connect-host[:port]>")
		case "token":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.Token = value
		case "forward":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.Forward = value
		case "tls":
			if d.NextArg() {
				return d.ArgErr()
			}
			if err := parseClientTLS(d, c, d.Nesting()); err != nil {
				return err
			}
		case "tls_ca_file", "tls_server_name", "tls_insecure_skip_verify":
			return d.Err("client TLS options moved under tls { ca_file, server_name, insecure_skip_verify }")
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func parseClientTLS(d *caddyfilepkg.Dispenser, c *config.Client, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "ca_file":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.TLSCAFile = value
		case "server_name":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.TLSServerName = value
		case "insecure_skip_verify":
			if d.NextArg() {
				return d.ArgErr()
			}
			c.TLSInsecureSkipVerify = true
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func ParseTransport(d *caddyfilepkg.Dispenser) (config.Transport, error) {
	var transport config.Transport
	for d.Next() {
		if d.NextArg() {
			return config.Transport{}, d.ArgErr()
		}
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "endpoint":
				return config.Transport{}, d.Err("endpoint is no longer supported in bifrost transport; use the reverse_proxy upstream host as the endpoint")
			case "app":
				value, err := singleArg(d)
				if err != nil {
					return config.Transport{}, err
				}
				transport.App = value
			case "dial_timeout":
				value, err := singleDuration(d)
				if err != nil {
					return config.Transport{}, err
				}
				transport.DialTimeout = value
			default:
				return config.Transport{}, d.ArgErr()
			}
		}
	}
	return transport, nil
}

func singleArg(d *caddyfilepkg.Dispenser) (string, error) {
	if !d.NextArg() {
		return "", d.ArgErr()
	}
	value := d.Val()
	if d.NextArg() {
		return "", d.ArgErr()
	}
	return value, nil
}

func singleInt(d *caddyfilepkg.Dispenser) (int, error) {
	value, err := singleArg(d)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, d.Errf("parse integer %q: %v", value, err)
	}
	return parsed, nil
}

func singleInt64(d *caddyfilepkg.Dispenser) (int64, error) {
	value, err := singleArg(d)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, d.Errf("parse integer %q: %v", value, err)
	}
	return parsed, nil
}

func singleDuration(d *caddyfilepkg.Dispenser) (caddy.Duration, error) {
	value, err := singleArg(d)
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, d.Errf("parse duration %q: %v", value, err)
	}
	return caddy.Duration(parsed), nil
}
