# Docker Deployment Guide

## Quick Start

### 1. Using Docker Compose (Recommended)

```bash
# Start all services (server + PostgreSQL)
docker compose up -d

# View logs
docker compose logs -f wayback

# Stop services
docker compose down

# Stop and remove all data
docker compose down -v  # This only removes postgres_data volume; ./data/ directory remains on host
```

The server will be available at http://localhost:8080

### 2. Using Docker Only

```bash
# Build image
docker build -t wayback-archiver:latest .

# Run PostgreSQL
docker run -d \
  --name wayback-postgres \
  -e POSTGRES_USER=wayback \
  -e POSTGRES_PASSWORD=wayback \
  -e POSTGRES_DB=wayback \
  -v wayback_postgres:/var/lib/postgresql/data \
  -v $(pwd)/server/init_db.sql:/docker-entrypoint-initdb.d/init_db.sql:ro \
  postgres:16-alpine

# Run server
docker run -d \
  --name wayback-server \
  --link wayback-postgres:postgres \
  -e DB_HOST=postgres \
  -e DB_USER=wayback \
  -e DB_PASSWORD=wayback \
  -e DB_NAME=wayback \
  -e SERVER_HOST=0.0.0.0 \
  -v $(pwd)/data:/app/data \
  -p 8080:8080 \
  wayback-archiver:latest
```

## Configuration

### Environment Variables

Create a `.env` file in the project root:

```bash
# Optional: Set version info
VERSION=v1.0.0
BUILD_TIME=2026-03-16

# Optional: Configure CORS
ALLOWED_ORIGINS=http://localhost:8080,https://your-domain.com,null

# Optional: Set authentication password
AUTH_PASSWORD=your-secure-password
```

### Remote Deployment

For remote deployment, you MUST set `AUTH_PASSWORD`:

```bash
# In .env file
AUTH_PASSWORD=your-secure-password
ALLOWED_ORIGINS=https://your-domain.com,null
```

Then update your userscript to include the password:

```javascript
// In browser/src/config.ts
export const API_BASE_URL = 'https://your-domain.com';
export const AUTH_PASSWORD = 'your-secure-password';
```

## Data Persistence

- **PostgreSQL** — Docker named volume `postgres_data`，由 Docker 管理
- **Archived pages & resources** — Bind mount 到宿主机 `./data/` 目录，可直接访问

### Backup

```bash
# Backup database
docker exec wayback-postgres pg_dump -U wayback wayback > backup.sql

# Archived files are already on the host at ./data/, just copy the directory
cp -r ./data ./data-backup
```

### Restore

```bash
# Restore database
docker exec -i wayback-postgres psql -U wayback wayback < backup.sql

# Restore archived files
cp -r ./data-backup/* ./data/
```

## Troubleshooting

### Check service status

```bash
docker compose ps
```

### View logs

```bash
# All services
docker compose logs

# Server only
docker compose logs wayback

# Follow logs
docker compose logs -f
```

### Access database

```bash
docker exec -it wayback-postgres psql -U wayback wayback
```

### Rebuild after code changes

```bash
docker compose down
docker compose build --no-cache
docker compose up -d
```

## Health Check

The server includes a health check endpoint:

```bash
curl http://localhost:8080/api/version
```

Docker will automatically restart the container if the health check fails.
