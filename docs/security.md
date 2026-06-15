# Security

`caddy-bifrost` relies on Caddy for certificate automation and on Bifrost for the tunnel transport.

The module is designed for a narrow trust boundary: it makes selected private upstreams reachable from a public Caddy instance, but it does not replace application authentication, network firewalls, secret management, or Caddy's normal TLS policy.

## Connector TLS

The public connector listener always uses TLS and the Bifrost ALPN value. Configure its certificate with:

```caddyfile
{
	bifrost {
		server public.example.com {
			endpoint home {
				token {$HOME_TOKEN}
			}
		}
	}
}
```

Caddy's normal TLS configuration controls issuer selection, DNS-01 providers, account email, storage, and local/internal certificates. For local or private CAs, use Caddy global options such as `local_certs` or `cert_issuer`, then configure the private client with `tls { ca_file ... }` so it trusts the connector certificate.

## Tokens

Endpoint tokens admit private client sessions. Use long random values, keep them out of checked-in Caddyfiles, and rotate them when a private host or config file may have been exposed.

Use environment variables, container secrets, or your normal deployment secret store for token values. A token identifies and admits a connector; it is not an end-user authentication mechanism for the exposed application.

## Public TLS Mode

In Public TLS mode, public Caddy terminates browser HTTPS and then proxies over Bifrost to a private upstream. HTTP routing, access logs, request headers, retries, and application authentication stay in normal Caddy configuration.

This mode is usually the best fit when the public VM is the trusted edge and you want Caddy middleware, request logging, auth plugins, header policy, compression, and retry behavior to apply before traffic enters the tunnel.

## Private TLS Mode

In Private TLS mode, public Caddy only reads ClientHello SNI and forwards the raw TLS stream. The application TLS session terminates on the private Caddy instance.

Listener-wrapper passthrough uses `listener_wrappers { bifrost { route_sni ... } tls }` so normal public-TLS routes can share the same `:443` listener. Private TLS hostnames are not configured as Caddy HTTP routes, so Caddy does not request edge certificates for them.

This mode is useful when certificates, client authentication, or application TLS policy must remain on the private side. Because HTTP traffic does not enter the public Caddy handler chain, public-side HTTP middleware and access logs do not see the application request.

## Admin Exposure

Keep Caddy's admin API and any private-side service listeners on loopback or protected networks. Do not expose private-side `forward` targets directly to the public internet unless that is intentional.

If you publish metrics through a site route, protect that route with network policy or authentication. Metrics intentionally avoid high-cardinality or secret labels, but they can still reveal endpoint names and traffic shape.

## Guardrails

Configure endpoint limits and server guardrails for the size of the deployment. Useful controls include maximum sessions, maximum streams per session, per-session bandwidth, stream idle timeout, header count, and header byte limits.

For single-connector homelab routes, `policy replace_existing` is usually the easiest reconnect behavior: a reconnecting private client takes ownership of the endpoint and closes the stale session.

## Metrics Labels

Bifrost metrics exported through Caddy use endpoint keys and controlled reason/direction values only. Tokens, remote addresses, HTTP paths, and SNI names are not used as Bifrost metric labels.

## Passthrough Stream Observations

Embedded runtimes can install a passthrough stream observer for
`stream_started`, `stream_ended`, and bounded `stream_rejected` events. The
payload is limited to endpoint key, event type, timestamp, controlled
result/reason, and an opaque resolver-provided observation key. It does not
include SNI hostnames, route hostnames, remote addresses, HTTP data,
participant data, tokens, token hashes, or private keys.

The opaque observation key belongs to the embedding runtime. caddy-bifrost does
not interpret it as a service, tenant, dashboard, billing, plan, or hostname
identifier, and it should not be exported as a Prometheus label.

## Passive Latency Observations

The Bifrost server app exposes endpoint-keyed passive latency to embedding
Caddy modules as bounded tunnel/session metadata. The payload is limited to
endpoint key, latency milliseconds, observation time, and controlled state
(`ok`, `unknown`, or `stale`).

Passive latency is derived by Bifrost from tunnel control traffic. It is not a
published application response time, active probe, connection test, Diagnose
result, or HTTP measurement.

The passive latency bridge does not include SNI hostnames, route hostnames,
remote addresses, HTTP paths, HTTP headers, cookies, bodies, content types,
participant data, tokens, token hashes, or private keys. It also does not add
passive latency Prometheus samples; embedders that export observations through
their own status surfaces must keep labels low-cardinality and secret-free.

## Operational Checklist

- Use long random endpoint tokens.
- Keep connector tokens out of git.
- Keep Caddy's admin API private.
- Decide intentionally between Public TLS and Private TLS mode.
- Set endpoint limits before exposing high-traffic services.
- Keep application authentication in Caddy or the private service.
- Treat `tls insecure_skip_verify` as development-only.
