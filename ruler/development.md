# Development Tools & Workflows

## Development Tools

- **Runtime**: Go 1.25.1
- **Web Framework**: Gorilla Mux
- **Key Dependencies**:
  - `lancekrogers/claude-code-go` - Claude Code Go SDK
  - `github.com/golang-jwt/jwt/v5` - GitHub App JWT authentication
  - `github.com/joho/godotenv` - Environment variable management

## Common Development Tasks

### Build and Run

```bash
# Build the binary
go build -o swe-agent cmd/main.go

# Run directly
go run cmd/main.go

# Run with environment variables loaded
source .env && go run cmd/main.go
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test ./... -cover

# Generate detailed coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# View coverage by function
go tool cover -func=coverage.out

# Run specific package tests
go test ./internal/webhook/...
go test ./internal/provider/...
```

### Code Quality

```bash
# Format code
go fmt ./...

# Lint/vet code
go vet ./...

# Tidy dependencies
go mod tidy
```

### Docker

```bash
# Build Docker image
docker build -t swe-agent .

# Run container
docker run -d -p 8000:8000 \
  -e GITHUB_APP_ID=123456 \
  -e GITHUB_PRIVATE_KEY="$(cat private-key.pem)" \
  -e GITHUB_WEBHOOK_SECRET=secret \
  -e ANTHROPIC_API_KEY=sk-ant-xxx \
  --name swe-agent \
  swe-agent
```

## Testing Standards

- Target: >75% coverage overall
- 100% coverage for security-critical code (webhook verification, auth)
- Test files located alongside implementation: `file.go` → `file_test.go`
- Use table-driven tests for multiple scenarios
