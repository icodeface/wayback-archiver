# Wayback Archiver

> *The Memory of Your Internet — Archive Everything You Browse.*

English | [Chinese](README-zh.md)

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
2. The Go server acknowledges archive/update requests as soon as the snapshot metadata is stored. Resource downloading, deduplication, URL rewriting, and snapshot finalization continue in the background.
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
- **Userscript**: `wayback-userscript.js`

Extract the archive:

```bash
# macOS/Linux
tar -xzf wayback-server-*.tar.gz

# Windows: extract the .zip file
```

> **Building from source?** See [docs/BUILD.md](docs/BUILD.md) for manual compilation instructions.

### 2. Database Setup

```bash
# PostgreSQL uses your current system username as the default database user
# If your system username is alice, this is equivalent to: createdb -U alice wayback
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

1. Download `wayback-userscript.js` from the [Releases page](https://github.com/icodeface/wayback-archiver/releases)
2. Open Tampermonkey dashboard in your browser
3. Click "Create a new script"
4. Paste the contents of `wayback-userscript.js`
5. Save and enable

> **Chrome users:** Right-click the Tampermonkey icon → Manage extension, then enable the "Allow user scripts" toggle. Firefox does not require this step.

### 5. Start Browsing

That's it. Pages are automatically archived as soon as they load. Open `http://localhost:8080` to browse your archive.

> **Puppeteer Integration:** For automated archiving, see [docs/PUPPETEER.md](docs/PUPPETEER.md).

## Configuration

Environment variables (or `.env` file in the project root):

The server automatically loads `.env` from the working directory if it exists. You can also set environment variables directly.

| Variable | Default | Description |
|---|---|---|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | Database user. PostgreSQL defaults to the current system username, so leaving this unset is usually recommended. |
| `DB_PASSWORD` | *(empty)* | Database password |
| `DB_NAME` | `wayback` | Database name |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode used by the server connection |
| `SERVER_HOST` | `127.0.0.1` | Server bind address (`0.0.0.0` = all interfaces, `127.0.0.1` = localhost only) |
| `SERVER_PORT` | `8080` | HTTP server port |
| `ALLOWED_ORIGINS` | `http://localhost:8080,http://127.0.0.1:8080` | CORS allowed origins (comma-separated). For remote deployment, add your domain: `https://your-domain.com` |
| `DATA_DIR` | `./data` | Storage directory for HTML and resources |
| `LOG_DIR` | `./data/logs` | Log file directory |
| `AUTH_PASSWORD` | *(empty)* | HTTP Basic Auth password (disabled when empty, username: `wayback`). **REQUIRED for remote deployment** |
| `RESOURCE_METADATA_CACHE_MB` | 10% of system memory | Metadata cache budget for resource URL lookups plus HTTP freshness/validator reuse and revalidation. `RESOURCE_CACHE_MB` is still accepted as a legacy alias. |
| `ENABLE_DEBUG_API` | `false` | Enable `/api/debug/*` endpoints (`memstats`, `gc`, `pprof`). Keep disabled unless you are actively debugging. |
| `COMPRESSION_LEVEL` | `-1` | Compression level: 1 (fastest) to 9 (best), -1 (default/balanced). Response compression always enabled, auto-negotiated via Accept-Encoding |

### Compression Settings

**Server-side (responses)**: Always enabled, auto-negotiated
- Clients that send `Accept-Encoding: gzip` get compressed responses
- Clients that don't support gzip get uncompressed responses
- No configuration needed - works automatically

**Client-side (uploads)**: Configurable in `browser/src/config.ts`
- **For local deployment** (default): Keep `ENABLE_COMPRESSION: false`
  - Localhost transfer is already fast
  - No CPU overhead from compression
- **For remote deployment**: Set `ENABLE_COMPRESSION: true`
  - 95%+ reduction for uploads (large HTML snapshots)
  - Rebuild userscript: `cd browser && npm run build`

## Remote Deployment

For deploying to a remote server, see [REMOTE_DEPLOYMENT.md](docs/REMOTE_DEPLOYMENT.md) for detailed instructions.

Quick setup:

```bash
# .env configuration
ALLOWED_ORIGINS=https://your-domain.com
AUTH_PASSWORD=your_secure_password
SERVER_HOST=0.0.0.0

# Browser config.ts
SERVER_URL: 'https://your-domain.com/api/archive'
AUTH_PASSWORD: 'your_secure_password'
ENABLE_COMPRESSION: true  # Enable upload compression for remote deployment
```

**Security Notes:**
- Always use HTTPS for remote deployment
- Set a strong `AUTH_PASSWORD`
- Limit `ALLOWED_ORIGINS` to trusted domains only
- `Origin: null` is intentionally rejected because it also covers sandboxed iframes and data/file-backed opaque origins
- Both CORS and Basic Auth are required for security (defense in depth)

**Performance Notes:**
- Enable `ENABLE_COMPRESSION` in browser config for remote deployment
- Reduces upload bandwidth by 95%+ (especially for large HTML snapshots)
- Response compression is automatic (no configuration needed)
- Minimal CPU overhead, significant network savings

**Capture Notes:**
- `/api/archive` and `/api/archive/:id` reject decompressed JSON bodies larger than 32 MiB with HTTP `413`
- Cross-origin iframe snapshots remain enabled, but the browser bridge now signs requests and returns frame HTML over a private `MessageChannel` instead of public `window.postMessage`
- Resource downloads only forward cookies that still match the target URL under browser rules (`hostOnly`, `domain`, `path`, `secure`, expiry, `SameSite`, partition top-level site)
- The browser script skips local/private targets including `localhost`, `127.0.0.1`, `172.16.0.0/12`, `169.254.0.0/16`, `::1`, `fc00::/7`, `fe80::/10`, and `.local`

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
When `action` is `created`, the response is sent immediately after the page row and raw HTML are stored; resource downloads and HTML rewriting continue in the background.

### PUT /api/archive/:id

Accepts the same body as POST. Returns immediately once the server has accepted the update request. If the content changed, resource re-processing and the final snapshot swap continue in the background; old HTML is queued for delayed deletion after a successful swap. Returns `{ status, page_id, action }` where `action` is `updated` or `unchanged`.
The request body `url` must exactly match the existing page URL for `:id`; otherwise the server rejects the update with HTTP `400` to prevent cross-page snapshot corruption.

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
