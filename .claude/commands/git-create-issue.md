---
allowed-tools: [Bash, Read, Glob, TodoWrite, Edit, Grep]
description: '根据上下文自动整理信息并创建标准化 GitHub Issue'
---

## Usage

```bash
# 按当前讨论与改动生成 Issue
/git-create-issue [--title <TITLE>] [--labels <label1,label2>] [--assignees <ASSIGNEES>]
```

## Context

- 汇总当前对话中的问题描述、需求背景与已完成的代码改动
- 自动分析 `git status`、`git diff --stat` 与相关文件内容
- 对照仓库规范生成 Issue 标题、复现步骤、预期行为与计划方案
- 确保 Issue 能为后续提交流程提供唯一 ID 支撑

## Your Role

你是 **Issue 创建协调者**，负责调用 `issue-creator` agent 自动生成标准化的 GitHub Issue。

## Process

使用 Task tool 调用 issue-creator agent 执行 Issue 创建：

```
Use Task tool with issue-creator agent:
"请根据当前对话上下文和代码变更创建 GitHub Issue

用户参数：
- 标题: [如果用户提供了 --title]
- 标签: [如果用户提供了 --labels]
- 指派: [如果用户提供了 --assignees]

分析重点：
- 从对话历史中提取问题描述和需求背景
- 分析代码变更（git status, git diff）
- 识别受影响的模块和范围
- 检查是否存在重复 Issue

输出要求：
- 生成结构化的 Issue 内容（背景、现状、期望、计划、影响）
- 使用 gh CLI 创建 Issue
- 返回 Issue 编号和链接供后续引用
"
```

## Delegation Strategy

**issue-creator agent 负责**：

- 自动分析代码变更（git status, git diff）
- 从对话历史提取问题描述和需求
- 生成结构化的 Issue 内容（背景、现状、期望、计划、影响）
- 选择合适的标签和模块分类
- 使用 gh CLI heredoc 格式创建 Issue
- 输出 Issue 编号和链接

**command 负责**：

- 解析用户提供的参数（--title, --labels, --assignees）
- 将参数传递给 issue-creator agent
- 接收并展示 agent 返回的 Issue 信息

## Output Format

```
✅ Issue 创建成功

Issue: #<编号>
标题: <标题>
链接: <GitHub URL>
标签: <标签列表>

后续动作：
- [ ] 在提交中使用 Refs: #<编号> 关联
- [ ] 在 PR 中使用 Closes: #<编号> 自动关闭
```

## Success Criteria

- ✅ **Issue 创建成功** - 返回有效的 Issue 编号和链接
- ✅ **内容结构完整** - 包含背景、现状、期望、计划、影响
- ✅ **规范一致** - 遵循仓库规范（中文、heredoc、标签准确）
- ✅ **可追踪** - 提供 Issue ID 供后续 commit/PR 引用

## Notes

- 所有详细的 Issue 创建逻辑由 `issue-creator` agent 处理
- Command 层保持简洁，专注于参数传递和结果展示
- Agent 层负责复杂的分析、生成和执行逻辑

通过 `/git-create-issue` command 调用 `issue-creator` agent，可以让讨论内容与代码改动快速沉淀为可执行的 Issue，确保后续提交/PR 有据可依。
