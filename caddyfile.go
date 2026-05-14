package caddybifrost

import (
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
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
			case "server":
				if a.Client != nil || a.Server != nil {
					return d.Errf("configure exactly one bifrost runtime: server or client")
				}
				if d.NextArg() {
					return d.ArgErr()
				}
				server := new(Server)
				if err := parseServerBody(d, server, d.Nesting()); err != nil {
					return err
				}
				a.Server = server
			case "client":
				if a.Client != nil || a.Server != nil {
					return d.Errf("configure exactly one bifrost runtime: server or client")
				}
				if d.NextArg() {
					return d.ArgErr()
				}
				client := new(Client)
				if err := parseClientBody(d, client, d.Nesting()); err != nil {
					return err
				}
				a.Client = client
			default:
				return d.ArgErr()
			}
		}
	}
	return nil
}

func (s *Server) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		return parseServerBody(d, s, d.Nesting())
	}
	return nil
}

func parseServerBody(d *caddyfile.Dispenser, s *Server, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "connectors":
			if d.NextArg() {
				s.ConnectorListen = d.Val()
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
					s.TLSSubject = d.Val()
					if d.NextArg() {
						return d.ArgErr()
					}
				case "client":
					client, err := parseClientAuth(d)
					if err != nil {
						return err
					}
					s.Clients = append(s.Clients, client)
				default:
					return d.ArgErr()
				}
			}
		case "passthrough":
			if d.NextArg() {
				s.Passthrough = d.Val()
			}
			if d.NextArg() {
				return d.ArgErr()
			}
			passthroughNesting := d.Nesting()
			for d.NextBlock(passthroughNesting) {
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
					s.Routes = append(s.Routes, SNIRoute{ServerName: serverName, Endpoint: endpoint})
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

func parseClientAuth(d *caddyfile.Dispenser) (ClientAuth, error) {
	if !d.NextArg() {
		return ClientAuth{}, d.ArgErr()
	}
	client := ClientAuth{Endpoint: d.Val()}
	if d.NextArg() {
		return ClientAuth{}, d.ArgErr()
	}
	nesting := d.Nesting()
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "token":
			if !d.NextArg() {
				return ClientAuth{}, d.ArgErr()
			}
			client.Token = d.Val()
			if d.NextArg() {
				return ClientAuth{}, d.ArgErr()
			}
		case "policy":
			if !d.NextArg() {
				return ClientAuth{}, d.ArgErr()
			}
			client.Policy = d.Val()
			if d.NextArg() {
				return ClientAuth{}, d.ArgErr()
			}
		case "max_streams":
			if !d.NextArg() {
				return ClientAuth{}, d.ArgErr()
			}
			maxStreams, err := strconv.Atoi(d.Val())
			if err != nil {
				return ClientAuth{}, err
			}
			client.MaxStreams = maxStreams
			if d.NextArg() {
				return ClientAuth{}, d.ArgErr()
			}
		default:
			return ClientAuth{}, d.ArgErr()
		}
	}
	return client, nil
}

func (c *Client) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		return parseClientBody(d, c, d.Nesting())
	}
	return nil
}

func parseClientBody(d *caddyfile.Dispenser, c *Client, nesting int) error {
	for d.NextBlock(nesting) {
		switch d.Val() {
		case "connect":
			if !d.NextArg() {
				return d.ArgErr()
			}
			c.Connect = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "token":
			if !d.NextArg() {
				return d.ArgErr()
			}
			c.Token = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "endpoint":
			if !d.NextArg() {
				return d.ArgErr()
			}
			c.Endpoint = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "forward":
			if !d.NextArg() {
				return d.ArgErr()
			}
			c.Forward = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "tls_ca_file":
			if !d.NextArg() {
				return d.ArgErr()
			}
			c.TLSCAFile = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "tls_server_name":
			if !d.NextArg() {
				return d.ArgErr()
			}
			c.TLSServerName = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "tls_insecure_skip_verify":
			c.TLSInsecureSkipVerify = true
			if d.NextArg() {
				return d.ArgErr()
			}
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func (t *Transport) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "endpoint":
				if !d.NextArg() {
					return d.ArgErr()
				}
				t.Endpoint = d.Val()
				if d.NextArg() {
					return d.ArgErr()
				}
			case "app":
				if !d.NextArg() {
					return d.ArgErr()
				}
				t.App = d.Val()
				if d.NextArg() {
					return d.ArgErr()
				}
			case "dial_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				timeout, err := time.ParseDuration(d.Val())
				if err != nil {
					return err
				}
				t.DialTimeout = caddy.Duration(timeout)
				if d.NextArg() {
					return d.ArgErr()
				}
			default:
				return d.ArgErr()
			}
		}
	}
	return nil
}
