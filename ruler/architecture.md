# Architecture Overview

## Product Positioning

**SWE-Agent is a bridge service**, not a full AI Agent.

**What we do**:
- ✅ Listen to GitHub webhooks and parse `/code` commands
- ✅ Aggregate context (Issue/PR history) and pass to AI CLI
- ✅ Call `codex` / `claude` CLI directly (no custom prompt engineering)
- ✅ Detect file changes, commit, push to branches
- ✅ Generate PR compare links (user manually creates PR)
- ✅ Real-time progress updates to GitHub comments

**What we DON'T do**:
- ❌ Implement AI capabilities (handled by Claude Code/Codex)
- ❌ Complex multi-stage workflows (`/clarify`, `/prd`, `/code-review`)
- ❌ Custom prompt templates (pass user instructions directly)
- ❌ Auto-create PRs (generate compare link, user confirms)

**Responsibilities delegated to Claude Code/Codex**:
- ✅ Requirement clarification (AI asks questions)
- ✅ Code quality checks (AI decides whether to lint/test)
- ✅ Multi-turn conversation & context understanding
- ✅ Code review & refactoring suggestions
- ✅ Documentation updates & PR descriptions

## Request Flow

```
GitHub Webhook (issue_comment event)
      ↓
  Handler (verify HMAC signature)
      ↓
  Executor (orchestrate task)
      ↓
  Provider (call Codex/Claude CLI)
      ↓
  GitHub Operations (clone, commit, push)
      ↓
  Comment (post PR creation link)
```

## Core Components

### 1. Webhook Handler (`internal/webhook/`)

- **handler.go**: HTTP endpoint for GitHub webhooks, event parsing
- **verify.go**: HMAC SHA-256 signature verification (constant-time comparison)
- **types.go**: GitHub webhook payload types

### 2. Provider System (`internal/provider/`)

- **provider.go**: Interface definition for AI CLI backends
- **factory.go**: Provider factory pattern for instantiation
- **claude/**: Claude Code CLI wrapper
- **codex/**: Codex CLI wrapper

**Key Design**: Provider is a thin wrapper around CLI tools, not a custom AI implementation:

```go
type Provider interface {
    GenerateCode(ctx, req) (*CodeResponse, error)
    Name() string
}
```

**Implementation Strategy**:
- Call external CLI (`codex` or `claude` command)
- Pass user instructions and aggregated context as-is
- No custom prompt templates or engineering
- Capture CLI output and parse file changes
- All AI logic handled by the CLI itself

### 3. Task Executor (`internal/executor/`)

- **task.go**: Orchestrates the full workflow:
  1. Clone repository
  2. Call AI provider
  3. Apply changes to filesystem
  4. Commit and push to new branch
  5. Post comment with PR link

### 4. GitHub Operations (`internal/github/`)

- **auth.go**: GitHub App JWT token generation and installation token exchange
- **clone.go**: Repository cloning via `gh repo clone`
- **comment.go**: Comment posting via `gh issue comment`
- **pr.go**: PR creation URL generation

### 5. Configuration (`internal/config/`)

- **config.go**: Environment variable loading and validation
- Supports multiple providers (Claude, Codex)
- Validates required secrets at startup

## Project Structure

```
swe/
├── cmd/
│   └── main.go                          # HTTP server entry point
├── internal/
│   ├── config/                          # Configuration management
│   ├── webhook/                         # GitHub webhook handling
│   ├── provider/                        # AI provider abstraction
│   │   ├── claude/                      # Claude implementation
│   │   └── codex/                       # Codex implementation
│   ├── executor/                        # Task orchestration
│   └── github/                          # GitHub API operations
├── Dockerfile                           # Container build
├── .env.example                         # Environment template
└── TEST_COVERAGE_REPORT.md              # Detailed test coverage
```

## Important Implementation Notes

### Provider Pattern Design

The provider system eliminates conditional branching through interface polymorphism:

```go
// Adding a new provider requires:
// 1. Implement Provider interface in internal/provider/<name>/
// 2. Add case in factory.go NewProvider() function
// 3. Add config fields in internal/config/config.go
// 4. No changes to executor or handler needed
```

### Authentication Flow

1. **GitHub App JWT**: Signs JWT with private key, includes App ID
2. **Installation Token**: Exchanges JWT for short-lived installation token via GitHub API
3. **Git Operations**: Uses installation token for authenticated git commands

Token generation happens per-request to ensure fresh credentials.

### Webhook Security

- HMAC SHA-256 signature verification using webhook secret
- Constant-time comparison prevents timing attacks (`subtle.ConstantTimeCompare`)
- Signature format: `sha256=<hex-encoded-hmac>`

### Error Handling Strategy

Errors are automatically posted as GitHub comments for user visibility:

```go
if err != nil {
    return e.notifyError(task, errorMsg)
    // User sees detailed error in GitHub comment
    // No need to check logs
}
```

### CLI Tool Dependencies

**Core Philosophy**: Delegate to existing tools instead of reimplementing them.

This project depends on external CLIs for all major operations:

- **`gh` CLI**: All GitHub operations (clone, comment, PR link generation)
- **`codex` CLI**: Codex AI code generation (when using Codex provider)
- **`claude` CLI**: Claude Code AI code generation (when using Claude provider)

**Why CLIs instead of libraries**:
- ✅ **Don't reinvent the wheel** - Claude Code/Codex already have powerful capabilities
- ✅ **Simpler architecture** - Just call commands, no complex AI integration
- ✅ **Easier updates** - Upgrade CLI versions independently
- ✅ **Better separation of concerns** - Bridge service vs AI logic

**Installation Requirements**:
- Ensure CLIs are installed and available in PATH
- For Docker deployments, CLIs are baked into the container image
- See Dockerfile for CLI installation steps

## Multi-Provider Support

Current providers (all via CLI):

- **Codex**: Calls `codex` CLI command (recommended)
- **Claude**: Calls `claude` CLI command via lancekrogers/claude-code-go

Provider selection via environment variable:

```bash
# Option 1: Codex (Recommended)
PROVIDER=codex
CODEX_MODEL=gpt-5-codex
# OPENAI_API_KEY=your-key  # Optional, Codex CLI handles this

# Option 2: Claude
PROVIDER=claude
ANTHROPIC_API_KEY=sk-ant-xxx
CLAUDE_MODEL=claude-sonnet-4-5-20250929
```

**Adding a new provider**:
1. Implement Provider interface in `internal/provider/<name>/`
2. Create CLI wrapper (call external command, parse output)
3. Add case in `factory.go` NewProvider() function
4. Add config fields in `internal/config/config.go`
5. No changes to executor or handler needed
