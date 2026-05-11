package caddybifrost

import (
	"strconv"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		nesting := d.Nesting()
		for d.NextBlock(nesting) {
			switch d.Val() {
			case "edge":
				if a.Agent != nil || a.Edge != nil {
					return d.Errf("configure exactly one bifrost runtime: edge or agent")
				}
				if d.NextArg() {
					return d.ArgErr()
				}
				edge := new(Edge)
				if err := parseEdgeBody(d, edge, d.Nesting()); err != nil {
					return err
				}
				a.Edge = edge
			case "agent":
				if a.Agent != nil || a.Edge != nil {
					return d.Errf("configure exactly one bifrost runtime: edge or agent")
				}
				if d.NextArg() {
					return d.ArgErr()
				}
				agent := new(Agent)
				if err := parseAgentBody(d, agent, d.Nesting()); err != nil {
					return err
				}
				a.Agent = agent
			default:
				return d.ArgErr()
			}
		}
	}
	return nil
}

func (e *Edge) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		return parseEdgeBody(d, e, d.Nesting())
	}
	return nil
}

func parseEdgeBody(d *caddyfile.Dispenser, e *Edge, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "connectors":
			if d.NextArg() {
				e.ConnectorListen = d.Val()
			}
			if d.NextArg() {
				return d.ArgErr()
			}
			connectorNesting := d.Nesting()
			for d.NextBlock(connectorNesting) {
				switch d.Val() {
				case "tls":
					if !d.NextArg() {
						return d.ArgErr()
					}
					e.TLSCertFile = d.Val()
					if !d.NextArg() {
						return d.ArgErr()
					}
					e.TLSKeyFile = d.Val()
					if d.NextArg() {
						return d.ArgErr()
					}
				case "client":
					client, err := parseEdgeClient(d)
					if err != nil {
						return err
					}
					e.Clients = append(e.Clients, client)
				default:
					return d.ArgErr()
				}
			}
		case "ingress":
			if d.NextArg() {
				e.IngressListen = d.Val()
			}
			if d.NextArg() {
				return d.ArgErr()
			}
			ingressNesting := d.Nesting()
			for d.NextBlock(ingressNesting) {
				switch d.Val() {
				case "route_sni":
					if !d.NextArg() {
						return d.ArgErr()
					}
					serverName := d.Val()
					if !d.NextArg() {
						return d.ArgErr()
					}
					endpoint := d.Val()
					if d.NextArg() {
						return d.ArgErr()
					}
					e.Routes = append(e.Routes, SNIRoute{ServerName: serverName, Endpoint: endpoint})
				default:
					return d.ArgErr()
				}
			}
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func parseEdgeClient(d *caddyfile.Dispenser) (EdgeClient, error) {
	if !d.NextArg() {
		return EdgeClient{}, d.ArgErr()
	}
	client := EdgeClient{Endpoint: d.Val()}
	if d.NextArg() {
		return EdgeClient{}, d.ArgErr()
	}
	nesting := d.Nesting()
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "token":
			if !d.NextArg() {
				return EdgeClient{}, d.ArgErr()
			}
			client.Token = d.Val()
			if d.NextArg() {
				return EdgeClient{}, d.ArgErr()
			}
		case "policy":
			if !d.NextArg() {
				return EdgeClient{}, d.ArgErr()
			}
			client.Policy = d.Val()
			if d.NextArg() {
				return EdgeClient{}, d.ArgErr()
			}
		case "max_streams":
			if !d.NextArg() {
				return EdgeClient{}, d.ArgErr()
			}
			maxStreams, err := strconv.Atoi(d.Val())
			if err != nil {
				return EdgeClient{}, err
			}
			client.MaxStreams = maxStreams
			if d.NextArg() {
				return EdgeClient{}, d.ArgErr()
			}
		default:
			return EdgeClient{}, d.ArgErr()
		}
	}
	return client, nil
}

func (a *Agent) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		return parseAgentBody(d, a, d.Nesting())
	}
	return nil
}

func parseAgentBody(d *caddyfile.Dispenser, a *Agent, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "connect":
			if !d.NextArg() {
				return d.ArgErr()
			}
			a.Connect = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "token":
			if !d.NextArg() {
				return d.ArgErr()
			}
			a.Token = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "endpoint":
			if !d.NextArg() {
				return d.ArgErr()
			}
			a.Endpoint = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "forward":
			if !d.NextArg() {
				return d.ArgErr()
			}
			a.Forward = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "tls_ca_file":
			if !d.NextArg() {
				return d.ArgErr()
			}
			a.TLSCAFile = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "tls_server_name":
			if !d.NextArg() {
				return d.ArgErr()
			}
			a.TLSServerName = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "tls_insecure_skip_verify":
			a.TLSInsecureSkipVerify = true
			if d.NextArg() {
				return d.ArgErr()
			}
		default:
			return d.ArgErr()
		}
	}
	return nil
}
