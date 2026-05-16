# Configuration

`caddy-bifrost` is configured as a Caddy app plus an optional `reverse_proxy` transport.

## Server Runtime

```caddyfile
{
	bifrost {
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
	}
}
```

Fields:

| Directive | Meaning |
| --- | --- |
| `server <subject>` | Certificate subject managed by Caddy's TLS app for the connector listener. |
| `listen <addr>` | Listener for private clients. Defaults to `:8443` when omitted. |
| `endpoint <key>` | Stable Bifrost endpoint identity used by transports and passthrough routes. |
| `token <value>` | Shared secret accepted for the endpoint. |
| `policy <mode>` | `reject_if_exists`, `replace_existing`, or `allow_parallel`. |
| `max_parallel <n>` | Maximum active sessions for `allow_parallel`. |

## Endpoint Limits

All Bifrost plan limits are available in Caddyfile:

```caddyfile
limits {
	max_streams 100
	max_bandwidth_bps 25000000
	stream_idle_timeout 5m
}
```

`stream_idle_timeout` uses Caddy duration syntax and is mapped to Bifrost's second-based idle timeout.

## Server Guardrails

All Bifrost server guardrails are available:

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

Unset guardrails use Bifrost defaults.

## Runtime

```caddyfile
runtime {
	handshake_timeout 10s
	stream_copy_buffer_bytes 32768
	tunnel_keepalive_interval 30s
	tunnel_keepalive_timeout 10s
}
```

Unset runtime values use Bifrost defaults.

## Private Client Runtime

```caddyfile
{
	bifrost {
		client public.example.com {
			token {$HOME_TOKEN}
			forward 127.0.0.1:8080
		}
	}
}
```

Fields:

| Directive | Meaning |
| --- | --- |
| `client <connect-host[:port]>` | Public connector address. Defaults to port `8443` when omitted. |
| `token` | Shared token configured on the matching server endpoint. Required. |
| `forward` | Private target reached for each accepted stream. Required. |
| `tls ca_file` | Optional CA bundle for private or self-signed connector certificates. |
| `tls server_name` | Optional TLS server name override. |
| `tls insecure_skip_verify` | Development-only TLS verification bypass. |

For private CA or self-signed connector certificates, configure the public server with Caddy's normal TLS options and trust that CA from the private client:

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

## Reverse Proxy Transport

```caddyfile
reverse_proxy home {
	transport bifrost {
		dial_timeout 5s
	}
}
```

The upstream host is the Bifrost endpoint. `reverse_proxy home` and `reverse_proxy http://home` both use endpoint `home`; the host is not resolved through DNS by the Bifrost transport.

## Metrics

`caddy-bifrost` writes Bifrost endpoint metrics into Caddy's standard metrics registry. Enable Caddy metrics through the global option. Caddy exposes them through the admin `/metrics` endpoint unless the admin API is disabled; use the HTTP `metrics` handler when you want a site route for scraping:

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

The exported Bifrost metrics are:

- `bifrost_endpoint_active_sessions{endpoint_key}`
- `bifrost_endpoint_streams_started_total{endpoint_key}`
- `bifrost_endpoint_streams_ended_total{endpoint_key}`
- `bifrost_endpoint_streams_rejected_total{endpoint_key,reason}`
- `bifrost_endpoint_stream_bytes_total{endpoint_key,direction}`

For `reverse_proxy home { transport bifrost }`, Caddy's HTTP and reverse proxy metrics remain the primary request metrics; Bifrost adds endpoint, stream, and tunnel byte metrics. For SNI passthrough traffic, Bifrost stream and byte metrics cover traffic that does not enter Caddy's HTTP handler chain.

Useful PromQL examples:

```promql
sum by(endpoint_key)(rate(bifrost_endpoint_stream_bytes_total[5m]))
sum by(endpoint_key,direction)(rate(bifrost_endpoint_stream_bytes_total[5m]))
bifrost_endpoint_active_sessions
```

Metrics labels use stable endpoint keys and controlled reason/direction values. Tokens, remote addresses, HTTP paths, and SNI hostnames are not exported as Bifrost metric labels.

## Dynamic Accept Providers

Standalone Caddyfile config uses static `endpoint` blocks for tunnel admission. Embedded builds can provide a custom module implementing `bifrost.AcceptProvider` through the `bifrost.accept_providers` namespace, or use the runtime `WithAcceptProvider` option directly.

When a custom accept provider is configured, static `endpoint` blocks must be omitted. The provider returns Bifrost's native accept decision; caddy-bifrost does not define a second decision model. Bifrost guardrails are still enforced after every decision.

## SNI Passthrough

```caddyfile
{
	servers :443 {
		listener_wrappers {
			bifrost {
				route_sni home.example.com home
			}
			tls
		}
	}
}

:443 {
	abort
}
```

`route_sni <server_name> <endpoint>` maps an exact ClientHello SNI name to a Bifrost endpoint. Matching SNI is forwarded as raw TLS through Bifrost; non-matching SNI continues into Caddy's normal TLS and HTTP pipeline.

The listener wrapper must appear before Caddy's `tls` listener wrapper. Without the explicit `tls` marker, Caddy places custom listener wrappers after TLS, which is too late for raw TLS passthrough.

Private TLS hostnames are not configured as Caddy HTTP site blocks, so Caddy does not include them in automatic HTTPS certificate management. If no public Caddy route exists on the wrapped listener, add a catch-all `:443 { abort }` block to make Caddy open the listener.

Embedded builds can replace static `route_sni` mappings with a custom Caddy module implementing:

```go
ResolvePassthrough(context.Context, string) (endpoint string, ok bool, err error)
```

through the `bifrost.passthrough_resolvers` namespace. Static `route_sni` mappings and `passthrough_resolver` are mutually exclusive. Resolver implementations own any caching or control-plane lookup behavior.
