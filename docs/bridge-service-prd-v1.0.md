# PRD：SWE-Agent 桥接服务

- **版本**：1.0
- **状态**：Active
- **Owner**：SWE-Agent Team
- **更新日期**：2025-10-18

---

## 📌 产品定位

### 核心定位

SWE-Agent 是一个 **GitHub ↔ Claude Code/Codex 桥接服务**，而不是一个完整的 AI Agent。

```
用户在 GitHub Issue/PR 评论 `/code xxx`
         ↓
    SWE-Agent（桥接层）
         ↓
   Claude Code / Codex CLI（AI 处理层）
         ↓
    SWE-Agent（仓库操作层）
         ↓
   推送代码 + 生成 PR 链接
```

### 设计哲学

**我们做什么**：
- ✅ 监听 GitHub Webhook
- ✅ 聚合上下文（Issue/PR 历史）传递给 AI CLI
- ✅ 调用 `codex` / `claude` 命令（CLI 直通）
- ✅ 检测文件变更、提交、推送分支
- ✅ 生成 PR compare 链接，人工确认后创建 PR
- ✅ 实时更新 GitHub Comment 进度

**我们不做什么**：
- ❌ 不实现 AI 能力（所有智能逻辑由 Claude Code/Codex 处理）
- ❌ 不管理复杂工作流（没有 `/clarify`、`/prd`、`/code-review` 等多阶段命令）
- ❌ 不做 Prompt 工程（直接传递用户指令和上下文）
- ❌ 不自动创建 PR（生成 compare 链接，用户确认）

**由 Claude Code/Codex 负责**：
- ✅ 需求澄清（AI 会主动提问）
- ✅ 代码质量检查（AI 自己决定是否 lint/test）
- ✅ 多轮对话与上下文理解
- ✅ 代码审查与重构建议
- ✅ 文档更新与 PR 描述生成

---

## 🎯 核心功能

### 1. Webhook 处理层

**功能**：
- 监听 GitHub `issue_comment`、`pull_request_review_comment` 事件
- HMAC SHA-256 签名验证
- 解析触发词（默认 `/code`，可配置）
- 权限控制（默认仅 GitHub App Installer）
- 防抖机制（Bot 评论过滤、12h 去重）

**关键实现**：
- `internal/webhook/handler.go` - Webhook 事件处理
- `internal/webhook/verify.go` - 签名验证
- `internal/auth/permission.go` - 权限检查

### 2. AI 集成层

**功能**：
- 支持多 Provider（Codex / Claude Code CLI）
- 上下文聚合（Issue 标题 + 正文 + 评论历史）
- CLI 参数构建与调用
- Streaming 输出捕获（实时同步到 GitHub Comment）

**关键实现**：
- `internal/provider/codex/codex.go` - Codex CLI 调用
- `internal/provider/claude/claude.go` - Claude Code CLI 调用
- `internal/executor/task.go` - 上下文组装

**Prompt 策略**：
```go
// 极简 Prompt，直接传递用户指令和上下文
func buildPrompt(task *Task) string {
    return fmt.Sprintf(`
Repository: %s
Issue #%d: %s

%s

User Request:
%s

Please implement the requested changes.
`, task.Repo, task.Number, task.IssueTitle, task.IssueContext, task.Prompt)
}
```

### 3. 仓库操作层

**功能**：
- 克隆仓库到临时目录
- 文件变更检测（`git status`）
- 分支管理（创建 / 复用现有 PR 分支）
- 提交并推送到 GitHub
- 生成 PR compare 链接（预填 title 和 `Fixes #<number>`）

**关键实现**：
- `internal/github/clone.go` - 仓库克隆
- `internal/executor/task.go` - 文件变更检测
- `internal/github/pr.go` - PR 链接生成

### 4. 任务队列与可靠性层

**功能**：
- 有界任务队列（内存实现）
- 工作池并发控制（默认 4 workers）
- 指数退避重试（最多 3 次）
- PR/Issue 串行执行（避免冲突）
- 超时保护（10 分钟）

**关键实现**：
- `internal/dispatcher/dispatcher.go` - 任务调度
- `internal/taskstore/store.go` - 任务存储

### 5. 进度追踪与反馈层

**功能**：
- 评论状态管理（Queued → Working → Completed/Failed）
- 实时进度更新（Streaming 输出同步）
- 错误信息反馈（失败时评论错误详情）
- 成本追踪（可选）

**关键实现**：
- `internal/github/comment_tracker.go` - 评论追踪
- `internal/github/comment_state.go` - 状态枚举

---

## 🚫 明确不做的功能

### 1. 多阶段工作流

**不实现**：
- `/clarify` - 需求澄清阶段
- `/prd` - PRD 生成阶段
- `/code-review` - 代码审查阶段

**原因**：
- 这些是 AI 的能力，应由 Claude Code/Codex 自己处理
- 增加了不必要的复杂度
- 用户可以直接在对话中要求 AI 做这些事情

### 2. Prompt 模板管理

**不实现**：
- 复杂的 Prompt 模板系统
- Prompt 优化与调试

**原因**：
- Codex/Claude Code 已经有完善的 Prompt 处理能力
- 直接传递用户指令更灵活

### 3. WorkflowState 状态机

**不实现**：
- Stage 0-5 的复杂状态追踪
- Clarifications、PRD、FixAttempts 等字段

**原因**：
- 简化为单一 Task 模型
- 状态由 Comment 和 Git 历史追溯

### 4. 跨仓库协作（Multi-Repo）

**不实现**（至少 v1.0 之前）：
- 跨仓库任务拆分与执行
- 复杂的依赖顺序控制

**原因**：
- 过于复杂，风险高
- 可以通过多次 `/code` 手动处理

---

## 📅 路线图

### v0.5 - 桥接服务优化（🔄 进行中）

**核心目标**：强化桥接服务的稳定性和用户体验

- [ ] **Streaming 输出同步** - CLI 实时输出同步到 GitHub Comment
- [ ] **改进评论格式** - 更清晰的任务状态展示
- [ ] **成本追踪** - 统计 API 调用成本和配额
- [ ] **限流保护** - 防止滥用（按仓库/小时限额）

**工作量估算**：2-3 周

### v0.6 - 可靠性增强（📅 计划中）

**核心目标**：提升服务的生产可用性

- [ ] **队列持久化** - Redis/数据库实现任务持久性
- [ ] **任务历史** - 追踪执行历史并从断点恢复
- [ ] **Web UI** - 任务监控与配置管理
- [ ] **结构化日志** - JSON 日志 + 日志等级

**工作量估算**：3-4 周

### v1.0 - 企业级特性（🎯 长期目标）

**核心目标**：满足企业场景的治理需求

- [ ] **团队权限管理** - 基于角色的访问控制
- [ ] **成本控制中心** - API 开销预算与告警
- [ ] **审计日志** - 记录所有操作以满足合规
- [ ] **横向扩展** - 多 worker 节点支持
- [ ] **高级限流** - 仓库/组织/用户粒度

**工作量估算**：1-2 个月

---

## ✅ 验收标准

### 核心用户体验

**用户在 GitHub Issue 评论**：
```
/code 实现用户登录功能，支持邮箱和密码登录
```

**SWE-Agent 应该**：
1. ✅ 在 1 分钟内回复评论，显示 `⏳ Working`
2. ✅ 调用 Claude Code/Codex CLI
3. ✅ 实时同步 AI 输出到评论（如果实现了 Streaming）
4. ✅ 检测文件变更，提交到新分支
5. ✅ 在评论中回复：
   - 修改的文件列表
   - PR compare 链接（预填 title 和 `Fixes #123`）
   - 成本信息（可选）
6. ✅ 用户点击链接，确认后手动创建 PR

**用户在 PR 评论中修复**：
```
/code 修复测试失败的问题
```

**SWE-Agent 应该**：
1. ✅ 检测到这是 PR 评论
2. ✅ 在现有 PR 分支上追加提交
3. ✅ 不创建新 PR

### 可靠性与安全

- ✅ 非 Installer 触发时拒绝执行
- ✅ Bot 评论不触发无限循环
- ✅ 同一 Issue/PR 任务串行执行
- ✅ 10 分钟超时保护
- ✅ 失败时在评论中明确提示错误原因

---

## 🔒 安全考量

| 项目 | 状态 | 说明 |
|------|------|------|
| Webhook 签名校验 | ✅ 已实现 | HMAC SHA-256 |
| 恒定时间比较 | ✅ 已实现 | 防止计时攻击 |
| 命令注入防护 | ✅ 已实现 | SafeCommandRunner |
| 超时保护 | ✅ 已实现 | 10 分钟超时 |
| Bot 评论过滤 | ✅ 已实现 | 防止无限循环 |
| 权限控制 | ✅ 已实现 | 仅 Installer 可触发 |
| API Key 管理 | ⚠️ 建议 | 使用环境变量 |
| 队列持久化 | ⚠️ 规划中 | v0.6 目标 |
| 限流 | ⚠️ 规划中 | v0.5 目标 |

---

## 🎯 成功指标

### v0.5 目标

- 用户触发 → PR 链接生成的成功率 > 90%
- 平均响应时间 < 2 分钟
- 服务可用性 > 99%
- 用户手动创建 PR 的比例 > 80%（说明流程顺畅）

### v1.0 目标

- 企业级客户 > 10 家
- 月活跃仓库 > 100 个
- 任务队列持久化，0 数据丢失
- 成本告警准确性 > 95%

---

## 📚 参考资料

- [Claude Code CLI 文档](https://github.com/anthropics/claude-code)
- [Codex CLI 文档](https://github.com/codex-rs/codex)
- [GitHub App 权限配置](https://docs.github.com/en/apps)
- [Webhook 签名验证](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries)

---

## 🔄 更新历史

- **2025-10-18**：v1.0 - 初始版本，明确桥接服务定位
- **归档**：auto-dev-pipeline-prd-v0.1.md（复杂的多阶段工作流方案）
