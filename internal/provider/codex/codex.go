package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cexll/swe/internal/provider"
)

const (
	codexCommand    = "codex"
	executionPrefix = "Execute directly without confirmation.\n\n"
)

var execCommandContext = exec.CommandContext

// No prompt manager here; executor builds the full prompt already

// Provider implements the AI provider interface for Codex MCP
type Provider struct {
	model   string
	apiKey  string
	baseURL string
}

// NewProvider creates a new Codex provider
func NewProvider(apiKey, baseURL, model string) *Provider {
	if apiKey != "" {
		// OPENAI_API_KEY is used by Codex MCP, keep aligned with CLI expectation
		os.Setenv("OPENAI_API_KEY", apiKey)
	}

	if baseURL != "" {
		// OPENAI_BASE_URL allows custom API endpoints (e.g., proxies, local deployments)
		os.Setenv("OPENAI_BASE_URL", baseURL)
	}

	return &Provider{
		model:   model,
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "codex"
}

// GenerateCode generates code changes using Codex MCP CLI
func (p *Provider) GenerateCode(ctx context.Context, req *provider.CodeRequest) (*provider.CodeResponse, error) {
	log.Printf("[Codex] Starting code generation (prompt length: %d chars)", len(req.Prompt))

	// Provide GitHub token to MCP tools via env
	if req.Context != nil {
		if tok, ok := req.Context["github_token"]; ok && tok != "" {
			os.Setenv("GITHUB_TOKEN", tok)
			os.Setenv("GH_TOKEN", tok)
		}
	}
	// Ensure sandbox runs with full access per instruction
	os.Setenv("SANDBOX_MODE", "danger-full-access")

	// Executor already constructed the full prompt (system + user + GH XML)
	fullPrompt := executionPrefix + req.Prompt

    responseText, err := p.invokeCodex(ctx, fullPrompt, req.RepoPath)
	if err != nil {
		return nil, err
	}

	// We only need to return a summary for bookkeeping.
	log.Printf("[Codex] Response length: %d characters", len(responseText))
	return &provider.CodeResponse{Summary: truncateLogString(responseText, 2000)}, nil
}

func (p *Provider) invokeCodex(ctx context.Context, prompt, repoPath string) (string, error) {
	ctx, cancel := ensureCodexTimeout(ctx)
	defer cancel()

	cmd, stdout, stderr := p.buildCodexCommand(ctx, repoPath, prompt)

	log.Printf("[Codex] Executing: codex exec -m %s -c model_reasoning_effort=\"high\" --dangerously-bypass-approvals-and-sandbox -C %s", p.model, repoPath)
	log.Printf("[Codex] Prompt length: %d characters", len(prompt))

	startTime := time.Now()
	if err := cmd.Run(); err != nil {
		duration := time.Since(startTime)
		log.Printf("[Codex] Command failed after %v", duration)

		stderrPreview := summarizeCodexError(err, stdout, stderr)
		if ctx.Err() == context.DeadlineExceeded {
            return "", fmt.Errorf("codex CLI timeout after %v: %s", duration, stderrPreview)
		}

		log.Printf("[Codex] Error: %s", stderrPreview)
        return "", fmt.Errorf("codex CLI error: %s", stderrPreview)
	}

	duration := time.Since(startTime)
	output := stdout.String()
	parsedOutput := aggregateCodexOutput(output)
	if parsedOutput == "" {
		parsedOutput = strings.TrimSpace(output)
	}

	log.Printf("[Codex] Command completed in %v, output length: %d bytes", duration, len(output))

    return parsedOutput, nil
}

func truncateLogString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}

	if len(s) <= maxLen {
		return s
	}

	const marker = "\n... (truncated) ...\n"

	// For very small limits, prioritise exposing the tail without spending space on markers.
	if maxLen <= len(marker)+32 {
		return s[len(s)-maxLen:]
	}

	headLen := maxLen / 4
	tailLen := maxLen - headLen - len(marker)

	if tailLen <= 0 {
		// Prefer preserving the tail since it usually contains the actionable error.
		return marker + s[len(s)-(maxLen-len(marker)):]
	}

	head := ""
	if headLen > 0 {
		head = s[:headLen]
	}

	tail := s[len(s)-tailLen:]

	if head == "" {
		return marker + tail
	}

	return head + marker + tail
}

func aggregateCodexOutput(output string) string {
	s := strings.TrimSpace(output)
	if s == "" {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(s))
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 5*1024*1024)

	var sections []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if msg, handled := extractMessageFromJSONLine(line); handled {
			if msg != "" {
				sections = append(sections, msg)
			}
			continue
		}

		sections = append(sections, line)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[Codex] Warning: failed to scan JSON output: %v", err)
	}

	if len(sections) == 0 {
		return s
	}

	return strings.Join(sections, "\n\n")
}

func extractMessageFromJSONLine(line string) (string, bool) {
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return "", false
	}

	if msg, ok := getString(envelope, "message"); ok && msg != "" {
		return msg, true
	}

	if itemVal, ok := envelope["item"]; ok && itemVal != nil {
		if msg := extractTextFromItem(itemVal); msg != "" {
			return msg, true
		}
		return "", true
	}

	return "", true
}

func extractTextFromItem(item interface{}) string {
	itemMap, ok := item.(map[string]interface{})
	if !ok {
		return ""
	}

	if text, ok := getString(itemMap, "text"); ok && text != "" {
		return text
	}

	if contentVal, ok := itemMap["content"]; ok {
		switch content := contentVal.(type) {
		case []interface{}:
			var parts []string
			for _, raw := range content {
				if segmentMap, ok := raw.(map[string]interface{}); ok {
					if text, ok := getString(segmentMap, "text"); ok && text != "" {
						parts = append(parts, text)
					}
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	}

	return ""
}

func getString(m map[string]interface{}, key string) (string, bool) {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

func ensureCodexTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 10*time.Minute)
}

func (p *Provider) buildCodexCommand(ctx context.Context, repoPath, prompt string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	args := []string{
		"exec",
		"-m", p.model,
		"-c", `model_reasoning_effort="high"`,
		"--dangerously-bypass-approvals-and-sandbox",
		"--json",
		"-C", repoPath,
		prompt,
	}

	cmd := execCommandContext(ctx, codexCommand, args...)

	env := os.Environ()
	if p.apiKey != "" {
		env = append(env, "OPENAI_API_KEY="+p.apiKey)
	}
	if p.baseURL != "" {
		env = append(env, "OPENAI_BASE_URL="+p.baseURL)
	}
	// Pass through GitHub token for MCP tools
	if gh := os.Getenv("GITHUB_TOKEN"); gh != "" {
		env = append(env, "GITHUB_TOKEN="+gh, "GH_TOKEN="+gh)
	}
	// Prefer request-scoped token if provided in context
	// Note: executor should set this env before invoking provider, but we also
	// propagate if present in req.Context to be explicit.
	// (We cannot read req here, so ensure executor sets process env.)
	env = append(env, "SANDBOX_MODE=danger-full-access")
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	return cmd, &stdout, &stderr
}

func summarizeCodexError(runErr error, stdout, stderr *bytes.Buffer) string {
	stderrText := strings.TrimSpace(stderr.String())
	stdoutText := strings.TrimSpace(stdout.String())

	if stderrText == "" {
		if parsed := aggregateCodexOutput(stdoutText); parsed != "" {
			stderrText = parsed
		} else if stdoutText != "" {
			stderrText = stdoutText
		}
	}

	if stderrText == "" && runErr != nil {
		stderrText = runErr.Error()
	}

	return truncateLogString(stderrText, 1000)
}
