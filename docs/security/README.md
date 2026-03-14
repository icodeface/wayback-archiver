# Security Documentation

This directory contains security-related documentation for Wayback Archiver.

## Security Fixes

- [2026-03-14 Security Fixes](FIXES_2026-03-14.md) - Medium severity vulnerabilities fixed
  - CORS restrictions
  - Resource deduplication race conditions
  - Resource download size limits
  - JSON parsing error handling

## Security Features

### SSRF Protection
- Blocks private IP addresses (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8)
- Blocks cloud metadata services (169.254.169.254)
- Only allows HTTP/HTTPS protocols

### CORS Protection
- Configurable origin whitelist via `ALLOWED_ORIGINS`
- Default: localhost and file:// protocol only
- Prevents cross-site request forgery (CSRF)

### Cookie Leakage Prevention
- Only forwards cookies to same root domain
- Supports multi-segment TLDs (co.uk, com.au, etc.)

### Resource Size Limits
- Maximum download size: 200MB
- Prevents memory exhaustion attacks

### Path Traversal Prevention
- Validates all resource paths
- Prevents access to files outside data directory

### CSP (Content Security Policy)
- Dynamic nonce generation for inline scripts
- Prevents XSS attacks in archived pages

## Reporting Security Issues

If you discover a security vulnerability, please:

1. **Do NOT** open a public issue
2. Email the maintainer directly
3. Include detailed steps to reproduce
4. Allow time for a fix before public disclosure

## Security Best Practices

### For Local Development
- No special configuration needed
- Default settings are secure for localhost

### For Remote Deployment
- **REQUIRED**: Set `AUTH_PASSWORD`
- **REQUIRED**: Use HTTPS (via Nginx/Caddy)
- **REQUIRED**: Limit `ALLOWED_ORIGINS` to trusted domains
- **RECOMMENDED**: Configure firewall (only ports 443, 22)
- **RECOMMENDED**: Regular backups
- **RECOMMENDED**: Monitor logs

See [REMOTE_DEPLOYMENT.md](../../REMOTE_DEPLOYMENT.md) for detailed deployment guide.

## Security Testing

Run security tests:

```bash
# API security tests
go test ./server/internal/api -v -run Security

# Storage security tests
go test ./server/internal/storage -v -run Security

# All tests
go test ./server/... -v
```

---

**Last Updated**: 2026-03-14
