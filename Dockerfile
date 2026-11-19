# syntax=docker/dockerfile:1

# Build and test environment. The default command runs tests, not the server.
ARG GO_VERSION=1.25
ARG NODE_VERSION=22
FROM node:${NODE_VERSION}-bookworm-slim AS node
FROM golang:${GO_VERSION}-bookworm AS checks

# Both images use Bookworm so Node and the CGO compiler share the same libc.
COPY --from=node /usr/local/ /usr/local/
ENV CGO_ENABLED=1
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY runtime/pi/package.json runtime/pi/package-lock.json ./runtime/pi/
RUN npm --prefix runtime/pi ci --ignore-scripts --no-audit --no-fund

COPY . .
RUN npm --prefix runtime/pi run check \
    && npm --prefix runtime/pi run build \
    && go run ./cmd/build-runtime \
    && bash scripts/check-pi-only.sh \
    && go build ./...

# Dependencies are already cached; tests use local fixtures without API keys.
ENV npm_config_offline=true
CMD ["sh", "-c", "npm --prefix runtime/pi test && node --test web/static/js/*.test.cjs && go test ./..."]
