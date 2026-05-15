# caddy-bifrost

`caddy-bifrost` connects two Caddy instances across NAT, DS-Lite, CGNAT, or a firewall without opening inbound ports on the private network.

Put the `server` runtime on the public Caddy instance and the `client` runtime next to the private Caddy instance. The private side dials out to the public side, and public Caddy can then proxy requests through Bifrost as if the private service were a normal upstream.

## Quick Start: Public TLS

This is the simplest setup for an existing Caddy user: public Caddy keeps handling HTTPS, routes, logs, retries, headers, and ACME. Bifrost only becomes the private upstream transport.

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

## Install

Build Caddy with `xcaddy`:

```sh
xcaddy build v2.11.3 \
  --with github.com/tunely-eu/caddy-bifrost@v0.5.0
```

Or use the prebuilt image:

```sh
docker pull ghcr.io/tunely-eu/caddy-bifrost:0.5.0
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
client bifrost-server {
	token {$HOME_TOKEN}
	forward 127.0.0.1:8080
	tls {
		ca_file /data/caddy/pki/authorities/local/root.crt
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

## Development

```sh
go test ./...
go test -race ./...
make xcaddy-build
make verify-module
```
