# gb28181-simulator Dockerfile
#
# Change: fix-problems-and-smoke-deploy-docs, task 6.1.
# Multi-stage build:
#   Stage 1 (builder-node) — npm ci + vite build, produces web/dist for embedding
#   Stage 2 (builder-go)   — go build, embeds web/dist via //go:embed
#   Stage 3 (runtime)      — alpine, runs the single static binary
#
# Build:
#   docker build -t gb28181-simulator:dev .
# Run (single node):
#   docker run --rm -p 8080:8080 -p 5060:5060/udp gb28181-simulator:dev

# ───────────────────────── Stage 1: frontend builder ─────────────────────────
FROM node:20-alpine AS builder-node
WORKDIR /app/web

# Copy only package manifests first to maximize layer caching.
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund

# Now copy the rest of the frontend source.
COPY web/ ./
RUN npm run build

# ───────────────────────── Stage 2: Go builder ──────────────────────────────
FROM golang:1.25-alpine AS builder-go
WORKDIR /app

# Install CA certs so `go mod download` works behind corporate proxies.
RUN apk add --no-cache ca-certificates

# Cache go module layer.
COPY go.mod go.sum ./
RUN go mod download

# Copy source (respecting .dockerignore which excludes build/ and web/node_modules).
COPY . .

# Bring in the built frontend from stage 1 — the //go:embed directive reads
# from internal/interface/webui/embed/dist at compile time.
COPY --from=builder-node /app/web/dist ./internal/interface/webui/embed/dist

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /out/gb28181-simulator \
    ./cmd/gb28181-simulator

# ───────────────────────── Stage 3: runtime ─────────────────────────────────
FROM alpine:3.20

# CA certs for outbound TLS (RTSP pull, HTTPS media sources); tzdata for
# Asia/Shanghai log timestamps; tini for correct signal forwarding.
RUN apk add --no-cache ca-certificates tzdata tini

# Run as a non-root user. UID/GID 10001 is arbitrary but stable.
RUN addgroup -g 10001 -S gbsim && \
    adduser -u 10001 -S -G gbsim -h /var/lib/gb28181-simulator gbsim

# Standard paths used by the binary (see internal/platform/config defaults).
RUN mkdir -p /var/lib/gb28181-simulator /etc/gb28181-simulator && \
    chown -R gbsim:gbsim /var/lib/gb28181-simulator /etc/gb28181-simulator

COPY --from=builder-go /out/gb28181-simulator /usr/local/bin/gb28181-simulator

USER gbsim
WORKDIR /var/lib/gb28181-simulator

# SIP over UDP/TCP, HTTP management API, optional media RTP/RTCP port range
# is configured via config file and bound at runtime.
EXPOSE 5060/udp 5060/tcp 8080/tcp

# Health check matches the /healthz endpoint asserted by internal/test/e2e.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["gb28181-simulator", "--config", "/etc/gb28181-simulator/config.yaml"]