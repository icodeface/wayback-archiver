---
name: wayback
description: Query and explore the Wayback Archiver personal web archiving system
---

# Wayback Archiver

Wayback Archiver is a self-hosted personal web archiving system that captures and preserves web pages with full fidelity — HTML, CSS, JavaScript, images, fonts, and all resources.

**Repository**: https://github.com/icodeface/wayback-archiver

## When to use

Use this skill when the user wants to:
- Search archived web pages
- View snapshots of a specific URL
- List recent archives or filter by date range
- Get details about a specific archived page
- Read page content in Markdown format (for AI/LLM consumption)
- Explore the archiving system's data

## Prerequisites

Before using this skill, ensure the Wayback Archiver server is running:

- **Git**
- **Go** 1.21+
- **Node.js** 16+
- **PostgreSQL** 14+
- **Chrome** with [Tampermonkey](https://www.tampermonkey.net/) extension

## Quick Start

### 1. Clone and Setup

```bash
git clone https://github.com/icodeface/wayback-archiver.git
cd wayback-archiver

# Create database
createdb -U postgres wayback
psql -U postgres wayback < server/init_db.sql
```

### 2. Start Server

```bash
cd server
go build -o wayback-server ./cmd/server
./wayback-server
```

Server runs at `http://localhost:8080` by default.

### 3. Install Browser Script

```bash
cd browser
npm install
npm run build
```

Install `browser/dist/wayback.user.js` in Tampermonkey.

## API Usage

Base URL: `http://localhost:8080`

### Authentication (Optional)

When `AUTH_PASSWORD` is set, use HTTP Basic Auth:
- **Username**: `wayback`
- **Password**: `$AUTH_PASSWORD`

### Endpoints

#### Search Pages

```bash
curl "http://localhost:8080/api/search?q=$KEYWORD"
```

#### List All Pages

```bash
curl "http://localhost:8080/api/pages?limit=100&offset=0"
```

#### Get Page Details

```bash
curl "http://localhost:8080/api/pages/$PAGE_ID"
```

#### Get Page Content as Markdown

Returns the page body as clean Markdown (strips scripts, nav, footer, etc.). Ideal for AI/LLM consumption.

```bash
curl "http://localhost:8080/api/pages/$PAGE_ID/content"
```

#### Get Timeline for URL

```bash
curl "http://localhost:8080/api/pages/timeline?url=$ENCODED_URL"
```

#### Filter by Date Range

```bash
curl "http://localhost:8080/api/pages?from=2026-03-01&to=2026-03-12"
```

#### View Archived Page

Open in browser:
```
http://localhost:8080/view/$PAGE_ID
```

## Data Models

### Page

```typescript
{
  id: number;
  url: string;
  title: string;
  captured_at: string;      // ISO 8601 timestamp
  html_path: string;
  content_hash: string;     // SHA-256
  first_visited: string;    // ISO 8601 timestamp
  last_visited: string;     // ISO 8601 timestamp
  created_at: string;       // ISO 8601 timestamp
}
```

### Resource

```typescript
{
  id: number;
  url: string;
  content_hash: string;     // SHA-256
  resource_type: string;    // "css", "js", "image", "font", etc.
  file_path: string;
  file_size: number;
  first_seen: string;       // ISO 8601 timestamp
  last_seen: string;        // ISO 8601 timestamp
}
```

## Storage Layout

```
data/
├── html/                     # HTML snapshots, organized by date
│   └── 2026/03/12/
│       └── <timestamp>_<hash>.html
└── resources/                # Deduplicated static resources
    └── ab/cd/
        └── <sha256>.css
```

## Features

- **High-fidelity replay**: CSSOM serialization, computed styles inlining
- **Full-page capture**: HTML, CSS, JS, images, fonts
- **Cross-origin resource recovery**: Server-side download of CORS-blocked resources
- **Content-hash deduplication**: SHA-256 based, shared resources stored once
- **Version history**: Multiple snapshots per URL, distinguished by timestamp
- **Smart dedup**: Session-level + server-level prevents redundant captures
- **Dynamic content support**: Captures live DOM state, auto-updates on significant mutations
- **SPA-aware**: Detects SPA navigation, resets capture state per route
- **Anti-refresh protection**: Archived pages frozen (timers, WebSockets, navigation APIs neutralized)

## Testing

```bash
# Go unit tests
cd server && go test ./... -v

# E2E tests (requires Chrome)
cd tests/server && node test_update_feature.js
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Server won't start | Check PostgreSQL is running and database exists |
| Pages not archiving | Verify Tampermonkey script is enabled and server is reachable |
| Missing resources | Check proxy settings if behind corporate firewall |
| Authentication errors | Verify `AUTH_PASSWORD` env var matches your curl credentials |

## Configuration

Create `.env` file in `server/` directory:

```bash
# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=
DB_NAME=wayback
DB_SSLMODE=disable

# Server
SERVER_PORT=8080

# Storage
DATA_DIR=./data

# Authentication (optional)
AUTH_PASSWORD=
```

## License

MIT
