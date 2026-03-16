.PHONY: build all server script test test-go test-e2e clean docker-build docker-up docker-down docker-logs

BINARY    := wayback-server
SERVER_PKG := ./cmd/server
BIN_DIR   := bin

# Version info (git tag or commit hash)
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d %H:%M:%S UTC')
LDFLAGS   := -X 'main.Version=$(VERSION)' -X 'main.BuildTime=$(BUILD_TIME)'

# Common cross-compile targets
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# Default: build for current platform + script
build: server script

server:
	cd server && go build -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(BINARY) $(SERVER_PKG)

script:
	cd browser && VERSION=$(VERSION) npm run build --silent
	mkdir -p $(BIN_DIR)
	cp browser/dist/wayback.user.js $(BIN_DIR)/wayback.user.js

# Cross-compile all platforms + script
all: script
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		output="$(BIN_DIR)/$(BINARY)-$${os}-$${arch}$${ext}"; \
		echo "Building $$output ..."; \
		cd server && GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o ../$$output $(SERVER_PKG) && cd ..; \
	done

clean:
	rm -rf $(BIN_DIR)

# Run Go unit tests
test:
	cd server && go test ./... -v

# Run Puppeteer E2E tests (requires server running on localhost:8080)
test-e2e:
	@echo "Running E2E tests (ensure server is running on localhost:8080)..."
	@for test in tests/server/test_*.js; do \
		echo "Running $$test ..."; \
		node $$test || exit 1; \
	done

# Docker targets
docker-build:
	docker build -t wayback-archiver:latest \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME="$(BUILD_TIME)" \
		.

docker-up:
	VERSION=$(VERSION) BUILD_TIME="$(BUILD_TIME)" docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f wayback
