# ── Stage 1: Build the React frontend ────────────────────────────────────
FROM node:20-alpine AS frontend

WORKDIR /web

# Cache npm dependencies in a separate layer.
COPY web/package.json web/package-lock.json ./
RUN npm ci

# Build the production bundle into web/dist.
COPY web/ .
RUN npm run build

# ── Stage 2: Build the Go binary ─────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a statically-linked binary for the production image.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-X main.version=$(git describe --tags --always 2>/dev/null || echo dev) -s -w" \
    -o /agent \
    ./cmd/agent

# ── Stage 3: Minimal production image ────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user.
RUN addgroup -S agent && adduser -S -G agent agent

WORKDIR /app

COPY --from=builder /agent /app/agent
COPY --from=builder /src/migrations/ /app/migrations/
COPY --from=builder /src/cache/ /app/cache/

# Embed the compiled React frontend (served by the agent at web/dist).
COPY --from=frontend /web/dist /app/web/dist

USER agent

EXPOSE 8080

ENTRYPOINT ["/app/agent"]
CMD ["--mode=server"]
