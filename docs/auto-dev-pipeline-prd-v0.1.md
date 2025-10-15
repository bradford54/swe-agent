# PRD：闭环 Agent 工作流 —— 从需求澄清到代码交付

- 版本：1.0
- Owner：swe-agent（本项目）
- 状态：Release 1.0 功能范围（需实现）
- 更新日期：2025-10-13

## 背景

### 核心痛点
- 维护者在 Issue 中提出需求后，人工沟通、拆分、实现、评审的**往返成本高，且质量不稳定**。
- AI 直接执行代码可能因**缺少仓库上下文**导致实现偏离预期，需要多轮返工。
- 自动创建 PR 可能产生**垃圾 PR**，人工决策点缺失。

### 现有能力
- 本项目已有 Webhook、任务队列、执行器、评论追踪等能力。
- 已支持 `issue_comment` 和 `pull_request_review_comment` 事件触发。
- 已支持推送到现有 PR 分支（避免重复 PR）。

### 参考案例
- https://github.com/goplus/xgo/issues/2449（需求澄清案例）
- https://github.com/goplus/xgo/issues/2461
- https://github.com/goplus/xgo/pull/2471（代码实现案例）
- https://github.com/goplus/xgo/pull/2460
- https://github.com/goplus/xgo/pull/2456

## 目标

### 核心目标：打造**人类参与的闭环 Agent**
- **Stage 0：需求澄清**（可选）→ AI 分析仓库，生成澄清问题，用户回答确认。
- **Stage 1：PRD 生成**（可选）→ 基于澄清结果生成结构化 PRD，用户确认后进入开发。
- **Stage 2：代码开发** → 按 PRD 或已有上下文实施，推送到分支，**不自动创建 PR**。
- **Stage 3：Code Review**（可选）→ 按需触发代码审查，节省成本。
- **Stage 4：修复迭代**（可选）→ PR 审查意见触发修复，在现有分支更新。

> 阶段顺序为推荐路径而非硬性约束；触发命令可任意组合，Agent 会按可用上下文自动兜底。

### 扩展目标：同组织/同用户下的跨仓库协作（Multi-Repo）（非 R1；Post-1.0 规划）
- 从单一仓库 Issue 出发，提议并执行跨仓库子任务（Subtasks），在目标仓库创建分支并给出 compare 链接（仍由人手动创建 PR）。
- 子任务按依赖顺序执行，所有进度在源 Issue 的跟踪评论中汇总展示（同一个“指挥面板”）。
- 仅在同一安装作用域（同组织/同用户，且安装了本 GitHub App）的仓库上执行，严格受白名单/黑名单约束。

### 设计原则
1. **人类决策点**：每个阶段都需要人工触发，避免失控。
2. **完整上下文**：每次触发都获取 Issue 全部评论历史。
3. **成本可控**：Code Review 可选，修复次数有上限。
4. **质量优先**：不自动创建 PR，人工审核分支内容后再创建。
5. **灵活编排**：所有阶段可独立触发，系统不得强制依赖前置阶段。

## 非目标
- 不替代人类最终审核与合并权限。
- 不对高风险操作（大规模重构/未知脚本执行）做自动化决策。
- 不承诺跨语言/跨生态的一键构建与复杂测试矩阵适配（后续增量支持）。
- （R1）不包含跨仓库 Multi-Repo 执行能力（仅作为后续目标，默认关闭）

## 触发与角色

### 触发者权限
- 默认（当前实现）：仅 GitHub App 安装者（Installer）可触发所有阶段（安全基线）
- 可选（配置白名单）：允许指定用户触发（推荐包含仓库 Owner）
- 未来（P2）：按角色细分（Issue 作者/Assignee、PR Reviewer 等）

### 分阶段触发词（核心创新）

触发命令互相独立，可根据需求自由组合；若跳过前置阶段，Agent 会基于现有信息尝试补全缺失上下文。命令集合统一为 `/clarify`、`/prd`、`/code`、`/code-review`。

| 触发词 | 阶段 | 触发事件 | 说明 |
|--------|------|----------|------|
| `/clarify` | Stage 0 | `issue_comment` | 需求澄清：生成澄清问题列表（可跳过） |
| `/prd` | Stage 1 | `issue_comment` | 生成 PRD：基于澄清结果或现有上下文生成结构化 PRD |
| `/code` | Stage 2 & Stage 4 | `issue_comment` / `pull_request_review_comment` / `pull_request` comment | Issue 上触发代码开发；PR 上触发修复迭代（在现有分支追加提交） |
| `/code-review` | Stage 3 | `issue_comment` 或 `pull_request` comment | 代码审查（可选）：分析变更文件，生成 Review 意见 |

> `/code` 触发时需先判定上下文：`issue_comment` 事件且 `issue.pull_request == nil` → Stage 2；`issue_comment` 事件且 `issue.pull_request != nil` → Stage 4；`pull_request_review_comment` 事件 → Stage 4。

**智能触发模式**（可选，未来支持）：
- 在开启智能模式后，系统可基于当前阶段推荐下一步（如首次 `/code` 时提示完成澄清），但默认不自动串联命令。

### 事件监听（Webhook）

| 事件类型 | 触发条件 | 行为 |
|----------|----------|------|
| `issue_comment` | 评论正文包含触发词 `/clarify`、`/prd`、`/code`、`/code-review` | 根据触发词执行对应阶段；若 `payload.issue.pull_request != nil`，视为 PR 会话区评论 |
| `pull_request_review_comment` | 评论正文包含 `/code` 或 `/code-review` | 在 Review 行内评论中触发修复或 Review；复用 Stage 4 / Stage 3 流程 |
| `pull_request_review` | `state = changes_requested` 且评论包含触发词 | 读取审查意见，触发修复（**不自动触发**，需要明确指令） |
| `pull_request` | `action = closed` 且 `merged = true` | 标记工作流完成（Done），记录指标并清理状态 |
| ~~`issues.opened`~~ | ❌ **不支持自动触发** | 需要手动 `/clarify` 启动流程 |

> PR 会话区评论与 Issue 共用 `issue_comment` 事件，通过 `payload.issue.pull_request` 判定是否来自 PR。

注：跨仓库执行不新增事件类型，所有指挥与状态回报仍集中在源 Issue 的评论线程中（R1 默认关闭）。

**防抖机制**：
- 忽略 Bot 发出的评论（`sender.type == "Bot"`）
- 同一评论 ID 12 小时内去重（已实现：`commentDeduper`）
- 按 `repo#number` 串行执行，避免并发冲突

## 端到端流程：5 阶段闭环

### **Stage 0：需求澄清（/clarify，可选）**

**触发**：用户在 Issue 中回复 `/clarify`（如需求已明确，可跳过）

**Agent 行为**：
1. 读取 Issue 全文（标题 + 正文 + 所有评论）
2. 读取仓库上下文：
   - README.md（项目简介）
   - 主要目录结构（`ls -R | head -100`）
   - 关键配置文件（package.json, go.mod, Cargo.toml 等）
3. 调用 Provider 生成澄清问题列表（5-10 个问题）
4. 在 Issue 评论中发布问题列表（Markdown Checklist 格式）

**Prompt 模板**：
```markdown
你是一个需求分析专家。用户提出了以下需求：

【Issue 标题】{issue.title}
【Issue 正文】{issue.body}
【仓库信息】
- 项目语言：{language}
- 主要目录：{directory_structure}
- README 摘要：{readme_summary}

请提出 5-10 个澄清问题，确保需求清晰。问题类型：
- 功能边界：这个功能是否包括 XXX？
- 技术约束：是否需要兼容 XXX 版本？
- 验收标准：如何判断功能完成？
- 依赖关系：是否依赖其他未完成的功能？

输出格式（Markdown Checklist）：
- [ ] **问题 1**：XXX
- [ ] **问题 2**：XXX
```

**用户响应**：
- 用户在问题下方回复答案，或直接编辑 Checklist
- 按需回复 `/prd` 生成 PRD，或直接 `/code` 进入开发阶段

**状态存储**：
```go
// taskstore.WorkflowState
Stage: "clarify"
Clarifications: []Clarification{
    {Question: "问题 1", Answer: "答案 1", Resolved: true},
    {Question: "问题 2", Answer: "", Resolved: false},
}
```

---

### **Stage 1：PRD 生成（/prd，可选）**

**触发**：用户回复 `/prd`（任意时刻可触发）；如存在澄清答案将自动引用，否则基于现有上下文生成。

**Agent 行为**：
1. 读取 Issue 全文 + 澄清问答历史
2. 调用 Provider 生成结构化 PRD（如识别到跨仓库场景，同时给出跨仓库任务拆分草案）
3. 在 Issue 评论中发布 PRD（**不落盘文件**，初期）

**PRD 格式（简化版）**：
```markdown
## PRD：{issue.title}

### 背景
{基于 Issue 正文和澄清结果提炼}

### 目标
- 核心功能 1
- 核心功能 2

### 非目标
- 不包含 XXX
- 不支持 XXX

### 技术方案
{实现思路，包含关键文件和修改点}

### 跨仓库任务拆分（如适用）
- Repo: org/repo-a
  - 变更：path1/, path2/file.go
  - 说明：为什么需要在该仓库变更
- Repo: org/repo-b
  - 变更：pkg/x/, docs/
  - 说明：与 repo-a 的依赖关系（先后顺序）

### 验收标准
- [ ] 功能 1 完成
- [ ] 测试通过
- [ ] 文档更新

### 文件变更预估
- 修改：internal/webhook/handler.go（新增 /clarify 触发逻辑）
- 新增：internal/workflow/clarify.go（澄清问题生成）
- 测试：internal/workflow/clarify_test.go

### 跨仓库影响面（如适用）
- org/repo-a：预估修改 3 个文件
- org/repo-b：预估新增 2 个文件

### 风险评估
- 复杂度：中等（预计 2-3 天）
- 破坏性：低（不影响现有功能）
- 依赖：需要扩展 webhook/types.go
```

**大需求拆分判断**：
- 如果预估文件变更 > 8 个 → 提示用户拆分为多个 Issue
- 如果复杂度高 → 建议分多个 PR 提交

**用户响应**：
- 回复"确认"或 `/code` → 进入开发阶段（也可直接跳过 PRD 在 Issue 任意时刻触发 `/code`）
- 回复修改意见 → Agent 更新 PRD（重新调用 Provider）

**状态存储**：
```go
Stage: "prd"
PRD: "{生成的 PRD 文档内容}"
```

---

### **Stage 2：代码开发（/code）**

**触发**：用户回复 `/code`（可直接执行，不依赖 `/clarify` 或 `/prd`）

**Agent 行为（R1）**：
1. 读取 Issue 全文 + 澄清 + PRD（**完整上下文**，缺失则使用可用信息并提示风险）
2. Clone 源仓库默认分支到临时目录
3. 生成代码变更并提交到分支 `swe/issue-{issueNumber}-{timestamp}`（多 PR 拆分时为 `swe/{category}-{issueNumber}-{timestamp}`）
4. 推送到 GitHub（**不自动创建 PR**）
5. 在评论中返回：
   - 分支链接：`https://github.com/{repo}/tree/{branch}`
   - **手动创建 PR 链接**：`https://github.com/{repo}/compare/{base}...{branch}`（预填 `title` 与 `body=Fixes #<number>`）
   - 变更文件列表
   - 成本

**与当前实现的关系**：
- 现实现已生成 compare 链接供手动创建 PR（不调用 PR 创建 API）。
- 本阶段保持该策略，仅统一评论文案，并在 compare URL 中预填 `title` 与 `body=Fixes #<number>`。

**用户决策**：
- 满意 → 手动点击链接创建 PR
- 不满意 → 回复修改意见，Agent 在同一分支继续修复（在 PR 上重新触发 `/code`，也可回退到 `/clarify` `/prd` 补充信息）

**状态存储**：
```go
Stage: "coding"
BranchName: "swe/issue-123-1234567890"
FilesChanged: []string{"handler.go", "clarify.go"}
```

---

### **Stage 3：Code Review（/code-review，可选）**

**触发**：用户在 PR 评论中回复 `/code-review`（与是否执行 `/clarify`、`/prd` 无关）

**Agent 行为**：
1. 读取 PR 文件与 patch（GitHub API：ListPullFiles）
2. 读取 PRD（审查标准，如无 PRD 则基于 Issue/澄清摘要生成简要审查基线）
3. 调用 Provider 生成 Review 意见
4. 在 PR 中发布 Review 评论（支持行内评论）

**Review Prompt**：
```markdown
你是一个代码审查专家。请审查以下代码变更：

【PRD 摘要】{prd_summary}
【变更文件（patch）】
{pr_patches}

【审查标准】
- 代码风格是否符合项目规范（参考现有代码）
- 是否有潜在的 Bug（边界条件、错误处理）
- 是否有性能问题（O(n^2) 循环、内存泄漏）
- 是否需要补充测试
- 是否符合 PRD 要求

请输出 Review 意见（GitHub Review 格式）。
```

**输出格式**：
```markdown
## 🔍 Code Review 结果

### ✅ 通过的检查
- 代码风格符合项目规范
- 无明显 Bug
- 符合 PRD 要求

### ⚠️ 需要改进
- `handler.go:132` 缺少错误处理，建议加 `if err != nil`
- `task.go:250` 函数过长（150 行），建议拆分

### 📝 建议（可选）
- 补充单元测试覆盖 `/clarify` 触发逻辑
- 更新 README 文档

### 💰 成本
- Review 成本：$0.05
```

**用户决策**：
- 接受 Review → 在 PR 上回复 `/code` 触发修复
- 忽略 Review → 直接 Merge PR

---

### **Stage 4：修复迭代（/code，可选）**

**触发**：
1. PR Review 提出问题，用户在 PR 上回复 `/code`
2. 或：用户在 PR 评论中直接附带 `/code ...` 指令（可跟随具体意见）

**Agent 行为**：
1. 读取 Review 意见（或用户评论）
2. 读取当前 PR 分支的代码
3. 调用 Provider 生成修复代码
4. 在 **现有 PR 分支** 上提交新 commit（**不创建新 PR**）
5. 推送到 GitHub
6. 在 PR 中评论修复结果

**关键实现**（已支持）：
```go
// executor/task.go:308
if task.IsPR && task.PRState == "open" && task.PRBranch != "" {
    // PR is open → push to existing PR branch
    branchName = task.PRBranch
    shouldCreateBranch = false
}
```

**修复次数限制**：
- 单个 PR 最多自动修复 **3 次**
- 超过 3 次 → 提示需要人工介入

**状态存储**：
```go
Stage: "review"
FixAttempts: 2  // 已修复 2 次
MaxFixAttempts: 3
```

---

### **Stage 5：完成（Done）**

**触发**：PR 被 Merge（监听 `pull_request.closed` 且 `merged=true`）

**Agent 行为**：
- 更新工作流状态：`Stage: "done"`
- 记录指标：总成本、总耗时、修复次数
- 清理临时数据

**指标收集**：
```go
Metrics {
    TotalCost: 0.25,         // 总成本（美元）
    TotalTime: "2h 30m",     // 从 /clarify 到 PR Merge
    ClarifyRounds: 2,        // 澄清轮次
    PRDIterations: 1,        // PRD 修改次数
    FixAttempts: 2,          // 代码修复次数
    FilesChanged: 8,         // 变更文件数
}
```

## 详细需求

### 跨仓库任务拆分（Multi-Repo）（Post-1.0 规划）

> 本节为后续版本规划，R1 不纳入。

**新数据结构**（扩展现有 SplitPlan）：
```go
// github.SplitPlan 现仅按“类别”拆分单仓库改动；扩展支持跨仓库维度：
type RepoSubPR struct {
    Repo       string            // 目标仓库（org/name）
    SubPR      SubPR             // 复用现有子 PR 结构（名称、描述、文件、依赖等）
}

type MultiRepoPlan struct {
    Items          []RepoSubPR   // 跨仓库子任务列表
    CreationOrder  []int         // 全局创建顺序（包含跨仓库依赖）
    TotalRepos     int           // 涉及仓库数
}
```

**识别与约束**：
- 识别依据：
  - Issue/PRD 中显式提到 `org/repo`、路径或模块边界
  - 配置文件 `.swe-agent.yml` 中的 `cross_repo.whitelist`
- 约束：
  - 仅同组织/同用户且安装了本 App 的仓库
  - 限制最大仓库数（默认 3），默认仅允许白名单匹配
  - 禁止修改各仓库的黑名单文件（如 CI、依赖清单）

**执行策略**：
- 顺序：
  - 先独立子任务（无依赖），后依赖子任务；跨仓库依赖通过 `CreationOrder` 控制
- 推送与链接：
  - 每个仓库创建独立分支并推送，输出 compare 链接（PR 描述预填 `Refs {sourceRepo}#{issue}`）
- 展示：
  - 源 Issue 的跟踪评论中，按仓库聚合展示子任务、分支、链接与状态

**确认与幂等**：
- `/prd` 输出跨仓库计划草案，需用户确认后方可 `/code` 执行
- 重复 `/code`：对已存在的目标仓库分支追加提交；新仓库按计划继续执行

**失败与回滚**：
- 任一仓库推送失败：记录失败项并继续其他仓库（不阻塞），汇总结果提示人工处理
- 建议在 PR 描述中引用源 Issue，便于追踪

### 工作流状态追踪

**数据结构**（新增）：
```go
// taskstore/workflow.go（新文件）
type WorkflowState struct {
    IssueNumber    int
    Repo           string
    Stage          string // "clarify", "prd", "coding", "review", "done"

    // 澄清历史
    Clarifications []Clarification

    // PRD 内容
    PRD            string

    // 代码变更
    BranchName     string
    FilesChanged   []string

    // 修复历史
    FixAttempts    int
    MaxFixAttempts int

    // 成本追踪
    TotalCost      float64

    // 时间追踪
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type Clarification struct {
    Question  string    // Agent 提出的问题
    Answer    string    // 用户的回答（可为空）
    Timestamp time.Time
}

// 存储接口
func (s *Store) GetWorkflow(repo string, issueNumber int) (*WorkflowState, error)
func (s *Store) UpdateWorkflow(state *WorkflowState) error
```

**存储方案**：
- **初期**：内存存储（`map[string]*WorkflowState`，key=`{repo}#{issue_number}`）
- **长期**：迁移到数据库（复用 `taskstore.Store`）

### Stage 状态映射

| `WorkflowState.Stage` | 描述 | 对应阶段 |
|----------------------|------|----------|
| `""`/`"clarify"` | 尚未触发或正在澄清 | Stage 0 |
| `"prd"` | PRD 生成中或已生成待确认 | Stage 1 |
| `"coding"` | 代码开发中 | Stage 2 |
| `"review"` | Code Review 阶段以及后续修复迭代 | Stage 3 & Stage 4 |
| `"done"` | 流程完成（PR merge 或手动结束） | Stage 5 |

> Stage 3 与 Stage 4 共享 `"review"` 状态，需结合 `FixAttempts`、PR 上下文等字段判断是首轮 Review 还是修复迭代。

### 幂等与重复触发策略（新增）
- `/clarify`：追加新问题，已存在的问题可被编辑标记为已解决
- `/prd`：默认覆盖上一次生成结果（保留历史版本于状态存储中）
- `/code`：Issue 上重复触发在同一分支继续提交；PR 上重复触发进入修复路径
- `/code-review`：多次触发视为新一轮审查，覆盖上一次机器人审查结果

### 状态字段扩展（跨仓库）
```go
type WorkflowState struct {
    // ...已有字段
    MultiRepoPlan *MultiRepoPlan  // 跨仓库子任务计划（确认后执行）
    TargetRepos   []string        // 实际涉及的仓库清单
}
```

### 完整上下文获取

**问题**：当前 `composeDiscussionSection()` 只获取评论，不包含工作流历史

**改进**（executor/task.go:550）：
```go
func (e *Executor) composeFullContext(task *webhook.Task, token string) string {
    // 1. 获取 Issue 所有评论
    issueComments := e.ghClient.ListIssueComments(task.Repo, task.Number, token)

    // 2. 获取工作流状态
    workflow := e.workflowStore.GetWorkflow(task.Repo, task.Number)

    // 3. 组装完整上下文
    var context strings.Builder

    // Issue 正文
    context.WriteString("# Issue: " + task.IssueTitle + "\n\n")
    context.WriteString(task.IssueBody + "\n\n")

    // 澄清历史
    if len(workflow.Clarifications) > 0 {
        context.WriteString("## 需求澄清\n\n")
        for _, c := range workflow.Clarifications {
            context.WriteString(fmt.Sprintf("Q: %s\nA: %s\n\n", c.Question, c.Answer))
        }
    }

    // PRD
    if workflow.PRD != "" {
        context.WriteString("## PRD\n\n")
        context.WriteString(workflow.PRD + "\n\n")
    }

    // 评论历史
    context.WriteString("## 讨论历史\n\n")
    context.WriteString(formatDiscussion(issueComments, nil))

    // （跨仓库）若存在 MultiRepoPlan，则追加子任务与目标仓库清单
    if workflow.MultiRepoPlan != nil {
        context.WriteString("## 跨仓库任务\n\n")
        for i, item := range workflow.MultiRepoPlan.Items {
            context.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, item.Repo, item.SubPR.Name))
        }
        context.WriteString("\n")
    }

    return context.String()
}
```

### 触发词解析

**新增函数**（webhook/handler.go）：
```go
// parseTriggerCommand 解析触发词和参数
func (h *Handler) parseTriggerCommand(body string) (action string, content string, found bool) {
    triggers := []string{"/clarify", "/prd", "/code-review", "/code"}

    for _, trigger := range triggers {
        if idx := strings.Index(body, trigger); idx != -1 {
            action = strings.TrimPrefix(trigger, "/")
            content = strings.TrimSpace(body[idx+len(trigger):])
            found = true
            return
        }
    }

    return "", "", false
}

// determineWorkflowAction 根据触发词和当前阶段决定行为
func (h *Handler) determineWorkflowAction(task *Task, action string) string {
    // 显式触发词优先
    if action != "" {
        return action
    }

    // 未来：智能触发模式（根据当前 Stage 自动判断）
    workflow := h.workflowStore.GetWorkflow(task.Repo, task.Number)
    switch workflow.Stage {
    case "":
        return "clarify" // 首次触发 → 澄清
    case "clarify":
        return "prd"     // 澄清完成 → 生成 PRD
    case "prd":
        return "coding"  // PRD 确认 → 开发代码
    default:
        return "coding"  // 默认行为
    }
}
```

### Prompt 模板管理

**新增文件**：
```
internal/prompt/
├── clarify.go      # 需求澄清 Prompt
├── prd.go          # PRD 生成 Prompt
├── review.go       # Code Review Prompt
└── manager.go      # Prompt 模板管理器（已存在）
```

**示例**（internal/prompt/clarify.go）：
```go
package prompt

func GenerateClarifyPrompt(issue Issue, repoContext RepoContext) string {
    return fmt.Sprintf(`你是一个需求分析专家。用户提出了以下需求：

【Issue 标题】%s
【Issue 正文】%s

【仓库信息】
- 项目语言：%s
- 主要目录：%s
- README 摘要：%s

请提出 5-10 个澄清问题，确保需求清晰。问题类型：
- 功能边界：这个功能是否包括 XXX？
- 技术约束：是否需要兼容 XXX 版本？
- 验收标准：如何判断功能完成？
- 依赖关系：是否依赖其他未完成的功能？

输出格式（Markdown Checklist）：
- [ ] **问题 1**：XXX
- [ ] **问题 2**：XXX
`, issue.Title, issue.Body, repoContext.Language, repoContext.Structure, repoContext.ReadmeSummary)
}
```

### PR 创建策略说明（更新）

- 当前实现：生成 compare 链接供“手动创建 PR”，不调用 PR 创建 API。
- 本方案：保持该策略，统一评论文案，并在 compare URL 中预填 `title` 与 `body=Fixes #<number>`。

### 修复次数限制

**新增检查**（executor/task.go）：
```go
func (e *Executor) shouldAllowFix(task *webhook.Task) (bool, string) {
    workflow := e.workflowStore.GetWorkflow(task.Repo, task.Number)

    if workflow.FixAttempts >= workflow.MaxFixAttempts {
        return false, fmt.Sprintf(
            "已达到最大修复次数（%d/%d），需要人工介入",
            workflow.FixAttempts,
            workflow.MaxFixAttempts,
        )
    }

    return true, ""
}

// 在 Execute() 中调用
workflow := e.workflowStore.GetWorkflow(task.Repo, task.Number)
if workflow == nil {
    workflow = taskstore.NewWorkflowState(task.Repo, task.Number) // 兜底，避免首次触发时 nil panic
}
if task.IsPR && strings.Contains(task.Prompt, "/code") && workflow.Stage == "review" {
    if allowed, reason := e.shouldAllowFix(task); !allowed {
        return e.handleError(task, tracker, token, reason)
    }
}
```

> 若暂未实现 `NewWorkflowState`，需自行提供最小初始化（例如设置 `MaxFixAttempts` 默认值），核心目的是在第一次修复前保证 `workflow` 非 nil。

## PRD 模板（未来自动生成至 `docs/prd/ISSUE-<number>.md`）
- 背景
- 目标（成功标准/可量化指标）
- 非目标
- 用户故事与验收标准（Given/When/Then）
- 方案（架构/数据结构/事件扩展/Prompt 策略/执行流程）
- 跨仓库任务拆分（目标仓库、路径、依赖顺序）
- 任务拆解（开发/测试/文档/回滚）
- 测试策略（单测/集成/手验要点）
- 风险（误改/权限/回归/配额）与缓解
- 里程碑（周）与返工阈值
- 附录（参考链接、样例 payload）

## 配置项

### 核心配置

```bash
# 触发词配置
TRIGGER_KEYWORD="@assistant"  # 默认触发词（已支持）

# 工作流配置（新增）
ENABLE_WORKFLOW_MODE=true     # 是否启用多阶段工作流
DEFAULT_WORKFLOW_STAGE="clarify"  # 首次触发的默认阶段

# 修复限制（新增）
MAX_FIX_ATTEMPTS=3            # 单个 PR 最多修复次数

# PR 创建策略（新增）
AUTO_CREATE_PR=false          # 是否自动创建 PR（默认关闭）。当前实现不启用自动创建，仅生成 compare 链接
PR_DRAFT_MODE=false           # 是否创建 Draft PR（仅当启用自动创建时生效）

# Code Review 配置（新增）
ENABLE_AUTO_REVIEW=false      # 是否默认开启 Code Review
REVIEW_STANDARDS="style,bug,performance,test"  # 审查标准（逗号分隔）

# 成本控制（新增，P0）
DAILY_CALL_LIMIT=100          # 每日 AI 调用上限
PER_ISSUE_COST_LIMIT=1.0      # 单个 Issue 成本上限（美元）
COST_ALERT_THRESHOLD=0.5      # 成本告警阈值（美元）

# Provider 配置（已支持）
PROVIDER="claude"             # or "codex"
CLAUDE_API_KEY="sk-ant-xxx"
CLAUDE_MODEL="claude-sonnet-4-5-20250929"

# 调度配置（已支持）
MAX_RETRIES=3
BACKOFF_SECONDS=60

# 测试配置（可选）
ENABLE_TEST_HOOKS=true        # 是否执行测试钩子
TEST_COMMAND=""               # 留空表示自动检测（go test, npm test 等）

# 调试模式
DRY_RUN=false                 # 仅生成计划，不执行代码
DEBUG_WORKFLOW=false          # 打印工作流状态日志

### 权限配置（新增）

```yaml
# .swe-agent.yml（仓库根目录）
auth:
  # 默认仅 App 安装者可触发；可选开启白名单
  whitelist:
    users:
      - "owner"
      - "maintainer1"
```

### 跨仓库配置（新增）

```yaml
# .swe-agent.yml（仓库根目录）
cross_repo:
  enabled: false           # R1 要求为 false；默认关闭，后续版本开启后才允许跨仓库
  org_scope_only: true     # 仅限同组织/同用户
  max_repos: 3             # 单次执行涉及的最大仓库数
  whitelist:
    repos:
      - "org/repo-a"
      - "org/repo-b"
  blacklist:
    files:
      - ".github/workflows/*"
      - "go.mod"
  pr:
    auto_create: false     # 仍不自动创建 PR
    title_prefix: "feat: "
    body_template: |
      Refs {sourceRepo}#{issueNumber}
      {summary}
```
```

### 配置优先级

1. **环境变量**（最高优先级）
2. **`.env` 文件**
3. **默认值**（代码中硬编码）

### 白名单/黑名单（可选，未来支持）

```yaml
# .swe-agent.yml（仓库根目录）
workflow:
  enabled: true
  stages:
    - clarify
    - prd
    - coding
    - review

  cost_limit:
    daily: 10.0      # 每日成本上限（美元）
    per_issue: 1.0   # 单 Issue 成本上限

  blacklist:
    files:
      - ".github/workflows/*"  # 禁止修改 CI 配置
      - "go.mod"               # 禁止修改依赖

  whitelist:
    users:
      - "owner"      # 仅 owner 可触发（示例）
```

## 验收标准

### Stage 0：需求澄清
- [ ] 用户在 Issue 中回复 `/clarify`
- [ ] ≤1 分钟，Issue 下出现澄清问题列表（5-10 个问题）
- [ ] 用户回答问题后，工作流状态更新为 `"clarify"`

### Stage 1：PRD 生成
- [ ] 用户回复 `/prd`
- [ ] ≤1 分钟，Issue 下出现 PRD 文档（Markdown 格式）
- [ ] PRD 包含：背景、目标、技术方案、验收标准、文件变更预估（如适用，包含跨仓库任务拆分与影响面）
- [ ] 工作流状态更新为 `"prd"`

### Stage 2：代码开发
- [ ] 用户回复 `/code`
- [ ] ≤2 分钟，Issue 下出现完成评论：
  - 单仓库：分支链接、**手动创建 PR 链接**（compare 预填 `title` 与 `Fixes #<number>`）、变更文件列表、成本
  - 跨仓库：按仓库聚合展示分支与 compare 链接（PR 描述预填 `Refs {sourceRepo}#{issue}`）、变更文件列表（按仓库分组）、成本（总计与分仓）
- [ ] 代码已推送到分支（`swe/issue-{number}` 或 `swe/{category}-{number}-{ts}`）
- [ ] 工作流状态更新为 `"coding"`

### Stage 3：Code Review（可选，R1 为总结级别）
- [ ] 用户在 PR 评论中回复 `/code-review`
- [ ] ≤1 分钟，PR 下出现 Review 评论（基于 PR files+patch 输入；R1 不要求行内评论）
  - 通过的检查
  - 需要改进的地方（带文件:行号）
  - 建议（可选）
  - Review 成本
- [ ] Review 结果准确（无明显误判）

### Stage 4：修复迭代
- [ ] 用户在 PR 上回复 `/code`（可附带具体意见）
- [ ] ≤2 分钟，PR 分支上出现新 commit
- [ ] 修复内容符合 Review 意见
- [ ] 修复次数 ≤3 次（超过提示人工介入）
- [ ] 工作流状态更新为 `"review"`

### 通用验收
- [ ] 防抖：同一评论 ID 12h 内不重复执行
- [ ] 权限：非 App Installer 无法触发（已实现）
- [ ] 成本追踪：每次调用记录成本，累计到 `WorkflowState.TotalCost`
- [ ] 状态追踪：每个阶段更新 `CommentTracker.State.Tasks` 进度条
- [ ] 错误处理：失败时在评论中明确提示错误原因与下一步

## 监控与指标

### 工作流指标（新增）

```go
type WorkflowMetrics struct {
    // 成功率
    TotalIssues       int     // 总处理 Issue 数
    CompletedIssues   int     // 完成的 Issue 数（PR 已 Merge）
    SuccessRate       float64 // 完成率 = Completed / Total

    // 阶段分布
    ClarifyStageCount int     // 停在澄清阶段的 Issue 数
    PRDStageCount     int     // 停在 PRD 阶段的 Issue 数
    CodingStageCount  int     // 停在开发阶段的 Issue 数
    ReviewStageCount  int     // 停在审查阶段的 Issue 数

    // 澄清效率
    AvgClarifyRounds  float64 // 平均澄清轮次
    ClarifyAcceptRate float64 // 澄清后直接进入 PRD 的比例

    // PRD 质量
    AvgPRDIterations  float64 // 平均 PRD 修改次数
    PRDAcceptRate     float64 // 一次通过率

    // 代码质量
    AvgFixAttempts    float64 // 平均修复次数
    FirstTimePassRate float64 // 代码一次通过率（无需修复）

    // 成本
    AvgCostPerIssue   float64 // 单 Issue 平均成本（美元）
    TotalCostToday    float64 // 今日总成本
    CostByStage       map[string]float64 // 各阶段成本分布
    CostByRepo        map[string]float64 // 按仓库统计成本（跨仓库）

    // 耗时
    AvgTimePerIssue   string  // 单 Issue 平均耗时（从 /clarify 到 PR Merge）
    AvgTimeByStage    map[string]string // 各阶段平均耗时
    AvgReposPerIssue  float64 // 单 Issue 平均涉及仓库数（跨仓库）
}
```

### 现有指标（保留）
- 任务完成/失败率
- 创建 PR 成功率
- Provider 错误分布
- 事件去重命中率

### 告警规则（新增）

| 指标 | 阈值 | 告警级别 |
|------|------|----------|
| 今日总成本 | > $10 | P1（高） |
| 单 Issue 成本 | > $1 | P2（中） |
| 成功率 | < 50% | P1（高） |
| 修复次数 | > 3 次/Issue | P2（中） |
| 澄清轮次 | > 5 次/Issue | P3（低） |

## Release 1.0（R1）与 Post-1.0

### **M1（本周，P0）：核心工作流打通**
- [x] 基础设施（已完成）：
  - Webhook handler、任务队列、执行器、评论追踪
  - 权限验证（App Installer）
  - 防抖机制（commentDeduper）
- [ ] 工作流状态追踪（新增）：
  - `taskstore/workflow.go`：WorkflowState 数据结构
  - 内存存储实现
- [ ] 触发词解析（新增）：
  - `/clarify`, `/prd`, `/code`, `/code-review`
  - `webhook/handler.go`：parseTriggerCommand()
- [ ] Stage 0：需求澄清：
  - `internal/prompt/clarify.go`：澄清 Prompt 模板
  - 读取仓库上下文（README, 目录结构）
- [ ] Stage 1：PRD 生成：
  - `internal/prompt/prd.go`：PRD Prompt 模板
  - 评论中展示 PRD（不落盘）
- [ ] Stage 2：代码开发：
  - 统一“手动创建 PR 链接”的评论文案
  - compare URL 预填 `title` 与 `Fixes #<number>`
- [ ] 成本控制（P0）：
  - `DAILY_CALL_LIMIT`, `PER_ISSUE_COST_LIMIT`
  - 超限阻止执行并告警
- [ ] 测试：
  - 单元测试覆盖核心逻辑
  - 集成测试验证完整流程

**验收标准**：
- ✅ 完整流程可运行：Issue → /clarify → /prd → /code → 手动创建 PR
- ✅ 成本控制生效：超限时阻止执行
- ✅ 工作流状态正确追踪

---

### **M2（下周，P1）：Code Review + 修复迭代**
- [ ] Stage 3：Code Review：
  - `internal/prompt/review.go`：Review Prompt 模板
  - 拉取 PR files+patch 并审查
- [ ] Stage 4：修复迭代：
  - 支持 `/code` 在 PR 上触发修复
  - 修复次数限制（MAX_FIX_ATTEMPTS=3）
  - 在现有 PR 分支上提交（已支持）
- [ ] 完整上下文获取：
  - `composeFullContext()` 组装澄清、PRD、评论历史（含 PR 历史 Review）
- [ ] 测试钩子（可选）：
  - 自动检测测试命令（go test, npm test 等）
  - 测试失败 → 阻止 PR 创建或标记 Draft

**验收标准**：
- ✅ Code Review 准确性 > 80%
- ✅ 修复迭代成功率 > 60%
- ✅ 修复次数控制生效

---

### Release 1.0（R1）功能范围

- 触发词与阶段化：`/clarify`、`/prd`、`/code`、`/code-review`（显式触发，智能触发关闭）
- 权限基线：默认仅 App Installer，可配置白名单
- 事件监听：`issue_comment.created`、`pull_request_review_comment.created`、`pull_request_review.submitted`（需显式触发）、`pull_request.closed (merged=true)`
- Stage 0（澄清）：5-10 个问题（Markdown Checklist）
- Stage 1（PRD）：结构化 PRD（背景/目标/非目标/技术方案/验收/变更预估）
- Stage 2（开发）：
  - Clone 源仓库，最小变更提交到分支 `swe/issue-{issue}-{ts}`（或按类别拆分 `swe/{category}-{issue}-{ts}`）
  - 推送，并在评论中提供分支链接与 compare 链接（预填 title 与 `Fixes #<number>`）
  - 支持多 PR 拆分显示（仍手动创建 PR）
- Stage 3（Review，可选）：基于 PR files+patch 的总结级 Review（R1 不要求行内评论）
- Stage 4（修复）：`/code` 在 PR 场景下追加提交；`MAX_FIX_ATTEMPTS=3`
- 成本闸门：`DAILY_CALL_LIMIT`、`PER_ISSUE_COST_LIMIT`、`COST_ALERT_THRESHOLD`
- 防抖与防环：Bot 忽略、12h 去重、按 `repo#number` 串行
- 追踪与回帖：CommentTracker 统一进度/链接/成本渲染

R1 验收标准
- Issue → `/clarify` → `/prd` → `/code` → 手动创建 PR 全链路可用
- compare 链接参数正确（title、Fixes）且不自动创建 PR
- 成本闸门生效：超限阻断并清晰回帖
- `/code-review` 能输出总结级 Review；PR 场景下 `/code` 生效且次数受限
- 事件去重/权限校验/错误提示工作正常

---

### Post-1.0（Backlog）
- [ ] 智能触发模式（可选）：
  - `/code` 根据当前 Stage 自动判断行为
- [ ] PRD 文件落盘（可选）：
  - 落盘到 `docs/prd/ISSUE-{number}.md`
  - PR 描述引用 PRD
- [ ] 大需求拆分提示：
  - 文件变更 > 8 个 → 提示拆分
  - 复杂度高 → 建议多个 PR
- composeFullContext（整合澄清/PRD/评论历史）
- 智能触发模式
- PRD 文件落盘（PR 描述自动引用 PRD）
- 监控面板与告警（实时/阈值）
- 黑白名单细化与数据库落存
- 跨仓库能力（按“跨仓库任务拆分（Post-1.0 规划）”实现）
- [ ] 更细 Playbook：
  - 文档迁移场景（替换链接、更新示例）
  - 语法迁移场景（API 升级、废弃警告）
  - 编译器小修（错误消息优化）

**验收标准**：
- ✅ 用户体验流畅（减少手动步骤）
- ✅ 大需求拆分建议准确性 > 70%

---

### **M4（1 个月，P3）：监控 + 黑白名单**
- [ ] 指标面板：
  - 工作流各阶段成功率
  - 平均成本、耗时
  - 澄清/PRD/修复质量
- [ ] 告警系统：
  - 成本告警（今日总成本 > $10）
  - 成功率告警（< 50%）
- [ ] 黑白名单：
  - 文件黑名单（禁止修改 CI 配置）
  - 用户白名单（仅 owner 可触发）
- [ ] 数据库存储：
  - 迁移 WorkflowState 到数据库
  - 支持跨会话查询
- [ ] 跨仓库能力（第二阶段）：
  - MultiRepoPlan 执行与依赖顺序控制
  - 源 Issue 聚合多仓库状态与成本
  - 指标新增“跨仓库任务成功率/平均仓库数”

**验收标准**：
- ✅ 指标面板可用
- ✅ 告警及时触发（5 分钟内）

## 风险与缓解

### **P0 风险：成本失控**
**风险**：自动触发导致 AI 调用量爆炸，月费失控
**影响**：严重（$100+/月）
**缓解**：
- ✅ **硬性配额**：`DAILY_CALL_LIMIT=100`, `PER_ISSUE_COST_LIMIT=1.0`
- ✅ **人工触发**：所有阶段都需要显式触发词（/clarify, /prd 等）
- ✅ **修复次数限制**：最多 3 次/PR
- ✅ **告警机制**：成本超限实时告警
- [ ] 仓库白名单：初期只对信任仓库启用（M4）

---

### **P1 风险：误改代码**
**风险**：AI 修改关键文件（如 CI 配置、依赖文件）导致破坏
**影响**：中等（需要人工回滚）
**缓解**：
- ✅ **最小变更原则**：Prompt 明确指示"最小改动"
- ✅ **人工审核**：不自动创建 PR，人工审核分支后再创建
- [ ] **文件黑名单**：禁止修改 `.github/workflows/*`, `go.mod` 等（M4）
- [ ] **失败降级**：严重错误 → 创建 Draft PR 而非正常 PR

---

### **P1 风险：循环触发**
**风险**：Bot 评论触发新的 webhook，导致无限循环
**影响**：中等（成本翻倍 + GitHub API 限流）
**缓解**：
- ✅ **Bot 过滤**：已实现（handler.go:116 忽略 Bot 评论）
- ✅ **事件去重**：已实现（commentDeduper 12h 去重）
- ✅ **串行执行**：按 `repo#number` 串行，避免并发冲突

---

### **P2 风险：权限问题**
**风险**：缺少 `gh` CLI 权限或环境配置错误；触发者权限误配导致越权
**影响**：低（功能不可用，但不会破坏）
**缓解**：
- ✅ **启动时检查**：检测 `gh` CLI 和权限（当前：运行时降级）
- [ ] **配置验证**：启动时验证所有必需环境变量（M1）
- ✅ **默认最小权限**：默认仅 App 安装者可触发；可选开启白名单

---

### **P2 风险：跨仓库/跨语言兼容性**
**风险**：非 Go 项目的测试策略不统一，导致部分功能失效
**影响**：中等（部分仓库不可用）
**缓解**：
- ✅ **尽力而为**：测试失败不阻止 PR 创建，只标记警告
- [ ] **自动检测**：根据 package.json, Cargo.toml 等检测项目类型（M2）
- ✅ **明确错误**：失败时提供可见解释与人工接管指引

---

### **P1 风险：跨仓库误操作/越权**
**风险**：识别到的目标仓库不在白名单或未安装 App；误触达不相关仓库；跨仓库依赖顺序错误导致编译失败
**影响**：中等到严重（需要多方回滚/协调）
**缓解**：
- ✅ **白名单 + 组织范围**：仅同组织/同用户且白名单匹配的仓库
- ✅ **计划先审**：/prd 阶段输出跨仓库计划，需显式确认后才执行
- ✅ **最小变更路径**：限制允许修改的目录/文件类型（黑名单生效）
- ✅ **依赖顺序控制**：MultiRepoPlan 指定 CreationOrder，先独立后依赖

---

### **P3 风险：PRD 质量不稳定**
**风险**：AI 生成的 PRD 可能偏离用户意图
**影响**：低（用户可以修改后再 `/code`）
**缓解**：
- ✅ **澄清环节**：Stage 0 确保需求清晰
- ✅ **迭代支持**：用户可以要求重新生成 PRD
- [ ] **Prompt 优化**：根据实际效果调整 PRD Prompt（持续优化）

## 开放问题

1. **是否支持智能触发模式？**（M3 决定）
   - 优点：用户体验更好，只需一个 `/code` 走完全流程
   - 缺点：误判风险，可能跳过某些阶段

2. **PRD 是否必须落盘文件？**（M2 决定）
   - 优点：便于追溯，可以在 PR 中引用
   - 缺点：可能与现有文档规范冲突

3. **是否默认开启 Code Review？**（M2 决定）
   - 优点：质量更高
   - 缺点：成本翻倍（每个 Issue 多 1-2 次 AI 调用）
   - **当前决定**：默认关闭，按需触发

4. **修复次数限制是否过于严格？**（M2 根据实际数据调整）
   - 当前：3 次/PR
   - 可能调整为：5 次/PR 或根据复杂度动态调整

5. **是否需要支持"草稿 PRD"功能？**（M3 决定）
   - 场景：需求不清晰时，生成草稿 PRD 供讨论
   - 实现：新增 `/prd-draft` 触发词

6. **行内 Review 评论是否作为首期目标？**（M2/M3 决定）
   - 现阶段可先输出统一评论包含文件:行号；行内评论后续迭代

7. **跨仓库目标识别策略？**（M3 决定）
   - 方案 A：仅使用 `.swe-agent.yml` 的 whitelist 显式声明
   - 方案 B：结合 Issue/PRD 中的 `org/repo` 引用进行建议，仍需用户确认
   - 方案 C：扫描组织下安装了 App 的仓库（高成本/高风险），不采纳为默认
