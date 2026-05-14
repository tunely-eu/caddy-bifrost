# caddy-bifrost

`caddy-bifrost` connects two Caddy instances through a firewall, NAT, DS-Lite, or CGNAT boundary.

Run the `server` runtime on the reachable Caddy instance and the `client` runtime next to the private Caddy instance. The private side dials out to the public side, then public Caddy can reach private upstreams through Bifrost without opening inbound ports on the private network.

The module is intentionally product-agnostic. It does not know about Tunely accounts, billing, route APIs, dashboards, or DNS ownership. Product-specific control planes should configure Caddy and Bifrost around this module.

## Modes

### Public TLS

Public Caddy terminates browser TLS and uses Bifrost as a `reverse_proxy` transport to a private Caddy upstream.

```caddyfile
{
	bifrost {
		server {
			connectors :8443 {
				tls public.example.com
				client home {
					token {$HOME_TOKEN}
					policy replace_existing
					max_streams 100
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
			endpoint home
			forward 127.0.0.1:8080
			tls_server_name public.example.com
		}
	}
}

:8080 {
	reverse_proxy 127.0.0.1:8123
}
```

This mode keeps the public Caddy experience familiar: routes, access logs, retries, header handling, and TLS automation stay in normal Caddy HTTP config.

### Private TLS / SNI Passthrough

Public Caddy reads ClientHello SNI without terminating browser TLS and forwards the raw stream through Bifrost. Private Caddy terminates browser TLS.

Public Caddy:

```caddyfile
{
	bifrost {
		server {
			connectors :8443 {
				tls public.example.com
				client home {
					token {$HOME_TOKEN}
					policy replace_existing
					max_streams 100
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
			endpoint home
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

Use this mode when the application TLS session must terminate only on the private side.

## Connector TLS

`connectors.tls <subject>` uses Caddy's TLS app for the Bifrost connector listener certificate.

```caddyfile
connectors :8443 {
	tls public.example.com
	client home {
		token {$HOME_TOKEN}
	}
}
```

Issuer selection, DNS-01, account email, storage, and local/internal certificates are configured through normal Caddy TLS configuration. The standalone `bifrost-server` still uses certificate files; ACME support is intentionally Caddy-specific and lives in this module.

If `passthrough :443` owns port 443, use DNS-01 or HTTP-01 on port 80 for the connector certificate. TLS-ALPN-01 on port 443 would collide with the passthrough listener.

## Install

Build Caddy with `xcaddy`:

```sh
xcaddy build v2.11.3 \
  --with github.com/tunely-eu/caddy-bifrost@v0.2.3
```

Use the prebuilt Docker image:

```sh
docker pull ghcr.io/tunely-eu/caddy-bifrost:0.2.3
```

## JSON

```json
{
  "apps": {
    "bifrost": {
      "server": {
        "connector_listen": ":8443",
        "tls_subject": "public.example.com",
        "clients": [
          {
            "endpoint": "home",
            "token": "secret",
            "policy": "replace_existing",
            "max_streams": 100
          }
        ]
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
