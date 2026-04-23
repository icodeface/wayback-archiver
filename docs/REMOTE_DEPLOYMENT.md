# Remote Deployment Guide

Quick guide to deploy Wayback Archiver on a remote server.

## Prerequisites

- Linux server with public IP or domain
- Go 1.21+ and GCC (for building from source)
- **PostgreSQL 12+** (recommended for remote deployment with multiple users)

## 1. Database Setup

### PostgreSQL (Recommended)

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

### SQLite (Alternative for single-user)

If you prefer SQLite for a single-user remote deployment, no database setup is required. The database file will be created automatically. Skip to step 2 and use the SQLite configuration.

## 2. Configure Server

Create `.env` file in project root:

**PostgreSQL configuration (recommended):**

```bash
# Database
DB_TYPE=postgres
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
ENABLE_DEBUG_API=false
ALLOWED_ORIGINS=http://your-server-ip:8080

# Storage
DATA_DIR=/var/wayback/data
```

**SQLite configuration (alternative for single-user):**

```bash
# Database
DB_TYPE=sqlite
DB_PATH=/var/wayback/data/wayback.db

# Server
SERVER_HOST=0.0.0.0
SERVER_PORT=8080

# Security (REQUIRED)
AUTH_PASSWORD=your_auth_password
ENABLE_DEBUG_API=false
ALLOWED_ORIGINS=http://your-server-ip:8080

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
make build
./bin/wayback-server
```

For production, use systemd:

```ini
# /etc/systemd/system/wayback.service
[Unit]
Description=Wayback Archiver
After=network.target

[Service]
Type=simple
User=wayback
WorkingDirectory=/opt/wayback
ExecStart=/opt/wayback/bin/wayback-server
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
make script
```

Load the extension from `bin/wayback-userscript.js`.

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
- Keep `ENABLE_DEBUG_API=false` unless you are actively debugging the server
- **Restrict `ALLOWED_ORIGINS`** to trusted domains only
- Configure firewall:
  ```bash
  sudo ufw allow 8080/tcp
  sudo ufw allow 22/tcp
  sudo ufw enable
  ```

## Backup

**SQLite backup:**

```bash
# Database file
cp /var/wayback/data/wayback.db wayback_$(date +%Y%m%d).db

# Or compressed
gzip -c /var/wayback/data/wayback.db > wayback_$(date +%Y%m%d).db.gz
```

**PostgreSQL backup:**

```bash
pg_dump -U wayback wayback | gzip > wayback_$(date +%Y%m%d).sql.gz
```

**Files backup:**

```bash
tar -czf wayback_data_$(date +%Y%m%d).tar.gz /var/wayback/data
```

## Troubleshooting

**CORS error**: Add your domain to `ALLOWED_ORIGINS`

**401 error**: Check `AUTH_PASSWORD` matches in server and browser config

**Connection refused**: Check firewall and server is running on `0.0.0.0`

**Database error**: Verify database configuration; for PostgreSQL check it's running and credentials are correct

---

For detailed security information, see [docs/security/README.md](docs/security/README.md)
