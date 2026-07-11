# Contributing

Thanks for your interest in Daily Info Agent! This document covers how to contribute to the project.

## How to Contribute

### 1. Reporting Issues

- Check existing issues to avoid duplicates
- Include the version, environment, and steps to reproduce
- Attach relevant logs (sanitized of secrets)
- Label the issue with the appropriate category (bug, feature, docs)

### 2. Feature Requests

- Describe the use case and why it's valuable
- If possible, suggest an approach or reference an existing implementation

### 3. Code Contributions

#### Prerequisites

- Go 1.25+
- Node.js 20+ (for frontend work)
- PostgreSQL 16 (optional, for database-dependent features)

#### Getting Started

```bash
# Fork and clone the repo
git clone https://github.com/your-username/daily-info-agent.git
cd daily-info-agent

# Set up environment
cp .env.example .env
# Edit .env with your API keys

# Install dependencies
go mod tidy
cd web && npm ci && cd ..

# Run tests
go test ./...

# Start development
make dev
```

#### Development Workflow

1. **Create a branch** from `main`:
   ```bash
   git checkout -b fix/your-fix-name
   ```

2. **Make changes** following these conventions:
   - **Go**: Follow [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
   - **TypeScript/React**: Follow the existing component patterns
   - **Error handling**: Use typed errors, wrap with `fmt.Errorf("context: %w", err)`
   - **Logging**: Use structured `slog` with component field
   - **Comments**: Chinese comments in existing code are preserved; new code uses English

3. **Run checks** before committing:
   ```bash
   make test      # Go unit tests with race detector
   go vet ./...   # Static analysis
   make web-lint  # TypeScript type checks
   make build     # Verify compilation
   ```

4. **Write tests** for new code:
   - Unit tests go in the same package as `_test.go`
   - Integration tests go in `test/integration/` with `//go:build integration`
   - Aim for meaningful coverage, not 100%

5. **Commit** with a clear message:
   ```
   feat: add support for X
   fix: correct Y behavior
   docs: update API reference
   chore: bump dependency
   ```

6. **Push and open a PR** against `main`.

#### Code Review

- All PRs need at least one review
- Address feedback with additional commits (no force-pushing until final)
- Keep PRs focused on a single concern

## Project Structure

```
daily-info-agent/
├── cmd/agent/            # Entry point
├── internal/
│   ├── agent/            # LLM agent (session, streaming, tool calling)
│   ├── api/              # REST management API handlers
│   ├── chat/             # Chat HTTP handler (auth, rate-limit, streaming)
│   ├── dedup/            # Near-duplicate title detection (Jaccard + Union-Find)
│   ├── fetcher/          # Data source adapters (RSS, NewsAPI, RSSHub, search)
│   ├── notifier/         # SMTP email digest
│   ├── processor/        # AI batch processing via LLM
│   ├── publisher/        # Java website API client
│   ├── scheduler/        # Pipeline orchestration
│   ├── store/            # PostgreSQL storage
│   └── verifier/         # Source credibility verification
├── pkg/
│   ├── backoff/          # Exponential backoff retry
│   ├── config/           # Environment variable loading
│   └── models/           # Shared data types
├── web/                  # React frontend
├── migrations/           # SQL migrations
├── test/                 # Integration tests
└── docs/                 # Documentation
```

## Code Style

- **Go**: `gofmt` + `go vet` must pass. Use `slog` for structured logging.
- **TypeScript**: Use strict types; avoid `any`. Prefer functional components with hooks.
- **Error messages**: Use English in code, Chinese for user-facing content.
- **Secrets**: Never commit `.env` or any file containing real API keys.

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.
