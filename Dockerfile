# ── Stage 1: Build the Go binary ──────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a statically-linked binary for the production image.
# The React frontend is built separately and embedded in web/dist.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-X main.version=$(git describe --tags --always 2>/dev/null || echo dev) -s -w" \
    -o /agent \
    ./cmd/agent

# ── Stage 2: Minimal production image ────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user.
RUN addgroup -S agent && adduser -S -G agent agent

WORKDIR /app

COPY --from=builder /agent /app/agent
COPY --chown=agent:agent migrations/ /app/migrations/
COPY --chown=agent:agent cache/ /app/cache/

# The React frontend static build can be mounted or baked in.
# Uncomment the next line when web/dist is built during CI:
# COPY --from=builder /src/web/dist /app/web/dist

USER agent

EXPOSE 8080

ENTRYPOINT ["/app/agent"]
CMD ["--mode=server"]
