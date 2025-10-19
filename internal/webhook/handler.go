package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cexll/swe/internal/github"
	"github.com/cexll/swe/internal/taskstore"
)

// Task represents a pilot task to be executed
type Task struct {
	ID            string
	Repo          string
	Number        int
	Branch        string
	Prompt        string
	PromptSummary string
	IssueTitle    string
	IssueBody     string
	IsPR          bool
	PRBranch      string // PR's source branch (if it's a PR)
	PRState       string // PR state: "open" or "closed"
	Username      string // User who triggered the task
	Attempt       int    // Current attempt number (managed by dispatcher)
	PromptContext map[string]string
}

// TaskIDComponents 封装 Task ID 组成部分（支持可选字段）
type TaskIDComponents struct {
	Repo        string
	IssueNumber *int // 可选：关联的 Issue 编号
	PRNumber    *int // 可选：PR 编号
	Timestamp   int64
}

// TaskDispatcher enqueues tasks for asynchronous execution
type TaskDispatcher interface {
	Enqueue(task *Task) error
}

// GitHubClient 封装 GitHub API 调用（用于查询 PR 关联的 Issue）
type GitHubClient struct {
	authProvider github.AuthProvider
}

// Handler handles GitHub webhook events
type Handler struct {
	webhookSecret  string
	triggerKeyword string
	dispatcher     TaskDispatcher
	issueDeduper   *commentDeduper
	reviewDeduper  *commentDeduper
	store          *taskstore.Store
	appAuth        github.AuthProvider
	githubClient   *GitHubClient // GitHub API 客户端（用于查询 PR 关联 Issue）
}

// NewHandler creates a new webhook handler
func NewHandler(webhookSecret, triggerKeyword string, dispatcher TaskDispatcher, store *taskstore.Store, appAuth github.AuthProvider) *Handler {
	var client *GitHubClient
	if appAuth != nil {
		client = &GitHubClient{authProvider: appAuth}
		log.Println("GitHub client initialized for Task ID enrichment")
	}

	return &Handler{
		webhookSecret:  webhookSecret,
		triggerKeyword: triggerKeyword,
		dispatcher:     dispatcher,
		issueDeduper:   newCommentDeduper(12 * time.Hour),
		reviewDeduper:  newCommentDeduper(12 * time.Hour),
		store:          store,
		appAuth:        appAuth,
		githubClient:   client,
	}
}

// Handle handles GitHub webhook events (issue comments, review comments, etc.)
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	// 1. Read payload
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading payload: %v", err)
		http.Error(w, "Error reading payload", http.StatusBadRequest)
		return
	}

	// 2. Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if err := ValidateSignatureHeader(signature); err != nil {
		log.Printf("Invalid signature header: %v", err)
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	if !VerifySignature(payload, signature, h.webhookSecret) {
		log.Printf("Signature verification failed")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// 3. Determine event type
	eventType := r.Header.Get("X-GitHub-Event")
	switch eventType {
	case "issue_comment":
		h.handleIssueComment(w, payload)
	case "pull_request_review_comment":
		h.handleReviewComment(w, payload)
	default:
		log.Printf("Ignoring unsupported event type: %s", eventType)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Event ignored"))
	}
}

func (h *Handler) handleIssueComment(w http.ResponseWriter, payload []byte) {
	// Parse event
	var event IssueCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		log.Printf("Error parsing event: %v", err)
		http.Error(w, "Error parsing event", http.StatusBadRequest)
		return
	}

	// Only handle newly created comments
	if event.Action != "created" {
		log.Printf("Ignoring issue_comment action: %s", event.Action)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Issue comment action ignored"))
		return
	}

	// 4. Check if comment is from a bot (prevent infinite loops)
	if event.Comment.User.Type == "Bot" {
		log.Printf("Ignoring comment from bot: %s", event.Comment.User.Login)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Bot comment ignored"))
		return
	}

	// 5. Check if comment contains trigger keyword
	if !strings.Contains(event.Comment.Body, h.triggerKeyword) {
		log.Printf("Comment does not contain trigger keyword '%s'", h.triggerKeyword)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("No trigger keyword found"))
		return
	}

	// 5.1 Verify permission: check if user is the app installer
	if !h.verifyPermission(event.Repository.FullName, event.Comment.User.Login) {
		log.Printf("Permission denied: user %s is not the app installer", event.Comment.User.Login)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Permission denied"))
		return
	}

	// 5.5 Prevent duplicate processing for the same comment ID
	if !h.issueDeduper.markIfNew(event.Comment.ID) {
		log.Printf("Ignoring duplicate issue comment: id=%d", event.Comment.ID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Duplicate comment ignored"))
		return
	}

	// 6. Extract prompt from comment
	customInstruction, found := extractPrompt(event.Comment.Body, h.triggerKeyword)
	if !found {
		log.Printf("No prompt found after trigger keyword")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("No prompt found"))
		return
	}

	// 7. Check if this is a PR or issue
	isPR := event.Issue.PullRequest != nil

	prompt := buildPrompt(event.Issue.Title, event.Issue.Body, customInstruction)
	promptSummary := buildPromptSummary(event.Issue.Title, customInstruction, isPR)

	// 8. 构建 Task ID 组件（分层策略）
	components := TaskIDComponents{
		Repo:      event.Repository.FullName,
		Timestamp: time.Now().UnixNano(),
	}

	if isPR {
		// PR 评论：先生成 PR-only ID，Best-Effort 查询关联 Issue
		components.PRNumber = &event.Issue.Number

		// 尝试查询关联 Issue（2s 超时）
		if h.githubClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			if issueNum, err := h.githubClient.GetLinkedIssue(ctx, components.Repo, event.Issue.Number); err == nil && issueNum != nil {
				components.IssueNumber = issueNum
				log.Printf("Task ID enrichment: Found linked issue #%d for PR #%d", *issueNum, event.Issue.Number)
			} else if err != nil {
				log.Printf("Warning: Failed to fetch linked issue for PR #%d: %v (continuing with PR-only ID)", event.Issue.Number, err)
			}
		}
	} else {
		// Issue 评论：直接使用 Issue 号
		components.IssueNumber = &event.Issue.Number
	}

	// 9. Create task
	task := &Task{
		ID:            h.generateTaskID(components),
		Repo:          event.Repository.FullName,
		Number:        event.Issue.Number,
		Branch:        event.Repository.DefaultBranch,
		Prompt:        prompt,
		PromptSummary: promptSummary,
		IssueTitle:    event.Issue.Title,
		IssueBody:     event.Issue.Body,
		IsPR:          isPR,
		Username:      event.Comment.User.Login,
		PromptContext: buildPromptContextForIssue(event, h.triggerKeyword, isPR),
	}

	h.createStoreTask(task)

	// No extra execution mode hints: keep KISS and rely on latest trigger comment

	log.Printf("Received task: repo=%s, number=%d, commentID=%d, user=%s", task.Repo, task.Number, event.Comment.ID, task.Username)

	h.enqueueTask(w, task, prompt)
}

func (h *Handler) handleReviewComment(w http.ResponseWriter, payload []byte) {
	var event PullRequestReviewCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		log.Printf("Error parsing review comment event: %v", err)
		http.Error(w, "Error parsing event", http.StatusBadRequest)
		return
	}

	// Only handle newly created review comments
	if event.Action != "created" {
		log.Printf("Ignoring pull_request_review_comment action: %s", event.Action)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Review comment action ignored"))
		return
	}

	// Ignore bot comments
	if event.Comment.User.Type == "Bot" {
		log.Printf("Ignoring review comment from bot: %s", event.Comment.User.Login)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Bot comment ignored"))
		return
	}

	// Check trigger keyword
	if !strings.Contains(event.Comment.Body, h.triggerKeyword) {
		log.Printf("Review comment does not contain trigger keyword '%s'", h.triggerKeyword)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("No trigger keyword found"))
		return
	}

	// Verify permission: check if user is the app installer
	if !h.verifyPermission(event.Repository.FullName, event.Comment.User.Login) {
		log.Printf("Permission denied: user %s is not the app installer", event.Comment.User.Login)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Permission denied"))
		return
	}

	if !h.reviewDeduper.markIfNew(event.Comment.ID) {
		log.Printf("Ignoring duplicate review comment: id=%d", event.Comment.ID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Duplicate comment ignored"))
		return
	}

	customInstruction, found := extractPrompt(event.Comment.Body, h.triggerKeyword)
	if !found {
		log.Printf("No prompt found after trigger keyword in review comment")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("No prompt found"))
		return
	}

	prompt := buildPrompt(event.PullRequest.Title, event.PullRequest.Body, customInstruction)
	promptSummary := buildPromptSummary(event.PullRequest.Title, customInstruction, true)

	branch := event.PullRequest.Base.Ref
	if branch == "" {
		branch = event.Repository.DefaultBranch
	}

	// 构建 Task ID 组件（PR review 一定有 PR）
	components := TaskIDComponents{
		Repo:      event.Repository.FullName,
		PRNumber:  &event.PullRequest.Number,
		Timestamp: time.Now().UnixNano(),
	}

	// Best-Effort: 查询关联 Issue（2s 超时）
	if h.githubClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if issueNum, err := h.githubClient.GetLinkedIssue(ctx, components.Repo, event.PullRequest.Number); err == nil && issueNum != nil {
			components.IssueNumber = issueNum
			log.Printf("Task ID enrichment: Found linked issue #%d for PR #%d", *issueNum, event.PullRequest.Number)
		} else if err != nil {
			log.Printf("Warning: Failed to fetch linked issue for PR #%d: %v (continuing with PR-only ID)", event.PullRequest.Number, err)
		}
	}

	task := &Task{
		ID:            h.generateTaskID(components),
		Repo:          event.Repository.FullName,
		Number:        event.PullRequest.Number,
		Branch:        branch,
		Prompt:        prompt,
		PromptSummary: promptSummary,
		IssueTitle:    event.PullRequest.Title,
		IssueBody:     event.PullRequest.Body,
		IsPR:          true,
		PRBranch:      event.PullRequest.Head.Ref,
		PRState:       event.PullRequest.State,
		Username:      event.Comment.User.Login,
		PromptContext: buildPromptContextForReview(event, h.triggerKeyword),
	}

	h.createStoreTask(task)

	// No execution mode injection to avoid over-design

	log.Printf("Received review task: repo=%s, number=%d, commentID=%d, user=%s", task.Repo, task.Number, event.Comment.ID, task.Username)

	h.enqueueTask(w, task, prompt)
}

func (h *Handler) generateTaskID(components TaskIDComponents) string {
	sanitized := strings.ReplaceAll(components.Repo, "/", "-")

	var parts []string
	parts = append(parts, sanitized)

	// 按优先级添加段落：issue -> pr -> timestamp
	if components.IssueNumber != nil {
		parts = append(parts, fmt.Sprintf("issue-%d", *components.IssueNumber))
	}

	if components.PRNumber != nil {
		parts = append(parts, fmt.Sprintf("pr-%d", *components.PRNumber))
	}

	parts = append(parts, fmt.Sprintf("%d", components.Timestamp))

	return strings.Join(parts, "-")
}

// verifyPermission checks if the user has permission to trigger tasks
// Returns true if user has write permission to the repository
func (h *Handler) verifyPermission(repo, username string) bool {
	// Allow override via environment for development or lenient deployments
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_ALL_USERS")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("PERMISSION_MODE")), "open") {
		log.Printf("Permission override enabled via env (ALLOW_ALL_USERS/PERMISSION_MODE), allowing user %s", username)
		return true
	}

	if h.appAuth == nil {
		// No auth provider, allow all (for testing)
		log.Printf("Warning: No app auth provider configured, allowing all users")
		return true
	}

	// Check if user has write permission to the repository
	hasPermission, err := h.appAuth.CheckUserPermission(repo, username)
	if err != nil {
		log.Printf("Warning: Failed to check user permission: %v (allowing request)", err)
		// On error, allow the request (fail-open for robustness)
		return true
	}

	if !hasPermission {
		log.Printf("Permission check failed: user=%s does not have write permission to repo=%s", username, repo)
		return false
	}

	log.Printf("Permission check passed: user=%s has write permission to repo=%s", username, repo)
	return true
}

func (h *Handler) createStoreTask(task *Task) {
	if h.store == nil {
		return
	}

	owner, name := splitRepo(task.Repo)
	storeTask := &taskstore.Task{
		ID:          task.ID,
		Title:       task.IssueTitle,
		Status:      taskstore.StatusPending,
		RepoOwner:   owner,
		RepoName:    name,
		IssueNumber: task.Number,
		Actor:       task.Username,
	}
	if err := h.store.Create(storeTask); err != nil {
		log.Printf("Failed to create task in store: %v", err)
		return
	}
	h.store.AddLog(task.ID, "info", "Task queued")
}

func splitRepo(full string) (string, string) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return full, ""
}

func (h *Handler) enqueueTask(w http.ResponseWriter, task *Task, prompt string) {
	if err := h.dispatcher.Enqueue(task); err != nil {
		log.Printf("Failed to enqueue task: %v", err)
		switch {
		case errors.Is(err, ErrQueueFull):
			http.Error(w, "Task queue is busy, try again later", http.StatusServiceUnavailable)
		case errors.Is(err, ErrQueueClosed):
			http.Error(w, "Task queue unavailable", http.StatusServiceUnavailable)
		default:
			http.Error(w, "Failed to enqueue task", http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Task queued"))
}

// extractPrompt extracts the prompt text after the trigger keyword.
// Returns the trimmed user instruction and a boolean indicating whether the trigger was found.
func extractPrompt(body, triggerKeyword string) (string, bool) {
	// Find the trigger keyword
	idx := strings.Index(body, triggerKeyword)
	if idx == -1 {
		return "", false
	}

	// Get text after trigger keyword
	remaining := strings.TrimSpace(body[idx+len(triggerKeyword):])

	return remaining, true
}

// KISS: no execution mode classifier; resolve via prompt design only

// buildPrompt builds the final prompt by treating the trigger instruction as the primary directive
// and including the issue/PR content as contextual reference.
func buildPrompt(title, body, userInstruction string) string {
	instruction := strings.TrimSpace(userInstruction)
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)

	var builder strings.Builder

	if instruction != "" {
		builder.WriteString(instruction)
	}

	if title != "" || body != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n\n---\n\n")
		}
		builder.WriteString("# Issue Context")
		if title != "" {
			builder.WriteString("\n\n## Title\n")
			builder.WriteString(title)
		}
		if body != "" {
			builder.WriteString("\n\n## Body\n")
			builder.WriteString(body)
		}
	}

	return builder.String()
}

func buildPromptSummary(title, userInstruction string, isPR bool) string {
	title = strings.TrimSpace(title)
	instruction := summarizeInstruction(userInstruction, 180)

	var builder strings.Builder
	if title != "" {
		if isPR {
			builder.WriteString("**PR:** ")
		} else {
			builder.WriteString("**Issue:** ")
		}
		builder.WriteString(title)
	}

	if instruction != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("**Instruction:**\n")
		builder.WriteString(instruction)
	}

	return builder.String()
}

func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || text == "" {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	truncated := strings.TrimSpace(string(runes[:limit]))
	return truncated + "…"
}

func summarizeInstruction(instruction string, limit int) string {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return ""
	}

	lines := strings.Split(instruction, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}

	if len(parts) == 0 {
		return ""
	}

	joined := strings.Join(parts, " ")
	return truncateText(joined, limit)
}

func buildPromptContextForIssue(event IssueCommentEvent, trigger string, isPR bool) map[string]string {
	context := map[string]string{
		"issue_title":          event.Issue.Title,
		"issue_body":           event.Issue.Body,
		"event_name":           "issue_comment",
		"event_type":           "GENERAL_COMMENT",
		"trigger_phrase":       trigger,
		"trigger_username":     event.Comment.User.Login,
		"trigger_display_name": event.Comment.User.Login,
		"trigger_comment":      event.Comment.Body,
		"trigger_context":      fmt.Sprintf("issue comment with '%s'", trigger),
		"repository":           event.Repository.FullName,
		"base_branch":          event.Repository.DefaultBranch,
		"is_pr":                strconv.FormatBool(isPR),
		"issue_number":         strconv.Itoa(event.Issue.Number),
	}

	if isPR {
		context["pr_number"] = strconv.Itoa(event.Issue.Number)
	}

	return context
}

func buildPromptContextForReview(event PullRequestReviewCommentEvent, trigger string) map[string]string {
	branch := event.PullRequest.Base.Ref
	if branch == "" {
		branch = event.Repository.DefaultBranch
	}

	return map[string]string{
		"issue_title":          event.PullRequest.Title,
		"issue_body":           event.PullRequest.Body,
		"event_name":           "pull_request_review_comment",
		"event_type":           "REVIEW_COMMENT",
		"trigger_phrase":       trigger,
		"trigger_username":     event.Comment.User.Login,
		"trigger_display_name": event.Comment.User.Login,
		"trigger_comment":      event.Comment.Body,
		"trigger_context":      fmt.Sprintf("PR review comment with '%s'", trigger),
		"repository":           event.Repository.FullName,
		"base_branch":          branch,
		"is_pr":                "true",
		"pr_number":            strconv.Itoa(event.PullRequest.Number),
	}
}

// GetLinkedIssue 查询 PR 关联的第一个 Issue（通过 GitHub GraphQL API）
// 返回 Issue 编号和是否成功的标志
// Best-Effort 策略：失败时返回 nil 而非错误
func (c *GitHubClient) GetLinkedIssue(ctx context.Context, repo string, prNumber int) (*int, error) {
	// 1. 获取安装 token
	token, err := c.authProvider.GetInstallationToken(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get installation token: %w", err)
	}

	// 2. 构建 GraphQL 查询
	owner, name := splitRepo(repo)
	query := fmt.Sprintf(`
	{
		repository(owner: "%s", name: "%s") {
			pullRequest(number: %d) {
				closingIssuesReferences(first: 1) {
					nodes {
						number
					}
				}
			}
		}
	}
	`, owner, name, prNumber)

	// 3. 调用 gh api graphql（复用 CLI）
	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-f", fmt.Sprintf("query=%s", query),
		"--header", fmt.Sprintf("Authorization: Bearer %s", token),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh api failed: %w (output: %s)", err, output)
	}

	// 4. 解析响应
	var result struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ClosingIssuesReferences struct {
						Nodes []struct {
							Number int `json:"number"`
						} `json:"nodes"`
					} `json:"closingIssuesReferences"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	nodes := result.Data.Repository.PullRequest.ClosingIssuesReferences.Nodes
	if len(nodes) == 0 {
		return nil, nil // 无关联 Issue（非错误）
	}

	issueNum := nodes[0].Number
	return &issueNum, nil
}
