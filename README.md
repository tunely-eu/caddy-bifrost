# caddy-bifrost

[![CI](https://github.com/tunely-eu/caddy-bifrost/actions/workflows/ci.yml/badge.svg)](https://github.com/tunely-eu/caddy-bifrost/actions/workflows/ci.yml)
[![Docker](https://github.com/tunely-eu/caddy-bifrost/actions/workflows/docker.yml/badge.svg)](https://github.com/tunely-eu/caddy-bifrost/actions/workflows/docker.yml)
[![Release](https://github.com/tunely-eu/caddy-bifrost/actions/workflows/release.yml/badge.svg)](https://github.com/tunely-eu/caddy-bifrost/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/tunely-eu/caddy-bifrost.svg)](https://pkg.go.dev/github.com/tunely-eu/caddy-bifrost)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

`caddy-bifrost` is a Caddy module for self-hosted reverse tunnels and NAT traversal across NAT, DS-Lite, CGNAT, and firewalls. It connects a public Caddy instance to a private Caddy instance or private TCP service without opening inbound ports on the private network.

The public side accepts outbound connector sessions from the private side. Once connected, public Caddy can proxy to a private endpoint with `transport bifrost`, or forward raw TLS by SNI before Caddy's HTTP pipeline. This keeps Caddy responsible for the things it already does well: HTTPS automation, routing, logs, retries, headers, and deployment ergonomics.

**Status:** `caddy-bifrost` is pre-1.0. It is suitable for homelab, SOHO, demo, and pilot deployments where the operator can review the configuration and accept compatibility changes between early releases.

## Why This Exists

Many self-hosters and small teams already trust Caddy as their public reverse proxy, but their services live behind networks that cannot receive inbound connections:

- a home ISP uses CGNAT or DS-Lite
- a small office router cannot be changed safely
- a lab service should get a public URL without exposing the whole LAN
- public TLS, access policy, logs, and DNS should stay on the public edge
- private TLS should remain possible when the service must terminate certificates internally

`caddy-bifrost` keeps the model familiar: write a Caddyfile, keep public routing in Caddy, and replace a normal upstream with a Bifrost endpoint.

## What You Get

| Capability | What it means in practice |
| --- | --- |
| Outbound-only private side | The private Caddy instance dials the public Caddy instance. |
| Caddy-native config | Tunnel server, client, reverse proxy transport, and SNI passthrough live in the Caddyfile. |
| Public TLS mode | Public Caddy terminates HTTPS and proxies over Bifrost to a private upstream. |
| Private TLS mode | Public Caddy routes by ClientHello SNI and forwards raw TLS to private Caddy. |
| Endpoint ownership policy | Reconnect behavior is explicit: reject, replace, or allow parallel sessions. |
| Guardrails and metrics | Bifrost limits and Prometheus metrics are exposed through Caddy's runtime. |
| Product-agnostic core | The module embeds [`bifrost`](https://github.com/tunely-eu/bifrost) and stays independent of hosted control planes. |

## Modes

| Mode | Use it when | Public Caddy does | Private side does |
| --- | --- | --- | --- |
| Public TLS | You want normal Caddy reverse proxy behavior on the public VM. | Terminates browser TLS, handles HTTP routing, proxies to `transport bifrost`. | Runs the app or a private Caddy/upstream on loopback or LAN. |
| Private TLS / SNI passthrough | Browser TLS must terminate on the private Caddy instance. | Reads ClientHello SNI, maps it to a Bifrost endpoint, forwards raw TLS. | Owns the site block, certificate, and application routing. |

## Quick Start: Public TLS

This is the simplest setup for an existing Caddy user. Public Caddy keeps handling HTTPS, routes, logs, retries, headers, and ACME. Bifrost only becomes the private upstream transport.

Public Caddy:

```caddyfile
{
	bifrost {
		server public.example.com {
			endpoint home {
				token {$HOME_TOKEN}
				policy replace_existing
				limits {
					max_streams 100
				}
			}
		}
	}
}

home.example.com {
	reverse_proxy home {
		transport bifrost
	}
}
```

Private Caddy:

```caddyfile
{
	bifrost {
		client public.example.com {
			token {$HOME_TOKEN}
			forward 127.0.0.1:8080
		}
	}
}

:8080 {
	reverse_proxy 127.0.0.1:8123
}
```

Open TCP `8443` on the public host for the Bifrost connector. The private host only needs outbound access to `public.example.com:8443`. The `client` address defaults to port `8443` when no port is specified.

Use a long random token, for example from:

```sh
openssl rand -hex 32
```

## Install

Build Caddy with `xcaddy`:

```sh
xcaddy build v2.11.3 \
  --with github.com/tunely-eu/caddy-bifrost@latest
```

For reproducible deployments, pin a release tag:

```sh
xcaddy build v2.11.3 \
  --with github.com/tunely-eu/caddy-bifrost@vX.Y.Z
```

Or use the prebuilt image:

```sh
docker pull ghcr.io/tunely-eu/caddy-bifrost:latest
```

## Private TLS / SNI Passthrough

Use passthrough when browser TLS must terminate only on the private Caddy instance. Public Caddy reads ClientHello SNI, picks an endpoint, and forwards the raw TLS stream through Bifrost.

Public Caddy:

```caddyfile
{
	servers :443 {
		listener_wrappers {
			bifrost {
				route_sni home.example.com home
				route_sni files.example.com home
			}
			tls
		}
	}

	bifrost {
		server public.example.com {
			endpoint home {
				token {$HOME_TOKEN}
				policy replace_existing
				limits {
					max_streams 100
				}
			}
		}
	}
}

:443 {
	abort
}
```

Private Caddy:

```caddyfile
{
	bifrost {
		client public.example.com {
			token {$HOME_TOKEN}
			forward 127.0.0.1:9443
		}
	}
}

home.example.com {
	bind 127.0.0.1
	reverse_proxy 127.0.0.1:8123
}
```

Listener-wrapper passthrough lets Caddy continue serving normal public-TLS routes on the same `:443` listener without creating HTTP routes or edge certificates for private TLS hostnames. The catch-all `:443 { abort }` block only keeps the listener alive when there are no public Caddy routes on that listener.

## Security Model

- The connector tunnel always uses TLS and the Bifrost ALPN value.
- Endpoint tokens admit private client sessions; keep them out of checked-in Caddyfiles.
- Public TLS mode leaves browser-facing HTTPS, request policy, and application authentication in normal Caddy configuration.
- Private TLS mode forwards raw TLS after SNI routing; application TLS terminates on the private Caddy instance.
- Guardrails bound sessions, streams, bandwidth, idle time, and hello metadata.
- Metrics labels use endpoint keys and controlled reason/direction values only, not tokens, paths, remote addresses, or SNI names.

See [Security](docs/security.md) for deployment notes and trust boundaries.

## Metrics

Enable Caddy's standard metrics support to expose Bifrost endpoint metrics from the same registry as Caddy's own HTTP and reverse proxy metrics. Caddy exposes metrics through the admin `/metrics` endpoint unless the admin API is disabled; use the HTTP `metrics` handler when you want a site route for scraping:

```caddyfile
{
	metrics

	bifrost {
		server public.example.com {
			endpoint home {
				token {$HOME_TOKEN}
			}
		}
	}
}

home.example.com {
	reverse_proxy home {
		transport bifrost
	}
}

metrics.internal.example.com {
	metrics /metrics
}
```

For `reverse_proxy home { transport bifrost }`, Caddy's HTTP and reverse proxy metrics remain the primary request metrics; Bifrost adds endpoint, stream, and tunnel byte metrics. For SNI passthrough traffic, Bifrost stream and byte metrics cover traffic that does not enter Caddy's HTTP handler chain.

Useful PromQL examples:

```promql
sum by(endpoint_key)(rate(bifrost_endpoint_stream_bytes_total[5m]))
sum by(endpoint_key,direction)(rate(bifrost_endpoint_stream_bytes_total[5m]))
bifrost_endpoint_active_sessions
```

## Configuration

Server endpoints are admitted by token and identified by endpoint key. Use `listen` only when the connector should not listen on the default `:8443` address:

```caddyfile
server public.example.com {
	listen :8443
	endpoint home {
		token {$HOME_TOKEN}
		policy replace_existing
		limits {
			max_streams 100
			max_bandwidth_bps 25000000
			stream_idle_timeout 5m
		}
	}
}
```

Available connection policies are `reject_if_exists`, `replace_existing`, and `allow_parallel`. Set `max_parallel` inside the endpoint when using `allow_parallel`.

Server guardrails expose Bifrost's session-wide safety limits:

```caddyfile
guardrails {
	max_sessions 1000
	max_streams_per_session 512
	max_bandwidth_bps_per_session 100000000
	min_stream_idle_timeout 30s
	max_stream_idle_timeout 1h
	max_headers 32
	max_header_bytes 8192
}
```

Runtime tuning is also available:

```caddyfile
runtime {
	handshake_timeout 10s
	stream_copy_buffer_bytes 32768
	tunnel_keepalive_interval 30s
	tunnel_keepalive_timeout 10s
}
```

For self-signed or private-CA connector certificates, configure Caddy's TLS app normally on the public server, then trust that CA from the private client:

```caddyfile
{
	local_certs

	bifrost {
		server bifrost-server {
			endpoint home {
				token {$HOME_TOKEN}
			}
		}
	}
}
```

```caddyfile
{
	bifrost {
		client bifrost-server {
			token {$HOME_TOKEN}
			forward 127.0.0.1:8080
			tls {
				ca_file /data/caddy/pki/authorities/local/root.crt
			}
		}
	}
}
```

See [Configuration](docs/configuration.md) and [Security](docs/security.md) for the full reference.

## JSON

```json
{
  "apps": {
    "bifrost": {
      "server": {
        "connector": {
          "listen": ":8443",
          "tls_subject": "public.example.com",
          "endpoints": [
            {
              "key": "home",
              "token": "secret",
              "policy": "replace_existing",
              "limits": {
                "max_streams": 100,
                "max_bandwidth_bps": 25000000,
                "stream_idle_timeout": 300000000000
              }
            }
          ]
        }
      }
    }
  }
}
```

`reverse_proxy` transport JSON:

```json
{
  "handler": "reverse_proxy",
  "transport": {
    "protocol": "bifrost"
  },
  "upstreams": [
    {"dial": "home"}
  ]
}
```

## Documentation

- [Configuration](docs/configuration.md)
- [Security](docs/security.md)
- [`bifrost` core runtime](https://github.com/tunely-eu/bifrost)

## Development

```sh
go test ./...
go test -race ./...
make xcaddy-build
make verify-module
```

## License

MIT License. See [LICENSE](LICENSE).
