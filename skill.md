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

- **PostgreSQL** 14+
- **Chrome** or **Firefox** with [Tampermonkey](https://www.tampermonkey.net/) extension (v5.3+)

## Quick Start

### 1. Download Pre-built Binaries

Download from [Releases](https://github.com/icodeface/wayback-archiver/releases):

- **Server binary**: `wayback-server-<os>-<arch>.tar.gz` (or `.zip` for Windows)
- **Userscript**: `wayback.user.js`

Extract the server binary:

```bash
# macOS/Linux
tar -xzf wayback-server-*.tar.gz

# Windows: extract the .zip file
```

> **Building from source?** See [docs/BUILD.md](https://github.com/icodeface/wayback-archiver/blob/main/docs/BUILD.md).

### 2. Setup Database

```bash
# Create database (PostgreSQL 默认使用当前系统用户名)
createdb wayback

# Run schema (init_db.sql is included in the release archive)
psql wayback < init_db.sql
```

### 3. Start Server

```bash
./wayback-server
```

Server runs at `http://localhost:8080` by default.

### 4. Install Browser Script

1. Download `wayback.user.js` from [Releases](https://github.com/icodeface/wayback-archiver/releases)
2. Open Tampermonkey dashboard
3. Create new script and paste the content
4. Save and enable

> **Chrome users:** Enable "Allow user scripts" in Tampermonkey's extension settings (right-click icon → Manage extension). Firefox does not require this.

## API Usage

Base URL: `http://localhost:8080`

### Authentication (Optional)

When `AUTH_PASSWORD` is set, use HTTP Basic Auth:
- **Username**: `wayback`
- **Password**: `$AUTH_PASSWORD`

### Endpoints

#### List All Pages

```bash
curl "http://localhost:8080/api/pages?limit=100&offset=0"
```

#### Search Pages

```bash
curl "http://localhost:8080/api/search?q=$KEYWORD"
```

#### Filter by Date Range

```bash
curl "http://localhost:8080/api/pages?from=2026-03-01&to=2026-03-12"
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

See [docs/BUILD.md](https://github.com/icodeface/wayback-archiver/blob/main/docs/BUILD.md) for build and test instructions.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Server won't start | Check PostgreSQL is running and database exists |
| Pages not archiving | Verify Tampermonkey script is enabled and server is reachable |
| Missing resources | Check proxy settings if behind corporate firewall |
| Authentication errors | Verify `AUTH_PASSWORD` env var matches your curl credentials |

## Configuration

Create `.env` file in the project root (or set environment variables directly):

The server automatically loads `.env` from the working directory if it exists.

```bash
# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres  # 可选，PostgreSQL 默认使用系统用户名
DB_PASSWORD=
DB_NAME=wayback
DB_SSLMODE=disable

# Server
SERVER_HOST=0.0.0.0  # 默认 127.0.0.1，设置 0.0.0.0 监听所有网卡
SERVER_PORT=8080

# Storage
DATA_DIR=./data

# Authentication (optional)
AUTH_PASSWORD=
```

## License

MIT
