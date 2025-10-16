# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

## Architecture Overview

Pilot SWE is a GitHub App webhook service that responds to `/code` commands in issue comments to automatically generate and commit code changes.

### Request Flow

```
GitHub Webhook (issue_comment event)
      ↓
  Handler (verify HMAC signature)
      ↓
  Executor (orchestrate task)
      ↓
  Provider (AI code generation)
      ↓
  GitHub Operations (clone, commit, push)
      ↓
  Comment (post PR creation link)
```

### Core Components

#### 1. Webhook Handler (`internal/webhook/`)

- **handler.go**: HTTP endpoint for GitHub webhooks, event parsing
- **verify.go**: HMAC SHA-256 signature verification (constant-time comparison)
- **types.go**: GitHub webhook payload types

#### 2. Provider System (`internal/provider/`)

- **provider.go**: Interface definition for AI backends
- **factory.go**: Provider factory pattern for instantiation
- **claude/**: Claude Code implementation
- **codex/**: Codex implementation (multi-provider support)

Provider interface enables zero-branch polymorphism:

```go
type Provider interface {
    GenerateCode(ctx, req) (*CodeResponse, error)
    Name() string
}
```

#### 3. Task Executor (`internal/executor/`)

- **task.go**: Orchestrates the full workflow:
  1. Clone repository
  2. Call AI provider
  3. Apply changes to filesystem
  4. Commit and push to new branch
  5. Post comment with PR link

#### 4. GitHub Operations (`internal/github/`)

- **auth.go**: GitHub App JWT token generation and installation token exchange
- **clone.go**: Repository cloning via `gh repo clone`
- **comment.go**: Comment posting via `gh issue comment`
- **pr.go**: PR creation URL generation

#### 5. Configuration (`internal/config/`)

- **config.go**: Environment variable loading and validation
- Supports multiple providers (Claude, Codex)
- Validates required secrets at startup

### Project Structure

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

This project delegates Git operations to CLI tools rather than reimplementing them:

- **`gh` CLI**: All GitHub operations (clone, comment, PR)
- **`claude` CLI**: AI code generation via lancekrogers/claude-code-go

Ensure both CLIs are installed and available in PATH.

## Code Conventions

### Design Philosophy (Linus-Style)

1. **Good Taste - Eliminate Special Cases**

   - Use interfaces over if/else chains
   - Design data structures to make edge cases disappear
   - Prefer polymorphism to conditionals

2. **Shallow Indentation**

   - Functions should not exceed 3 levels of indentation
   - Early returns over nested conditionals
   - Extract complex logic into helper functions

3. **Clear Naming**

   - Use domain-specific names: `Provider`, `Executor`, `Handler`
   - Avoid generic names: `Manager`, `Service`, `Helper`
   - Package names match their primary type

4. **Error Visibility**

   - Don't hide errors in logs
   - Surface errors to users (GitHub comments)
   - Include context in error messages

5. **Backward Compatibility**
   - Provider interface designed for future extension
   - Config fields have sensible defaults
   - No breaking API changes without major version bump

### Testing Standards

- Target: >75% coverage overall
- 100% coverage for security-critical code (webhook verification, auth)
- Test files located alongside implementation: `file.go` → `file_test.go`
- Use table-driven tests for multiple scenarios

## Multi-Provider Support

Current providers:

- **Claude**: Via `lancekrogers/claude-code-go` SDK
- **Codex**: Via Codex provider implementation

Provider selection via environment variable:

```bash
PROVIDER=claude  # or "codex"
CLAUDE_API_KEY=sk-ant-xxx
CLAUDE_MODEL=claude-3-5-sonnet-20241022
```


### Git 与 Issue 强制规则

- 🔗 **Issue ID 必需**：提交前必须有 Issue ID；若无则询问用户创建或指定
- 🚨 **Issue 分支强制**：修改代码前必须检查当前分支
  - ✅ 允许：在 `feat/<issue-id>-*`、`fix/<issue-id>-*`、`refactor/<issue-id>-*` 等 issue 分支上修改
  - ❌ 禁止：在 `main`、`master` 等主分支上修改代码
  - 📋 处理流程：
    1. 检测到需要修改代码时，先执行 `git branch --show-current` 检查当前分支
    2. 如果在主分支，询问用户提供 Issue ID 或使用 `/git-create-issue` 创建
    3. 获取 Issue ID 后，创建对应分支：`git checkout -b <type>/<issue-id>-<description>`
    4. 切换到 issue 分支后再执行代码修改
    5. 如果用户拒绝创建分支，则拒绝修改代码并说明原因
- 📝 **Heredoc 格式**：Git 提交与 GitHub CLI 必须使用 heredoc（见 7.2）
- 🚫 **禁止 `\n` 换行**：在命令参数中写 `\n` 只会产生字面量，不会换行
- 📌 **推送后评论**：推送后必须在对应 Issue 评论报告修改并关联 commit hash
- 🔑 **统一 SSH 认证**：Git 远程和 GitHub CLI 操作统一使用 SSH key 认证


## Git 与 GitHub 规范

### 1 提交格式

- 使用 Conventional Commits：`feat:`/`fix:`/`docs:`/`refactor:` 等
- 末尾添加：`Refs: #123` 或 `Closes: #123`

### 2 Heredoc 使用（强制）

**Git 提交**

```bash
git commit -F - <<'MSG'
feat: 功能摘要

变更说明：
- 具体变更点1
- 具体变更点2

Refs: #123
MSG
```

**GitHub CLI - PR 创建**

```bash
gh pr create --body-file - <<'MSG'
## 变更说明
- 具体变更点1
- 具体变更点2

close: #123
MSG
```

**GitHub CLI - Issue 评论**

```bash
gh issue comment 123 --body-file - <<'MSG'
问题分析：
- 原因1
- 原因2
MSG
```

**GitHub CLI - PR Review**

```bash
gh pr review 123 --comment --body-file - <<'MSG'
代码审查意见：
- 建议1
- 建议2
MSG
```

#### 0. Linus 三问（决策前必答）

1. "这是真实问题还是想象的？" → 拒绝过度设计
2. "有更简单的方法吗？" → 永远追求最简解法
3. "这会破坏什么？" → 兼容性是铁律

#### 1. 需求理解确认

> 基于当前信息，我的理解是：[用 Linus 思维重述需求]
> 请确认我的理解是否准确。

#### 2. Linus 式问题拆解

**第一层：数据结构分析**
"Bad programmers worry about the code. Good programmers worry about data structures."

- 核心数据实体是什么？如何关联？
- 数据流向哪里？谁拥有？谁修改？
- 有无不必要的数据拷贝或转换？

**第二层：特殊分支识别**
"Good code has no special cases."

- 找出所有 if/else 分支
- 哪些是真正的业务逻辑？哪些是糟糕设计的补丁？
- 能否重新设计数据结构来消除这些分支？

**第三层：复杂度审查**
"If the implementation needs more than three levels of indentation, redesign it."

- 这个功能的本质是什么？（一句话说明）
- 当前方案涉及多少概念？
- 能否削减一半？再削减一半？

**第四层：破坏性分析**
"Never break userspace" — 兼容性是铁律

- 列出所有可能受影响的现有功能
- 哪些依赖会被破坏？
- 如何在不破坏任何东西的前提下改进？

**第五层：实用性验证**
"Theory and practice sometimes clash. Theory loses. Every single time."

- 这个问题在生产环境真实存在吗？
- 有多少用户真正遇到它？
- 解决方案的复杂度与问题严重程度是否匹配？

#### 3. 决策输出模式

**[核心判断]**
值得做：[原因] / 不值得做：[原因]

**[关键洞察]**

- 数据结构：[最关键的数据关系]
- 复杂度：[可消除的复杂度]
- 风险点：[最大破坏风险]

**[Linus 式计划]**
若值得做：

1. 第一步总是简化数据结构
2. 消除所有特殊分支
3. 用最笨但最清晰的方式实现
4. 确保零破坏

若不值得做：
"这在解决一个不存在的问题。真正的问题是 [XXX]。"

#### 4. 代码评审输出

**[Taste Score]**
Good taste / So-so / Garbage

**[Fatal Issues]**

- [如有，直接指出最糟糕的部分]

**[Directions for Improvement]**
"消除这个特殊分支"
"这 10 行可以变成 3 行"
"数据结构错了；应该是 …"

### 8.4 工具支持

- `resolve-library-id` — 解析库名称到 Context7 ID
- `get-library-docs` — 获取最新官方文档

---

