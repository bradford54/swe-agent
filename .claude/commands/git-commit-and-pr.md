---
allowed-tools: [Bash, Read, Glob, TodoWrite, Edit, Grep]
description: 'Unified Git workflow: commit code and/or create PR with intelligent automation'
---

## Usage

```bash
# 仅提交（默认行为，如果有未提交的修改）
/git-commit-and-pr [--issue <ISSUE_ID>] [--message <COMMIT_MESSAGE>]

# 仅创建 PR（如果工作目录干净）
/git-commit-and-pr --pr [--issue <ISSUE_ID>] [--base <BASE_BRANCH>]

# 提交后立即创建 PR
/git-commit-and-pr --all [--issue <ISSUE_ID>] [--base <BASE_BRANCH>]
```

## Context

- 增量预检：提交前执行 `./scripts/dx lint`；若后端改动需先跑 `./scripts/dx build backend`，随后按需执行 `./scripts/dx build sdk --online`（仅 DTO/API 变更）、再决定是否运行 `./scripts/dx build front` 与 `./scripts/dx build admin`
- 若增量预检包含 `./scripts/dx build sdk --online`，需检查并清理无需提交的 openapi.json 变更
- 自动检测当前状态并智能选择操作模式
- 支持单独提交、单独创建 PR，或组合执行
- 所有操作自动关联 Issue

## Your Role

你是 **Git 工作流协调者**，负责管理智能化的 Git 提交和 PR 创建流程。你需要：

1. **状态检查员** – 验证当前分支和变更状态
2. **分支管理员** – 处理分支创建和命名规范
3. **质量守护者** – 执行增量预检（lint 必跑；若后端改动先跑 backend，再视 DTO/API 变更决定是否执行 `./scripts/dx build sdk --online`，最后按需构建 front/admin），并检查 openapi.json 是否需要提交
4. **提交协调员** – 生成规范的提交信息并执行提交
5. **测试执行者** – 在必要时执行 E2E 测试（PR 模式 + Main 分支提交）
6. **PR 生成器** – 生成 PR 标题和描述
7. **Issue 关联员** – 确保提交与 Issue 正确关联

## Workflow Selection Logic (智能模式选择)

### 自动模式判断

根据用户输入和当前状态，自动选择执行模式：

1. **仅 Commit 模式** (当以下任一条件满足)：
   - 用户未使用 `--pr` 或 `--all` 标志
   - 工作目录有未提交的修改
   - 当前在 main 分支且需要创建功能分支

2. **仅 PR 模式** (当以下所有条件满足)：
   - 用户使用了 `--pr` 标志
   - 工作目录干净（无未提交修改）
   - 当前在功能分支（非 main/master）
   - 已有提交未创建 PR

3. **Commit + PR 模式** (当以下条件满足)：
   - 用户使用了 `--all` 标志
   - 工作目录有未提交的修改
   - 或用户明确要求两步都执行

### 决策流程图

```
开始
  ↓
检查工作目录状态
  ↓
有未提交修改？
  ├─ 是 → 检查用户标志
  │       ├─ --all → [Commit + PR 模式]
  │       └─ 其他 → [仅 Commit 模式]
  └─ 否 → 检查用户标志
          ├─ --pr 或 --all → 检查分支
          │                            ├─ 功能分支 → [仅 PR 模式]
          │                            └─ main → 错误：不能从 main 创建 PR
          └─ 其他 → 提示：工作目录干净，无需操作
```

## Process

### 阶段 0：初始化

**初始化和模式选择**

1. **解析命令参数**
   - `--issue <ID>`: Issue ID
   - `--message <MSG>`: 自定义提交信息
   - `--base <BRANCH>`: PR 基础分支（默认 main）
   - `--pr`: 仅创建 PR
   - `--all`: 先提交后创建 PR

2. **检查 Git 状态**
   - 运行 `git status` 检查修改状态
   - 运行 `git branch --show-current` 获取当前分支

3. **决定执行模式**
   - 根据上述逻辑决定执行哪个模式
   - 向用户明确说明将要执行的操作

4. **初始化 TodoList**
   - 根据选定模式创建对应的任务列表

---

## Mode 1: Commit Only (仅提交模式)

### 适用场景

- 工作目录有未提交的修改
- 用户未请求创建 PR
- 需要创建或切换到功能分支

### 执行流程

#### 第一阶段：状态检查

1. **检查本地修改**
   - 如果没有修改，提示用户并直接返回
   - 如果有修改，继续下一步

2. **分支安全检查**
   - 如果在 `main` 或 `master` 分支：
     - ⚠️ **警告**：不建议直接提交到主分支
     - **询问用户**选择以下操作：
       - 选项 1：创建新的 Issue 和分支（推荐）
       - 选项 2：提供现有的 Issue ID
       - 选项 3：强制提交到 main（需通过严格验证）

#### 第二阶段：Issue 和分支管理

3. **Issue 处理**
   - **场景 A - 创建新 Issue**：
     - 分析代码变更内容
     - 使用 `gh issue create` 创建 Issue
     - 记录 Issue ID

   - **场景 B - 使用现有 Issue**：
     - 验证 Issue ID 有效性：`gh issue view <issue-id>`
     - 记录 Issue ID

   - **场景 C - 强制提交到 main**：
     - 跳过 Issue 创建，进入验证流程

4. **分支创建**（如果不在 main）
   - 基于 Issue ID 创建分支：`git checkout -b <type>/<issue-id>-<description>`
   - 分支命名遵循规范：`<type>/<issue-id>-<short-description>`

#### 第三阶段：提交前质量检查（智能执行）

5. **增量预检**（所有分支）

   **跳过条件**（同时满足才可跳过）：
   - 本次会话已完成一次增量预检
   - 自上次预检后，仅修改了以下类型文件：
     - 文档文件（`*.md`）
     - 注释（单纯的注释修改，无代码变更）
     - `.gitignore`、`LICENSE` 等非代码文件

   **必须执行**（任一条件满足）：
   - 本次会话未完成增量预检
   - 修改了任何代码文件（`.ts`、`.tsx`、`.js`、`.jsx` 等）
   - 修改了配置文件（`package.json`、`tsconfig.json`、`.env.*` 等）
   - 修改了样式文件（`.css`、`.scss` 等）

   **执行内容**：
   - `./scripts/dx lint` —— 所有代码改动必跑
   - `./scripts/dx build backend` —— 仅当后端代码或共享逻辑被后端使用时执行
   - `./scripts/dx build sdk --online` —— 仅当后端 DTO/API（如 controller、dto）发生变更时执行，需紧随 backend 之后
   - `./scripts/dx build front` —— 仅当用户端前端代码有改动时执行
   - `./scripts/dx build admin` —— 仅当管理后台代码有改动时执行

   - 任一命令失败则终止，不允许提交；修复后重新执行对应命令

6. **Main 分支 E2E 测试**（仅 main 分支）
   - 如果目标是 main 分支，额外执行：
     - 分析变更影响的模块/功能
     - 识别受影响的 E2E 测试用例
     - 逐个运行受影响的测试
     - 所有测试必须通过，否则禁止提交

#### 第四阶段：提交前最终检查

7. **检查 OpenAPI 文件**（增量预检执行过 `./scripts/dx build sdk --online` 时）
   - 检查 `apps/sdk/openapi/openapi.json` 是否有变更
   - 使用 `git diff --name-only` 检查是否有后端 DTO/接口变更：
     - `apps/backend/src/**/*.dto.ts`
     - `apps/backend/src/**/*.controller.ts`
   - **如果没有后端 DTO/接口变更，但有 openapi.json 变更**：
     - ⚠️ 警告：检测到 openapi.json 变更但无对应的后端 DTO/接口修改
     - 自动执行：`git restore apps/sdk/openapi/openapi.json`
     - 说明：该文件由 `./scripts/dx build sdk --online` 自动生成，仅在后端接口/DTO 变更时才需要提交

#### 第五阶段：提交执行

8. **生成智能提交信息**
   - 分析代码变更（`git diff --staged` 或 `git diff`）
   - 查看最近的提交历史：`git log --oneline -10`
   - 生成符合 Conventional Commits 的提交信息：
     - `feat:` - 新功能
     - `fix:` - 修复 bug
     - `docs:` - 文档更新
     - `refactor:` - 重构
     - `test:` - 测试相关
     - `chore:` - 构建/工具相关

9. **执行提交**
   - 暂存所有修改：`git add -A`
   - 使用 heredoc 格式提交（必须关联 Issue）：

   ```bash
   git commit -F - <<'MSG'
   <type>: <简短描述>

   变更说明：
   - <具体变更点1>
   - <具体变更点2>

   Refs: #<issue-id>
   MSG
   ```

10. **推送到远程**

- 首次推送：`git push -u origin <branch-name>`
- 后续推送：`git push`

#### 第六阶段：Issue 关联和报告

11. **在 Issue 下评论提交信息**

```bash
gh issue comment <issue-id> --body-file - <<'MSG'
📝 代码已提交

**提交信息：**
- Commit Hash: <hash>
- 分支: <branch-name>

**变更内容：**
- <变更说明1>
- <变更说明2>

**状态：** 已推送到远程仓库
MSG
```

---

## Mode 2: PR Only (仅 PR 模式)

### 适用场景

- 工作目录干净（无未提交修改）
- 当前在功能分支（非 main/master）
- 用户使用了 `--pr` 标志
- 已有提交但未创建 PR

### 执行流程

#### 第一阶段：前置检查

1. **检查本地修改**
   - **如果有未提交的修改**：
     - ❌ **终止流程**
     - 提示用户：`检测到未提交的修改，请先提交或使用 --all`
     - 列出未提交的文件
     - 退出命令

2. **分支验证**
   - **如果在 main 或 master 分支**：
     - ❌ **错误**：不能从主分支创建 PR
     - 提示：请先创建功能分支并提交代码
     - 退出命令

3. **检查远程推送状态**
   - 运行 `git status -sb` 检查是否有未推送的提交
   - 如有未推送提交，先执行 `git push`

4. **获取基础分支**
   - 如果用户提供 `--base`，使用指定分支
   - 否则默认使用 `main`

#### 第二阶段：Issue 处理

5. **获取 Issue ID**
   - **优先级 1**：使用 `--issue` 参数提供的 Issue ID
   - **优先级 2**：从当前分支名提取 Issue ID
     - 分支格式：`<type>/<issue-id>-<description>`
     - 提取：`feat/123-add-auth` → Issue #123
   - **优先级 3**：从最近的 commit message 提取 Issue ID
     - 查找 `Refs: #123` 或 `Closes: #123`
   - **如果以上都找不到**：
     - 询问用户：`未找到关联的 Issue ID，请提供 Issue 编号`

6. **验证 Issue**
   - 使用 `gh issue view <issue-id>` 验证 Issue 存在
   - 读取 Issue 标题和描述，用于生成 PR 内容
   - 如 Issue 不存在，提示用户并退出

#### 第三阶段：提交前质量检查（智能执行）

7. **增量预检**

   **跳过条件**（同时满足才可跳过）：
   - 本次会话已完成一次增量预检
   - 自上次预检后，仅修改了以下类型文件：
     - 文档文件（`*.md`）
     - 注释（单纯的注释修改，无代码变更）
     - `.gitignore`、`LICENSE` 等非代码文件

   **必须执行**（任一条件满足）：
   - 本次会话未完成增量预检
   - 修改了任何代码文件（`.ts`、`.tsx`、`.js`、`.jsx` 等）
   - 修改了配置文件（`package.json`、`tsconfig.json`、`.env.*` 等）
   - 修改了样式文件（`.css`、`.scss` 等）

   **执行内容**：
   - `./scripts/dx lint` —— 所有代码改动必跑
   - `./scripts/dx build backend` —— 仅当后端代码或共享逻辑被后端使用时执行
   - `./scripts/dx build sdk --online` —— 仅当后端 DTO/API（如 controller、dto）发生变更时执行，需紧随 backend 之后
   - `./scripts/dx build front` —— 仅当用户端前端代码有改动时执行
   - `./scripts/dx build admin` —— 仅当管理后台代码有改动时执行

   - 任一命令失败则终止，不允许创建 PR；修复后重新执行对应命令

8. **执行后端 E2E 测试**（如有后端变更则强制执行）
   - **分析受影响的模块**：
     - 检查代码变更：`git diff <base-branch>...HEAD --name-only`
     - 识别后端文件变更（`apps/backend/src/` 下的文件）
     - 根据变更的模块定位对应的 E2E 测试文件

   - **识别受影响的测试用例**：
     - 如变更涉及 `modules/auth/` → 运行 `apps/backend/e2e/auth.e2e-spec.ts`
     - 如变更涉及 `modules/user/` → 运行 `apps/backend/e2e/user.e2e-spec.ts`

   - **执行测试**：

     ```bash
     ./scripts/dx test e2e backend <test-file>
     ```

   - **失败处理**：
     - 任一测试失败：
       - ❌ **阻止 PR 创建**
       - 展示失败的测试用例
       - 提示：修复测试或代码后重试
       - 退出命令

   - **无后端变更处理**：
     - 如果没有后端代码变更，跳过 E2E 测试
     - 在 PR 描述中注明：`无后端变更，已跳过 E2E 测试`

#### 第四阶段：生成 PR 内容

9. **生成 PR 标题**
   - **基于 Issue 和代码变更自动生成**
   - 从 Issue 标题提取主要内容
   - 从 commit messages 分析变更类型
   - 生成格式：`<type>: <简短描述>` 或 `<emoji> <type>: <简短描述>`

10. **生成 PR 描述**
    - 分析所有提交信息：`git log <base-branch>..HEAD`
    - 获取代码变更统计：`git diff <base-branch>...HEAD --stat`
    - 读取 Issue 内容作为上下文
    - 生成结构化描述：

    ```markdown
    ## 📋 变更概述

    [基于 Issue 和 commits 生成的概述，2-3 句话]

    ## 🔗 关联 Issue

    Closes: #<issue-id>

    ## 💡 变更内容

    - <变更点1>
    - <变更点2>
    - <变更点3>

    ## 📦 影响范围

    **后端：** [变更的模块]
    **前端：** [变更的模块]
    **数据库：** [Schema 变更说明]
    **API：** [接口变更摘要]

    ## ✅ 质量验证

    - [x] PR 预检通过 (lint + 构建)
    - [x] E2E 测试通过 ([X/X] 用例)

    ## 📊 代码统计

    [变更文件数、新增/删除行数]

    ---

    🤖 Generated with [Claude Code](https://claude.com/claude-code)
    ```

#### 第五阶段：创建 PR

11. **执行 PR 创建**

    ```bash
    gh pr create \
      --base <base-branch> \
      --head <current-branch> \
      --title "<auto-generated-title>" \
      --body-file - <<'EOF'
    [生成的 PR 描述]
    EOF
    ```

12. **添加标签**
    - 基于变更类型自动添加：
      - `feature` - 新功能
      - `bug` - Bug 修复
      - `refactor` - 重构
      - `backend` - 后端变更
      - `frontend` - 前端变更
      - `database` - 数据库变更

13. **在 Issue 下评论**

    ```bash
    gh issue comment <issue-id> --body-file - <<'MSG'
    🔀 PR 已创建

    PR #<pr-number>: <pr-title>
    链接: <pr-url>

    质量验证：
    - ✅ PR 预检通过
    - ✅ E2E 测试通过 (<X>/<X> 用例)

    请 review。
    MSG
    ```

14. **输出 PR 信息**

---

## Mode 3: Commit + PR (组合模式)

### 适用场景

- 用户使用了 `--all` 标志
- 工作目录有未提交的修改
- 用户希望一次性完成提交和 PR 创建

### 执行流程

**按顺序执行 Mode 1 和 Mode 2：**

1. **执行 Commit 模式**
   - 完整执行 Mode 1 的所有步骤
   - 确保代码已提交并推送到远程

2. **检查 Commit 是否成功**
   - 如果 Commit 失败，终止流程
   - 如果 Commit 成功，继续下一步

3. **执行 PR 模式**
   - 完整执行 Mode 2 的所有步骤
   - 基于刚才的提交创建 PR

4. **统一输出报告**

   ```
   ## ✅ Commit + PR 完成

   ### 提交信息
   - Commit Hash: <hash>
   - 分支: <branch-name>
   - Issue: #<issue-id>

   ### PR 信息
   - PR #<pr-number>: <title>
   - URL: <pr-url>
   - 状态: Open
   - 质量验证: ✅ 全部通过

   工作流程已完成！
   ```

---

## Output Format

### 1. 模式选择报告

```
## 🎯 工作流模式选择

检测到的状态：
- 工作目录: [干净/有修改]
- 当前分支: <branch-name>
- 用户标志: [--pr / --all / 无]

选定模式: [仅 Commit / 仅 PR / Commit + PR]

将要执行的操作：
- [ ] <操作1>
- [ ] <操作2>
- [ ] <操作3>
```

### 2. Commit 模式输出

参考 Mode 1 中的输出格式

### 3. PR 模式输出

参考 Mode 2 中的输出格式

### 4. Commit + PR 模式输出

```
## ✅ Commit + PR 流程完成

### 📝 Commit 阶段
- Commit Hash: 54c758e1
- 分支: feat/879-add-feature
- Issue: #879
- 状态: ✅ 已推送

### 🔀 PR 阶段
- PR #123: ✨ feat: 添加新功能
- URL: https://github.com/owner/repo/pull/123
- 基础分支: main
- 质量验证: ✅ 全部通过
- 标签: feature, backend

### 📊 总结
- 提交数: 3 commits
- 变更文件: 12 files
- 新增行数: +487
- 删除行数: -123

工作流程已完成！🎉
```

---

## Key Constraints

### Git 操作规范

- 所有命令必须从仓库根目录执行
- 提交信息必须使用 heredoc 格式（`git commit -F -`）
- PR 描述必须使用 heredoc 格式（`gh pr create --body-file -`）
- 必须使用 GH CLI 与 SSH 推送
- 禁止在 `-m`/`-b` 参数中使用字面量 `\n`

### 分支保护规则

- **Main 分支**：
  - 默认禁止直接提交
  - 不能从 main 创建 PR
  - 如强制提交，必须完成增量预检（lint + 按改动执行构建 + 必要时 `./scripts/dx build sdk --online`）并通过所有受影响的 E2E 测试
- **功能分支**：
  - 命名规范：`<type>/<issue-id>-<description>`
  - 必须关联 Issue ID

### Issue 关联规则

- **所有分支**（包括 main）的每次提交都必须关联 Issue
- 如无 Issue，必须创建或询问用户指定
- 提交后必须在 Issue 下评论提交哈希
- PR 必须使用 `Closes: #<id>` 或 `Refs: #<id>` 关联 Issue

### 质量门禁

- ✅ 增量预检：已执行且仅改文档/注释可跳过，涉及代码改动必须重新执行
- ✅ 任一步检查失败则终止，不允许提交
- ✅ 受影响的 E2E 测试必须通过（PR 模式 + Main 分支提交）
- ✅ openapi.json 检查在执行 `./scripts/dx build sdk --online` 后、提交前完成

### 提交信息规范

- 遵循 Conventional Commits 格式
- 必须包含变更说明
- 使用 `Refs: #<issue-id>` 或 `Closes: #<issue-id>`
- 所有内容使用中文

---

## Success Criteria

- ✅ **质量优先**：增量预检（lint + 按改动构建 + 必要时 `./scripts/dx build sdk --online`）
- ✅ **OpenAPI 检查**：执行 `./scripts/dx build sdk --online` 后自动清理无需提交的 openapi.json 变更
- ✅ **智能模式选择**：正确识别执行模式
- ✅ **状态验证**：正确识别分支和修改状态
- ✅ **分支安全**：避免意外提交到 main 分支
- ✅ **Issue 关联**：所有提交和 PR 正确关联 Issue
- ✅ **完整测试**：E2E 测试在必要时执行（PR 模式 + Main 分支提交）
- ✅ **信息规范**：提交信息和 PR 描述符合规范
- ✅ **完整追踪**：Issue 下有完整的提交和 PR 记录
- ✅ **流程连贯**：Commit + PR 模式无缝衔接

---

## Examples

### 示例 1：自动模式（有未提交修改）

```bash
/git-commit-and-pr

→ 检测到未提交的修改
→ 选定模式: 仅 Commit
→ 执行提交流程...
→ ✅ 提交完成
```

### 示例 2：自动模式（工作目录干净）

```bash
/git-commit-and-pr

→ 工作目录干净，无需提交
→ 提示: 使用 --pr 创建 Pull Request
```

### 示例 3：仅创建 PR

```bash
/git-commit-and-pr --pr --issue 123

→ 选定模式: 仅 PR
→ 执行质量验证...
→ 生成 PR 内容...
→ ✅ PR #456 创建成功
```

### 示例 4：组合模式

```bash
/git-commit-and-pr --all --issue 123

→ 选定模式: Commit + PR
→ 阶段 1: 执行提交...
→ ✅ 提交完成
→ 阶段 2: 创建 PR...
→ ✅ PR 创建成功
→ 🎉 工作流程完成！
```

### 示例 5：从 main 分支开始

```bash
# 当前在 main 分支，有未提交修改
/git-commit-and-pr

→ ⚠️ 警告：当前在主分支 (main)
→ 建议：创建功能分支
→ 是否创建新 Issue 和分支？(y/n)
```

---

智能化处理 Git 提交和 PR 创建，确保代码质量和规范性！🚀
