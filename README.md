# caddy-bifrost

`caddy-bifrost` connects two Caddy instances across NAT, DS-Lite, CGNAT, or a firewall without opening inbound ports on the private network.

Put the `server` runtime on the public Caddy instance and the `client` runtime next to the private Caddy instance. The private side dials out to the public side, and public Caddy can then proxy requests through Bifrost as if the private service were a normal upstream.

## Quick Start: Public TLS

This is the simplest setup for an existing Caddy user: public Caddy keeps handling HTTPS, routes, logs, retries, headers, and ACME. Bifrost only becomes the private upstream transport.

Public Caddy:

```caddyfile
{
	bifrost {
		server {
			connector :8443 {
				tls public.example.com
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
}

home.example.com {
	reverse_proxy http://home {
		transport bifrost {
			endpoint home
		}
	}
}
```

Private Caddy:

```caddyfile
{
	bifrost {
		client {
			connect public.example.com:8443
			token {$HOME_TOKEN}
			forward 127.0.0.1:8080
			tls_server_name public.example.com
		}
	}
}

:8080 {
	reverse_proxy 127.0.0.1:8123
}
```

Open TCP `8443` on the public host for the Bifrost connector. The private host only needs outbound access to `public.example.com:8443`.

## Install

Build Caddy with `xcaddy`:

```sh
xcaddy build v2.11.3 \
  --with github.com/tunely-eu/caddy-bifrost@v0.3.0
```

Or use the prebuilt image:

```sh
docker pull ghcr.io/tunely-eu/caddy-bifrost:0.3.0
```

## Private TLS / SNI Passthrough

Use passthrough when browser TLS must terminate only on the private Caddy instance. Public Caddy reads ClientHello SNI, picks an endpoint, and forwards the raw TLS stream through Bifrost.

Public Caddy:

```caddyfile
{
	bifrost {
		server {
			connector :8443 {
				tls public.example.com
				endpoint home {
					token {$HOME_TOKEN}
					policy replace_existing
					limits {
						max_streams 100
					}
				}
			}

			passthrough :443 {
				route_sni home.example.com home
				route_sni files.example.com home
			}
		}
	}
}
```

Private Caddy:

```caddyfile
{
	bifrost {
		client {
			connect public.example.com:8443
			token {$HOME_TOKEN}
			forward 127.0.0.1:9443
			tls_server_name public.example.com
		}
	}
}

home.example.com {
	bind 127.0.0.1
	reverse_proxy 127.0.0.1:8123
}
```

When `passthrough :443` owns port 443 on the public host, use DNS-01 or HTTP-01 on port 80 for the connector certificate. TLS-ALPN-01 on port 443 would collide with the passthrough listener.

## Configuration

Server endpoints are admitted by token and identified by endpoint key:

```caddyfile
endpoint home {
	token {$HOME_TOKEN}
	policy replace_existing
	limits {
		max_streams 100
		max_bandwidth_bps 25000000
		stream_idle_timeout 5m
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
    "protocol": "bifrost",
    "endpoint": "home"
  },
  "upstreams": [
    {"dial": "home"}
  ]
}
```

## Development

```sh
go test ./...
go test -race ./...
make xcaddy-build
make verify-module
```
