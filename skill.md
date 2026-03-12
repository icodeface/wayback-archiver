# Wayback Archiver - AI Agent Skill

## Overview

Wayback Archiver is a self-hosted personal web archiving system that captures and preserves web pages with full fidelity — HTML, CSS, JavaScript, images, fonts, and all resources. This skill enables AI agents to autonomously install, configure, and query the Wayback Archiver read-only API.

**Purpose**: Archive web pages you browse in Chrome and replay them offline when the original goes down.

**Repository**: https://github.com/icodeface/wayback-archiver

## Prerequisites

Before installation, ensure these are available:

- **Git**
- **Go** 1.21+
- **Node.js** 16+
- **PostgreSQL** 14+
- **Chrome** with [Tampermonkey](https://www.tampermonkey.net/) extension

## Installation

### 1. Clone the Repository

```bash
git clone https://github.com/icodeface/wayback-archiver.git
cd wayback-archiver
```

### 2. Database Setup

```bash
# Create database
createdb -U postgres wayback

# Initialize schema
psql -U postgres wayback < server/init_db.sql
```

### 3. Server Configuration

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

# Authentication (optional, disabled when empty)
AUTH_PASSWORD=
```

### 4. Start Server

```bash
cd server
go build -o wayback-server ./cmd/server
./wayback-server
```

Server starts at `http://localhost:8080` by default.

**Proxy Support** (if needed for downloading external resources):

```bash
export http_proxy=http://127.0.0.1:7897
export https_proxy=http://127.0.0.1:7897
./wayback-server
```

### 5. Install Userscript

```bash
cd browser
npm install
npm run build
```

Then install `browser/dist/wayback.user.js` in Tampermonkey.

## API Reference

Base URL: `http://localhost:8080`

### Authentication

HTTP Basic Auth (optional, controlled by `AUTH_PASSWORD` env var):
- **Username**: `wayback`
- **Password**: value of `AUTH_PASSWORD`
- **Header**: `Authorization: Basic <base64(wayback:password)>`

When `AUTH_PASSWORD` is empty, authentication is disabled.

### Endpoints

#### 1. List Pages

**GET** `/api/pages`

List all archived pages with pagination and date filtering.

**Query Parameters**:
- `limit` (optional): Items per page (default: 100, max: 1000)
- `offset` (optional): Pagination offset (default: 0)
- `from` (optional): Start date filter (format: `2006-01-02`)
- `to` (optional): End date filter (format: `2006-01-02`)

**Response**:
```json
{
  "pages": [
    {
      "id": 123,
      "url": "https://example.com",
      "title": "Example Page",
      "captured_at": "2026-03-12T10:30:00Z",
      "html_path": "data/html/2026/03/12/1710241800_abc123.html",
      "content_hash": "sha256_hash",
      "first_visited": "2026-03-12T10:30:00Z",
      "last_visited": "2026-03-12T10:30:00Z",
      "created_at": "2026-03-12T10:30:00Z"
    }
  ],
  "total": 1,
  "limit": 100,
  "offset": 0
}
```

#### 4. Get Page Details

**GET** `/api/pages/:id`

Retrieve details of a specific archived page.

**Response**:
```json
{
  "id": 123,
  "url": "https://example.com",
  "title": "Example Page",
  "captured_at": "2026-03-12T10:30:00Z",
  "html_path": "data/html/2026/03/12/1710241800_abc123.html",
  "content_hash": "sha256_hash",
  "first_visited": "2026-03-12T10:30:00Z",
  "last_visited": "2026-03-12T10:30:00Z",
  "created_at": "2026-03-12T10:30:00Z"
}
```

#### 5. Search Pages

**GET** `/api/search`

Full-text search across page URLs, titles, and content.

**Query Parameters**:
- `q` (required): Search keyword
- `from` (optional): Start date filter (format: `2006-01-02`)
- `to` (optional): End date filter (format: `2006-01-02`)

**Response**:
```json
[
  {
    "id": 123,
    "url": "https://example.com",
    "title": "Example Page",
    "captured_at": "2026-03-12T10:30:00Z",
    "html_path": "data/html/2026/03/12/1710241800_abc123.html",
    "content_hash": "sha256_hash",
    "first_visited": "2026-03-12T10:30:00Z",
    "last_visited": "2026-03-12T10:30:00Z",
    "created_at": "2026-03-12T10:30:00Z"
  }
]
```

#### 6. Get Page Timeline

**GET** `/api/pages/timeline`

Retrieve all snapshots of a specific URL (version history).

**Query Parameters**:
- `url` (required): The URL to query

**Response**:
```json
{
  "url": "https://example.com",
  "snapshots": [
    {
      "id": 123,
      "url": "https://example.com",
      "title": "Example Page",
      "captured_at": "2026-03-12T10:30:00Z",
      "html_path": "data/html/2026/03/12/1710241800_abc123.html",
      "content_hash": "sha256_hash",
      "first_visited": "2026-03-12T10:30:00Z",
      "last_visited": "2026-03-12T10:30:00Z",
      "created_at": "2026-03-12T10:30:00Z"
    }
  ],
  "total": 1
}
```

#### 5. View Archived Page

**GET** `/view/:id`

Replay an archived page in the browser (HTML response).

#### 6. Timeline UI

**GET** `/timeline?url=<encoded_url>`

Visual timeline page showing all snapshots of a URL.

#### 7. Robots.txt

**GET** `/robots.txt`

Returns robots.txt that blocks major search engines and AI crawlers while allowing other user agents.

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

## Common Use Cases

### 1. Search Archived Pages

```bash
curl "http://localhost:8080/api/search?q=example"
```

### 2. Get All Snapshots of a URL

```bash
curl "http://localhost:8080/api/pages/timeline?url=https://example.com"
```

### 3. List Recent Archives

```bash
curl "http://localhost:8080/api/pages?limit=10&offset=0"
```

### 4. Filter by Date Range

```bash
curl "http://localhost:8080/api/pages?from=2026-03-01&to=2026-03-12"
```

### 5. Get Page Details

```bash
curl "http://localhost:8080/api/pages/123"
```

### 6. With Authentication

```bash
curl -u wayback:your_password "http://localhost:8080/api/pages?limit=10"
```

## Limitations

- Cross-origin resources may fail due to server-side 403/404
- Dynamically injected scripts (loaded via JS at runtime) may not be captured
- Tracking pixels and analytics URLs with dynamic parameters not preserved
- Large media files consume significant storage

## License

MIT
