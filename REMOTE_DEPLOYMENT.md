# Remote Deployment Guide

Quick guide to deploy Wayback Archiver on a remote server.

## Prerequisites

- Linux server with public IP or domain
- PostgreSQL 12+
- Go 1.21+

## 1. Install PostgreSQL

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install postgresql postgresql-contrib

# Start PostgreSQL
sudo systemctl start postgresql
sudo systemctl enable postgresql
```

Create database and user:

```bash
sudo -u postgres psql
```

```sql
CREATE DATABASE wayback;
CREATE USER wayback WITH PASSWORD 'your_secure_password';
GRANT ALL PRIVILEGES ON DATABASE wayback TO wayback;
\q
```

## 2. Configure Server

Create `.env` file in `server/` directory:

```bash
# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=wayback
DB_PASSWORD=your_secure_password
DB_NAME=wayback

# Server
SERVER_HOST=0.0.0.0
SERVER_PORT=8080

# Security (REQUIRED)
AUTH_PASSWORD=your_auth_password
ALLOWED_ORIGINS=http://your-server-ip:8080,null

# Storage
DATA_DIR=/var/wayback/data
```

Create data directory:

```bash
sudo mkdir -p /var/wayback/data
sudo chown $USER:$USER /var/wayback/data
```

## 3. Build and Run Server

```bash
cd server
go build -o wayback-server ./cmd/server
./wayback-server
```

For production, use systemd:

```ini
# /etc/systemd/system/wayback.service
[Unit]
Description=Wayback Archiver
After=network.target postgresql.service

[Service]
Type=simple
User=wayback
WorkingDirectory=/opt/wayback/server
ExecStart=/opt/wayback/server/wayback-server
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl start wayback
sudo systemctl enable wayback
```

## 4. Configure Browser Extension

Edit `browser/src/config.ts`:

```typescript
export const CONFIG = {
  SERVER_URL: 'http://your-server-ip:8080/api/archive',
  AUTH_PASSWORD: 'your_auth_password',  // Same as server
  // ... other settings
}
```

Rebuild:

```bash
cd browser
npm install
npm run build
```

Load the extension from `browser/dist/` directory.

**Note**: The Tampermonkey script is configured with `@connect *` to allow connections to any domain, making it work seamlessly with both local and remote servers.

## 5. Verify

Test server:

```bash
# Should return 401 (auth required)
curl http://your-server-ip:8080/api/pages

# Should return 200
curl -u wayback:your_auth_password http://your-server-ip:8080/api/pages
```

Test browser extension:
1. Visit any webpage
2. Open console, check for `[Wayback] ✓ Archived`

## Security Notes

- **Always set `AUTH_PASSWORD`** for remote deployment
- **Restrict `ALLOWED_ORIGINS`** to trusted domains only
- Configure firewall:
  ```bash
  sudo ufw allow 8080/tcp
  sudo ufw allow 22/tcp
  sudo ufw enable
  ```

## Backup

Database backup:

```bash
pg_dump -U wayback wayback | gzip > wayback_$(date +%Y%m%d).sql.gz
```

Files backup:

```bash
tar -czf wayback_data_$(date +%Y%m%d).tar.gz /var/wayback/data
```

## Troubleshooting

**CORS error**: Add your domain to `ALLOWED_ORIGINS`

**401 error**: Check `AUTH_PASSWORD` matches in server and browser config

**Connection refused**: Check firewall and server is running on `0.0.0.0`

**Database error**: Verify PostgreSQL is running and credentials are correct

---

For detailed security information, see [docs/security/README.md](docs/security/README.md)
