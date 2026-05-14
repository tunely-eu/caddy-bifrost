# Security

`caddy-bifrost` relies on Caddy for certificate automation and on Bifrost for the tunnel transport.

## Connector TLS

The public connector listener always uses TLS and the Bifrost ALPN value. Configure its certificate with:

```caddyfile
connector :8443 {
	tls public.example.com
	endpoint home {
		token {$HOME_TOKEN}
	}
}
```

Caddy's normal TLS configuration controls issuer selection, DNS-01 providers, account email, storage, and local/internal certificates.

## Tokens

Endpoint tokens admit private client sessions. Use long random values, keep them out of checked-in Caddyfiles, and rotate them when a private host or config file may have been exposed.

## Public TLS Mode

In Public TLS mode, public Caddy terminates browser HTTPS and then proxies over Bifrost to a private upstream. HTTP routing, access logs, request headers, retries, and application authentication stay in normal Caddy configuration.

## Private TLS Mode

In Private TLS mode, public Caddy only reads ClientHello SNI and forwards the raw TLS stream. The application TLS session terminates on the private Caddy instance.

If `passthrough :443` owns port 443, avoid TLS-ALPN-01 for the connector certificate on the same port. Use DNS-01 or HTTP-01 on port 80 instead.

## Admin Exposure

Keep Caddy's admin API and any private-side service listeners on loopback or protected networks. Do not expose private-side `forward` targets directly to the public internet unless that is intentional.

