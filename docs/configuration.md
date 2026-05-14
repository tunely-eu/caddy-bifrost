# Configuration

`caddy-bifrost` is configured as a Caddy app plus an optional `reverse_proxy` transport.

## Server Runtime

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
						max_bandwidth_bps 25000000
						stream_idle_timeout 5m
					}
				}
			}
		}
	}
}
```

Fields:

| Directive | Meaning |
| --- | --- |
| `connector <listen>` | Listener for private clients. Defaults to `:8443` when omitted. |
| `tls <subject>` | Certificate subject managed by Caddy's TLS app for the connector listener. |
| `endpoint <key>` | Stable Bifrost endpoint identity used by transports and SNI routes. |
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
		client {
			connect public.example.com:8443
			token {$HOME_TOKEN}
			forward 127.0.0.1:8080
			tls_server_name public.example.com
		}
	}
}
```

Fields:

| Directive | Meaning |
| --- | --- |
| `connect` | Public connector address. Required. |
| `token` | Shared token configured on the matching server endpoint. Required. |
| `forward` | Private target reached for each accepted stream. Required. |
| `tls_ca_file` | Optional CA bundle for private or self-signed connector certificates. |
| `tls_server_name` | Optional TLS server name override. |
| `tls_insecure_skip_verify` | Development-only TLS verification bypass. |

## Reverse Proxy Transport

```caddyfile
reverse_proxy http://home {
	transport bifrost {
		endpoint home
		dial_timeout 5s
	}
}
```

`endpoint` must match an endpoint key configured on the server runtime.

## SNI Passthrough

```caddyfile
passthrough :443 {
	route_sni home.example.com home
	route_sni files.example.com home
}
```

Public Caddy reads ClientHello SNI only and forwards the raw TLS stream through Bifrost to the selected endpoint.

