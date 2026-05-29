.PHONY: build test lint run-schedule run-server clean tidy

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

## clean: Remove build artifacts and cache
clean:
	rm -f $(BINARY)
	rm -f cache/dedup.json
