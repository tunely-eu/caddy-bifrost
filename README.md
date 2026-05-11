# caddy-bifrost

`caddy-bifrost` is an OSS Caddy app module for running Bifrost edge and agent
runtimes inside Caddy.

The module is intentionally Tunely-agnostic. Product-specific API lookups belong
in a separate module, such as `apps.tunely`, by implementing Bifrost control-plane
providers around the same core APIs.

## Install

Caddy modules are distributed as Go modules. Build a Caddy binary with `xcaddy`
and pin the module by Git tag:

```sh
xcaddy build \
  --with github.com/tunely-eu/caddy-bifrost@v0.1.0
```

Use the prebuilt Docker image:

```sh
docker pull ghcr.io/tunely-eu/caddy-bifrost:v0.1.0
```

Build Caddy with this module plus any other Caddy plugins you need:

```sh
xcaddy build \
  --with github.com/tunely-eu/caddy-bifrost@v0.1.0 \
  --with github.com/mholt/caddy-l4@latest
```

The GHCR image is only a convenience build. It contains Caddy's standard modules
and `caddy-bifrost`; users who need additional Caddy modules should build their
own binary or image with `xcaddy`.

`caddy-l4` is not required for the standard edge path. `caddy-bifrost` opens the
public edge listener itself, reads ClientHello SNI without terminating TLS, and
forwards raw TCP bytes through Bifrost.

## Caddyfile

`caddy-bifrost` registers one Caddy app named `bifrost`. Configure it in the
Caddyfile global options block.

Edge:

```caddyfile
{
	bifrost {
		edge {
			connectors :8443 {
				tls /certs/bifrost/server.crt /certs/bifrost/server.key
				client home {
					token {$HOME_TOKEN}
					policy replace_existing
					max_streams 100
				}
			}

			ingress :443 {
				route_sni home.example.com home
				route_sni files.example.com home
			}
		}
	}
}
```

Agent:

```caddyfile
{
	bifrost {
		agent {
			connect edge.example.com:8443
			token {$HOME_TOKEN}
			endpoint home
			forward unix//run/tunely/agent-https.sock
		}
	}
}

https://home.example.com {
	bind unix//run/tunely/agent-https.sock
	reverse_proxy unix//run/home/app.sock
}
```

The edge app reads ClientHello SNI and forwards raw TCP bytes over Bifrost. It
does not terminate public TLS. The agent app forwards raw streams into the
configured local Caddy ingress, where normal Caddy TLS and `reverse_proxy` routes
apply.

Only one runtime can be configured per Caddy process: `edge` or `agent`.

## JSON

```json
{
  "apps": {
    "bifrost": {
      "edge": {
        "connector_listen": ":8443",
        "ingress_listen": ":443",
        "tls_cert_file": "/certs/bifrost/server.crt",
        "tls_key_file": "/certs/bifrost/server.key",
        "clients": [
          {
            "endpoint": "home",
            "token": "secret",
            "policy": "replace_existing",
            "max_streams": 100
          }
        ],
        "routes": [
          {
            "server_name": "home.example.com",
            "endpoint": "home"
          }
        ]
      }
    }
  }
}
```

## Development

```sh
go test ./...
go test -race ./...
xcaddy build --with github.com/tunely-eu/caddy-bifrost=.
./caddy list-modules | grep '^bifrost$'
```
