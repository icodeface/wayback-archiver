# Building from Source

This guide covers manual compilation of Wayback Archiver. For most users, downloading pre-built binaries from the [Releases page](https://github.com/icodeface/wayback-archiver/releases) is recommended.

## Prerequisites

- **Go** 1.21+
- **Node.js** 16+ (for building the userscript)
- **Make** (optional, for using Makefile targets)

## Build Steps

### 1. Clone the Repository

```bash
git clone https://github.com/icodeface/wayback-archiver.git
cd wayback-archiver
```

### 2. Build Server and Userscript

```bash
make build
```

This compiles:
- Go server binary → `bin/wayback-server`
- Tampermonkey userscript → `bin/wayback-userscript.js`

### 3. Build Server Only

```bash
make server
```

### 4. Build Userscript Only

```bash
make script
```

### 5. Cross-Compile for All Platforms

```bash
make all
```

Outputs binaries for:
- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64, arm64)

All binaries are placed in `bin/wayback-server-<os>-<arch>`.

## Manual Build (without Make)

### Server

```bash
cd server
go build -o ../bin/wayback-server \
  -ldflags "-X main.Version=$(git describe --tags --always) -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  ./cmd/server
```

### Userscript

```bash
cd browser
npm install
node build.js
```

Output: `browser/dist/wayback-userscript.js`

## Testing

```bash
# Go unit tests
make test

# E2E tests (requires server running on localhost:8080)
make test-e2e
```

## Clean Build Artifacts

```bash
make clean
```

This removes the `bin/` directory.
