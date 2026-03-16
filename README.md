# Wayback Archiver

> *The Memory of Your Internet — Archive Everything You Browse.*

English | [中文](README-zh.md)

A self-hosted personal web archiving system that automatically captures and preserves web pages you visit in Chrome — HTML, CSS, JavaScript, images, and all. When the original page goes offline, you can still browse your archived copy with styles and layout intact.

![index](./screenshot/index.webp)  
![x](./screenshot/x.webp)   
![v2ex](./screenshot/v2ex.webp)  

## How It Works

```
Chrome + Tampermonkey ──HTTP POST──▶ Go Server ──▶ PostgreSQL (metadata)
  (auto-capture on                    │               + File System (assets)
   page load)                         │
                                      ▼
                                   Web UI ──▶ Browse / Search / Replay
```

1. A Tampermonkey userscript runs in your browser, automatically capturing the full DOM and resources once the page finishes loading. If significant DOM changes occur afterward, it submits one additional update.
2. The Go server receives the snapshot, downloads any cross-origin resources the browser couldn't fetch, deduplicates everything by content hash, and stores it locally.
3. A built-in Web UI lets you list, search, and replay any archived page — fully offline, no external dependencies.

## Features

- **High-fidelity replay** — CSSOM serialization, computed styles inlining, and anti-refresh protection reproduce pages as close to the original as possible
- **Full-page capture** — HTML, CSS, JS, images, fonts; resource URLs are rewritten to local paths
- **Cross-origin resource recovery** — server-side extraction and download of resources blocked by CORS
- **Content-hash deduplication** — identical resources shared across pages are stored only once (SHA-256)
- **Version history** — same URL archived multiple times, distinguished by timestamp
- **Timeline view** — browse all snapshots of a URL on a visual timeline (like web.archive.org), with prev/next navigation between snapshots
- **Smart dedup** — session-level + server-level dedup prevents redundant captures; content-hash comparison skips unchanged pages
- **Dynamic content support** — captures the live DOM state; MutationObserver triggers one auto-update if significant changes occur after initial capture
- **SPA-aware** — detects SPA navigation, resets capture state per route
- **Anti-refresh protection** — archived pages are frozen: timers, WebSockets, and navigation APIs are neutralized
- **Web UI** — responsive interface to browse, full-text search (page content, URL, and title), filter by date range and domain, and replay archived pages
- **RESTful API** — programmatic access to all archiving and query operations

## Prerequisites

- **PostgreSQL** 14+
- **Chrome** or **Firefox** + [Tampermonkey](https://www.tampermonkey.net/) extension (v5.3+)

## Quick Start

### Option A: Docker (Recommended)

The fastest way to get started. Docker Compose will set up both the server and PostgreSQL automatically.

```bash
# Clone the repository
git clone https://github.com/icodeface/wayback-archiver.git
cd wayback-archiver

# Start all services
docker compose up -d

# View logs
docker compose logs -f wayback
```

The server will be available at `http://localhost:8080`. Skip to [step 4 (Install the Userscript)](#4-install-the-userscript).

For detailed Docker configuration and deployment options, see [docs/DOCKER.md](docs/DOCKER.md).

### Option B: Pre-built Binaries

### 1. Download Pre-built Binaries

Download the latest release from the [Releases page](https://github.com/icodeface/wayback-archiver/releases):

- **macOS**: `wayback-server-darwin-amd64.tar.gz` (Intel) or `wayback-server-darwin-arm64.tar.gz` (Apple Silicon)
- **Linux**: `wayback-server-linux-amd64.tar.gz` or `wayback-server-linux-arm64.tar.gz`
- **Windows**: `wayback-server-windows-amd64.zip`
- **Userscript**: `wayback.user.js`

Extract the archive:

```bash
# macOS/Linux
tar -xzf wayback-server-*.tar.gz

# Windows: extract the .zip file
```

> **Building from source?** See [docs/BUILD.md](docs/BUILD.md) for manual compilation instructions.

### 2. Database Setup

```bash
# PostgreSQL 默认使用当前系统用户名作为数据库用户
# 如果你的系统用户名是 alice，以下命令等同于 createdb -U alice wayback
createdb wayback

# Run the schema (init_db.sql is included in the release archive)
psql wayback < init_db.sql
```

### 3. Start the Server

```bash
# Optional: create .env file for custom configuration
# See Configuration section below for available options

./wayback-server
```

The server starts at `http://localhost:8080` by default.

If you need a proxy for downloading external resources:

```bash
export http_proxy=http://127.0.0.1:7897
export https_proxy=http://127.0.0.1:7897
./wayback-server
```

### 4. Install the Userscript

1. Download `wayback.user.js` from the [Releases page](https://github.com/icodeface/wayback-archiver/releases)
2. Open Tampermonkey dashboard in your browser
3. Click "Create a new script"
4. Paste the contents of `wayback.user.js`
5. Save and enable

> **Chrome users:** Right-click the Tampermonkey icon → Manage extension, then enable the "Allow user scripts" toggle. Firefox does not require this step.

### 5. Start Browsing

That's it. Pages are automatically archived as soon as they load. Open `http://localhost:8080` to browse your archive.

## Configuration

Environment variables (or `.env` file in the project root):

The server automatically loads `.env` from the working directory if it exists. You can also set environment variables directly.

| Variable | Default | Description |
|---|---|---|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | Database user (PostgreSQL 默认使用系统用户名，建议不设置此变量) |
| `DB_PASSWORD` | *(empty)* | Database password |
| `DB_NAME` | `wayback` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode |
| `SERVER_HOST` | `127.0.0.1` | Server bind address (`0.0.0.0` = all interfaces, `127.0.0.1` = localhost only) |
| `SERVER_PORT` | `8080` | HTTP server port |
| `ALLOWED_ORIGINS` | `http://localhost:8080,http://127.0.0.1:8080,null` | CORS allowed origins (comma-separated). For remote deployment, add your domain: `https://your-domain.com,null` |
| `DATA_DIR` | `./data` | Storage directory for HTML and resources |
| `LOG_DIR` | `./data/logs` | Log file directory |
| `AUTH_PASSWORD` | *(empty)* | HTTP Basic Auth password (disabled when empty, username: `wayback`). **REQUIRED for remote deployment** |

## Remote Deployment

For deploying to a remote server, see [REMOTE_DEPLOYMENT.md](docs/REMOTE_DEPLOYMENT.md) for detailed instructions.

Quick setup:

```bash
# .env configuration
ALLOWED_ORIGINS=https://your-domain.com,null
AUTH_PASSWORD=your_secure_password
SERVER_HOST=0.0.0.0

# Browser config.ts
SERVER_URL: 'https://your-domain.com/api/archive'
AUTH_PASSWORD: 'your_secure_password'
```

**Security Notes:**
- Always use HTTPS for remote deployment
- Set a strong `AUTH_PASSWORD`
- Limit `ALLOWED_ORIGINS` to trusted domains only
- Both CORS and Basic Auth are required for security (defense in depth)

## API

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/version` | Server version and build info |
| `POST` | `/api/archive` | Create a page archive |
| `PUT` | `/api/archive/:id` | Update an existing archive snapshot |
| `GET` | `/api/pages` | List all archived pages |
| `GET` | `/api/pages/:id` | Get page details |
| `GET` | `/api/pages/:id/content` | Get page content as Markdown (for AI/LLM consumption) |
| `GET` | `/api/search?q=keyword` | Search pages by URL or title |
| `GET` | `/api/pages/timeline?url=URL` | Get all snapshots of a URL (timeline view) |
| `GET` | `/api/logs` | List available log files |
| `GET` | `/api/logs/latest` | Get latest log file content (supports `?tail=N&grep=keyword`) |
| `GET` | `/api/logs/:filename` | Get log file content (supports `?tail=N&grep=keyword`) |
| `GET` | `/view/:id` | Replay an archived page |
| `GET` | `/timeline?url=URL` | Visual timeline page for a URL |
| `GET` | `/logs` | Server logs viewer |

### POST /api/archive

Returns `{ status, page_id, action }` where `action` is `created` or `unchanged` (content identical, only `last_visited` updated).

### PUT /api/archive/:id

Accepts the same body as POST. Replaces the snapshot content — old HTML and resource associations are removed, resources are re-processed. Returns `{ status, page_id, action }` where `action` is `updated` or `unchanged`.

## Project Structure

```
wayback-archiver/
├── Makefile                  # Build, test, cross-compile
├── bin/                      # Build output (server binary + userscript)
├── browser/                  # Tampermonkey userscript (TypeScript)
│   ├── src/
│   │   ├── main.ts           # Entry point & orchestration
│   │   ├── config.ts         # Constants
│   │   ├── types.ts          # TypeScript interfaces
│   │   ├── page-filter.ts    # URL filtering logic
│   │   ├── page-freezer.ts   # Freeze page runtime state
│   │   ├── dom-collector.ts  # DOM serialization
│   │   └── archiver.ts       # Server communication
│   ├── dist/                 # Built userscript
│   └── build.js              # Bundle script
│
├── server/                   # Go backend
│   ├── cmd/server/main.go    # Entry point
│   ├── internal/
│   │   ├── api/              # HTTP handlers (modular)
│   │   ├── config/           # Environment-based config
│   │   ├── database/         # PostgreSQL operations
│   │   ├── logging/          # File-based logging with rotation
│   │   ├── models/           # Data models
│   │   └── storage/          # File storage & dedup
│   └── web/                  # Web UI static files
│
├── .env.example              # Configuration template
└── tests/                    # Test suites
    ├── browser/              # Browser-side tests
    └── server/               # Server-side & E2E tests
```

## Storage Layout

```
data/
├── html/                     # HTML snapshots, organized by date
│   └── 2026/03/09/
│       └── <timestamp>_<hash>.html
├── logs/                     # Server logs, rotated by size (10MB) and date (7-day retention)
│   ├── wayback-2026-03-12.001.log
│   └── wayback-2026-03-12.002.log
└── resources/                # Deduplicated static resources
    └── ab/cd/
        └── <sha256>.css
```

## Building from Source

See [docs/BUILD.md](docs/BUILD.md) for build instructions, cross-compilation, and testing.

## Agent Integration

This project includes an [Agent skill](skill.md) for AI-assisted querying and exploration of your archived pages. Use it to search, analyze, and interact with your archive through natural language.

## Known Limitations

- Some cross-origin resources may still fail due to server-side 403/404 responses
- Dynamically injected scripts (loaded via JS at runtime) may not be captured
- Tracking pixels and analytics URLs with dynamic parameters are not preserved (they don't affect page rendering)
- Very large media files (video, large images) will consume significant storage

## License

MIT
