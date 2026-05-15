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
				if d.NextArg() {
					return nil, nil, d.ArgErr()
				}
				cfg := new(config.Server)
				if err := parseServerBody(d, cfg, d.Nesting()); err != nil {
					return nil, nil, err
				}
				server = cfg
			case "client":
				if server != nil || client != nil {
					return nil, nil, d.Errf("configure exactly one bifrost runtime: server or client")
				}
				if d.NextArg() {
					return nil, nil, d.ArgErr()
				}
				cfg := new(config.Client)
				if err := parseClientBody(d, cfg, d.Nesting()); err != nil {
					return nil, nil, err
				}
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
		if d.NextArg() {
			return nil, d.ArgErr()
		}
		return cfg, parseServerBody(d, cfg, d.Nesting())
	}
	return cfg, nil
}

func parseServerBody(d *caddyfilepkg.Dispenser, s *config.Server, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "connector":
			if d.NextArg() {
				s.Connector.Listen = d.Val()
			}
			if d.NextArg() {
				return d.ArgErr()
			}
			if err := parseConnectorBody(d, &s.Connector, d.Nesting()); err != nil {
				return err
			}
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
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func parseConnectorBody(d *caddyfilepkg.Dispenser, c *config.Connector, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "tls":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.TLSSubject = value
		case "endpoint":
			endpoint, err := parseEndpoint(d)
			if err != nil {
				return err
			}
			c.Endpoints = append(c.Endpoints, endpoint)
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
		if d.NextArg() {
			return nil, d.ArgErr()
		}
		return cfg, parseClientBody(d, cfg, d.Nesting())
	}
	return cfg, nil
}

func parseClientBody(d *caddyfilepkg.Dispenser, c *config.Client, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "connect":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.Connect = value
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
		case "tls_ca_file":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.TLSCAFile = value
		case "tls_server_name":
			value, err := singleArg(d)
			if err != nil {
				return err
			}
			c.TLSServerName = value
		case "tls_insecure_skip_verify":
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
				value, err := singleArg(d)
				if err != nil {
					return config.Transport{}, err
				}
				transport.Endpoint = value
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
