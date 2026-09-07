# Security Features

AxonHub provides multiple layers of IP-based access control to secure your AI gateway.

## Trusted Reverse Proxies

Forwarded client-IP headers are ignored by default. If AxonHub runs behind a
reverse proxy, configure only that proxy's actual IP addresses or networks:

```yaml
server:
  trusted_proxies:
    - "127.0.0.1/32"
    - "::1/128"
```

This example is for a local proxy; use the real proxy ranges for your deployment.
Only connections from trusted proxies may supply `X-Forwarded-For` or
`X-Real-IP`. Other connections use their TCP peer address, preventing forged
headers from bypassing IP restrictions. Do not trust all networks as a shortcut.
After upgrading, review reverse-proxy configuration before relying on client-IP
allowlists, blocklists or per-key restrictions. No production configuration is
changed automatically.

## IP Access Control (Global Allowlist)

IP Access Control is a global middleware that restricts access to the entire AxonHub instance. When enabled, only requests from IP addresses or CIDR ranges in the allowlist are accepted; all others are denied.

This is useful when you want to lock down your AxonHub instance to only accept traffic from known networks (e.g., your corporate VPN or cloud VPC).

### Configuration

IP Access Control is configured via the AxonHub configuration file or environment variables:

```yaml
ip_access_control:
  enabled: true
  allowed_ips:
    - "10.0.0.0/8"
    - "192.168.1.0/24"
    - "203.0.113.10"
  redirect_url: ""  # Optional: redirect denied requests to this URL
```

### Behavior

- When **disabled** (default): all requests are allowed through.
- When **enabled**: requests from IPs matching any entry in `allowed_ips` are allowed; all others receive a 404 (or 302 redirect if `redirect_url` is set).
- The client IP uses the trusted-proxy validation above, falling back to the TCP peer address.

## IP Blocklist (System-Wide Denylist)

The IP Blocklist allows administrators to block specific IP addresses or CIDR ranges from making API requests. This is managed at runtime through the system settings UI.

### Configuration

IP Blocklist is configured through System Settings:

```json
{
  "blocked_ips": ["203.0.113.50", "198.51.100.0/24"]
}
```

### Behavior

- Requests from blocked IPs receive a 403 Forbidden response.
- The blocklist applies to all external API requests.
- Changes take effect immediately through the settings UI.

## API Key IP Restriction (Per-Key Allowlist)

API Key IP Restriction allows you to restrict individual API keys to only accept requests from specified source IP addresses or CIDR ranges. This is useful for limiting API key usage to specific servers or networks.

### How It Works

When an API key has `allowed_ips` configured, AxonHub checks the source IP of every request made with that key. If the source IP does not match any entry in the allowlist, the request is rejected with a 403 Forbidden response.

### Source IP Detection

API-key restrictions use the same trusted-proxy client-IP resolution as the
global controls. Forwarded headers are not trusted merely because they exist.
Configure `server.trusted_proxies` for the actual reverse proxy (such as Nginx,
Cloudflare or AWS ALB); otherwise the TCP peer address is used.

### Configuration

You can configure IP restrictions when creating or editing an API key:

1. Navigate to **API Keys** in the dashboard.
2. Click **Create API Key** or edit an existing key.
3. Enable the **IP Restriction** toggle.
4. Enter one or more IP addresses or CIDR ranges, separated by commas (e.g., `192.168.1.0/24, 10.0.0.5, 2001:db8::/32`).
5. Save the API key.

### Supported Formats

- **Single IP**: `203.0.113.10` or `2001:db8::10`
- **CIDR Range**: `203.0.113.0/24` or `2001:db8::/32`

Both IPv4 and IPv6 are supported.

### Example

```bash
# Create an API key with IP restriction
curl -X POST https://your-axonhub-instance/api/api-keys \
  -H "Authorization: Bearer your-admin-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Production Server Key",
    "allowed_ips": ["10.0.0.0/8", "192.168.1.100"]
  }'

# Request from an allowed IP - succeeds
curl https://your-axonhub-instance/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Request from a non-allowed IP - 403 Forbidden
curl https://your-axonhub-instance/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
# Returns: 403 IP address is not allowed for this API key
```

## Layered Security

These three features work together to provide defense in depth:

| Feature | Scope | Action | When to Use |
|---------|-------|--------|-------------|
| IP Access Control | Global | Allow only listed IPs | Lock down entire instance |
| IP Blocklist | Global | Block specific IPs | Block abusive IPs at runtime |
| API Key IP Restriction | Per-Key | Allow only listed IPs | Restrict individual API keys |

Requests are evaluated in order: IP Access Control → IP Blocklist → API Key Authentication → API Key IP Restriction.
