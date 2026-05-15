# Security

`caddy-bifrost` relies on Caddy for certificate automation and on Bifrost for the tunnel transport.

## Connector TLS

The public connector listener always uses TLS and the Bifrost ALPN value. Configure its certificate with:

```caddyfile
server public.example.com {
	endpoint home {
		token {$HOME_TOKEN}
	}
}
```

Caddy's normal TLS configuration controls issuer selection, DNS-01 providers, account email, storage, and local/internal certificates. For local or private CAs, use Caddy global options such as `local_certs` or `cert_issuer`, then configure the private client with `tls { ca_file ... }` so it trusts the connector certificate.

## Tokens

Endpoint tokens admit private client sessions. Use long random values, keep them out of checked-in Caddyfiles, and rotate them when a private host or config file may have been exposed.

## Public TLS Mode

In Public TLS mode, public Caddy terminates browser HTTPS and then proxies over Bifrost to a private upstream. HTTP routing, access logs, request headers, retries, and application authentication stay in normal Caddy configuration.

## Private TLS Mode

In Private TLS mode, public Caddy only reads ClientHello SNI and forwards the raw TLS stream. The application TLS session terminates on the private Caddy instance.

Listener-wrapper passthrough uses `listener_wrappers { bifrost { route_sni ... } tls }` so normal public-TLS routes can share the same `:443` listener. Private TLS hostnames are not configured as Caddy HTTP routes, so Caddy does not request edge certificates for them.

## Admin Exposure

Keep Caddy's admin API and any private-side service listeners on loopback or protected networks. Do not expose private-side `forward` targets directly to the public internet unless that is intentional.

## Metrics Labels

Bifrost metrics exported through Caddy use endpoint keys and controlled reason/direction values only. Tokens, remote addresses, HTTP paths, and SNI names are not used as Bifrost metric labels.
