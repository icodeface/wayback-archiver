# Stage 1: Build userscript
FROM node:20-alpine AS script-builder

WORKDIR /build
COPY browser/package*.json ./browser/
RUN cd browser && npm ci --silent

COPY browser/ ./browser/
ARG VERSION=docker
RUN cd browser && VERSION=${VERSION} npm run build --silent

# Stage 2: Build Go server
FROM golang:1.24-alpine AS server-builder

WORKDIR /build
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

COPY server/ ./server/
ARG VERSION=docker
ARG BUILD_TIME
RUN cd server && \
    CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}'" \
    -o wayback-server ./cmd/server

# Stage 3: Runtime
FROM alpine:3.19

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binaries
COPY --from=server-builder /build/server/wayback-server /app/
RUN mkdir -p /app/bin
COPY --from=script-builder /build/browser/dist/wayback.user.js /app/bin/

# Copy database initialization files
COPY server/init_db.sql /app/
COPY server/migrations/ /app/migrations/

# Create data directory
RUN mkdir -p /app/data/logs

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/version || exit 1

# Run as non-root user
RUN addgroup -g 1000 wayback && \
    adduser -D -u 1000 -G wayback wayback && \
    chown -R wayback:wayback /app
USER wayback

CMD ["/app/wayback-server"]
