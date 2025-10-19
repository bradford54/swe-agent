# Git 与 GitHub 规范

## Git 与 Issue 强制规则

- 🔗 **Issue ID 必需**：提交前必须有 Issue ID；若无则询问用户创建或指定
- 🚨 **Issue 分支强制**：修改代码前必须检查当前分支
  - ✅ 允许：在 `feat/<issue-id>-*`、`fix/<issue-id>-*`、`refactor/<issue-id>-*` 等 issue 分支上修改
  - ❌ 禁止：在 `main`、`master` 等主分支上修改代码
  - 📋 处理流程：
    1. 检测到需要修改代码时，先执行 `git branch --show-current` 检查当前分支
    2. 如果在主分支,询问用户提供 Issue ID 或使用 `/git-create-issue` 创建
    3. 获取 Issue ID 后，创建对应分支：`git checkout -b <type>/<issue-id>-<description>`
    4. 切换到 issue 分支后再执行代码修改
    5. 如果用户拒绝创建分支，则拒绝修改代码并说明原因
- 📝 **Heredoc 格式**：Git 提交与 GitHub CLI 必须使用 heredoc（见下方示例）
- 🚫 **禁止 `\n` 换行**：在命令参数中写 `\n` 只会产生字面量，不会换行
- 📌 **推送后评论**：推送后必须在对应 Issue 评论报告修改并关联 commit hash
- 🔑 **统一 SSH 认证**：Git 远程和 GitHub CLI 操作统一使用 SSH key 认证

## 提交格式

- 使用 Conventional Commits：`feat:`/`fix:`/`docs:`/`refactor:` 等
- 末尾添加：`Refs: #123` 或 `Closes: #123`

## Heredoc 使用（强制）

### Git 提交

```bash
git commit -F - <<'MSG'
feat: 功能摘要

变更说明：
- 具体变更点1
- 具体变更点2

Refs: #123
MSG
```

### GitHub CLI - PR 创建

```bash
gh pr create --body-file - <<'MSG'
## 变更说明
- 具体变更点1
- 具体变更点2

close: #123
MSG
```

### GitHub CLI - Issue 评论

```bash
gh issue comment 123 --body-file - <<'MSG'
问题分析：
- 原因1
- 原因2
MSG
```

### GitHub CLI - PR Review

```bash
gh pr review 123 --comment --body-file - <<'MSG'
代码审查意见：
- 建议1
- 建议2
MSG
```
