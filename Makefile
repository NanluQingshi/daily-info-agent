.PHONY: build test lint run-schedule run-server dev clean tidy web-install web-build web-dev build-full db-create

# Build flags — override version at build time
VERSION ?= 1.0.0
LDFLAGS := -ldflags="-X main.version=$(VERSION)"
BINARY  := agent

## build: Compile the agent binary
build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/agent

## test: Run all tests with race detector
test:
	go test -race -cover ./...

## lint: Run go vet and staticcheck (install staticcheck first if missing)
lint:
	go vet ./...

## tidy: Update go.sum and tidy dependencies
tidy:
	go mod tidy

## run-schedule: Build and run in scheduled (one-shot) mode
run-schedule: build
	./$(BINARY) --mode=schedule

## run-server: Build and run in HTTP server mode
run-server: build
	./$(BINARY) --mode=server

## web-install: Install frontend npm dependencies
web-install:
	cd web && npm install

## web-build: Build the React frontend for production
web-build:
	cd web && npm run build

## web-dev: Start Vite dev server (proxies /api to localhost:8080)
web-dev:
	cd web && npm run dev

## dev: Start both Go backend (8080) and Vite dev server (5173) together
dev: build
	@echo "==> backend  → http://localhost:8080"
	@echo "==> frontend → http://localhost:5173"
	@./$(BINARY) --mode=server & \
	  BACKEND_PID=$$!; \
	  cd web && npm run dev; \
	  kill $$BACKEND_PID 2>/dev/null || true

## build-full: Build React frontend then compile Go binary (embeds web/dist)
build-full: web-build build

## db-create: Create the local PostgreSQL database
db-create:
	createdb daily_info

## clean: Remove build artifacts and cache
clean:
	rm -f $(BINARY)
	rm -f cache/dedup.json
