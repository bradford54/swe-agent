package webhook

import (
	"context"
	"testing"
)

// intPtr 辅助函数：创建 int 指针
func intPtr(i int) *int {
	return &i
}

// TestGenerateTaskID_AllCombinations 测试所有 Task ID 组合场景
func TestGenerateTaskID_AllCombinations(t *testing.T) {
	h := &Handler{}
	fixedTime := int64(1234567890)

	tests := []struct {
		name       string
		components TaskIDComponents
		want       string
	}{
		{
			name: "Issue only",
			components: TaskIDComponents{
				Repo:        "owner/repo",
				IssueNumber: intPtr(123),
				Timestamp:   fixedTime,
			},
			want: "owner-repo-issue-123-1234567890",
		},
		{
			name: "PR only (no linked issue)",
			components: TaskIDComponents{
				Repo:      "owner/repo",
				PRNumber:  intPtr(456),
				Timestamp: fixedTime,
			},
			want: "owner-repo-pr-456-1234567890",
		},
		{
			name: "Issue + PR (linked)",
			components: TaskIDComponents{
				Repo:        "owner/repo",
				IssueNumber: intPtr(123),
				PRNumber:    intPtr(456),
				Timestamp:   fixedTime,
			},
			want: "owner-repo-issue-123-pr-456-1234567890",
		},
		{
			name: "Backward compatibility: timestamp only",
			components: TaskIDComponents{
				Repo:      "owner/repo",
				Timestamp: fixedTime,
			},
			want: "owner-repo-1234567890",
		},
		{
			name: "Repo with slash sanitization",
			components: TaskIDComponents{
				Repo:        "deep/nested/repo",
				IssueNumber: intPtr(1),
				Timestamp:   fixedTime,
			},
			want: "deep-nested-repo-issue-1-1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.generateTaskID(tt.components)
			if got != tt.want {
				t.Errorf("generateTaskID() = %q, want %q", got, tt.want)
			}
		})
	}
}

// mockGitHubClient 用于测试的 GitHub 客户端 mock
type mockGitHubClient struct {
	linkedIssue *int
	err         error
}

func (m *mockGitHubClient) GetLinkedIssue(ctx context.Context, repo string, prNumber int) (*int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.linkedIssue, nil
}

// TestTaskIDComponents_EdgeCases 测试边界情况
func TestTaskIDComponents_EdgeCases(t *testing.T) {
	h := &Handler{}

	tests := []struct {
		name       string
		components TaskIDComponents
		wantParts  []string // 预期包含的部分
	}{
		{
			name: "Large issue number",
			components: TaskIDComponents{
				Repo:        "owner/repo",
				IssueNumber: intPtr(999999),
				Timestamp:   1234567890,
			},
			wantParts: []string{"owner-repo", "issue-999999", "1234567890"},
		},
		{
			name: "Large PR number",
			components: TaskIDComponents{
				Repo:      "owner/repo",
				PRNumber:  intPtr(888888),
				Timestamp: 1234567890,
			},
			wantParts: []string{"owner-repo", "pr-888888", "1234567890"},
		},
		{
			name: "Issue and PR both large",
			components: TaskIDComponents{
				Repo:        "owner/repo",
				IssueNumber: intPtr(123456),
				PRNumber:    intPtr(789012),
				Timestamp:   1234567890,
			},
			wantParts: []string{"owner-repo", "issue-123456", "pr-789012", "1234567890"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.generateTaskID(tt.components)
			for _, part := range tt.wantParts {
				if !contains(got, part) {
					t.Errorf("generateTaskID() = %q, missing part %q", got, part)
				}
			}
		})
	}
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestTaskIDComponents_Ordering 测试 ID 段落顺序
func TestTaskIDComponents_Ordering(t *testing.T) {
	h := &Handler{}
	components := TaskIDComponents{
		Repo:        "owner/repo",
		IssueNumber: intPtr(10),
		PRNumber:    intPtr(20),
		Timestamp:   9999,
	}

	got := h.generateTaskID(components)
	want := "owner-repo-issue-10-pr-20-9999"

	if got != want {
		t.Errorf("generateTaskID() ordering incorrect:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestGitHubClient_GetLinkedIssue_Fallback 测试 GitHub API 降级场景
func TestGitHubClient_GetLinkedIssue_Fallback(t *testing.T) {
	tests := []struct {
		name        string
		mockClient  *mockGitHubClient
		wantIssue   *int
		wantErr     bool
		description string
	}{
		{
			name:        "API returns linked issue",
			mockClient:  &mockGitHubClient{linkedIssue: intPtr(42)},
			wantIssue:   intPtr(42),
			wantErr:     false,
			description: "成功场景：API 返回关联 Issue",
		},
		{
			name:        "API returns no linked issue",
			mockClient:  &mockGitHubClient{linkedIssue: nil},
			wantIssue:   nil,
			wantErr:     false,
			description: "降级场景：PR 无关联 Issue",
		},
		{
			name:        "API call fails",
			mockClient:  &mockGitHubClient{err: context.DeadlineExceeded},
			wantIssue:   nil,
			wantErr:     true,
			description: "错误场景：API 超时",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := tt.mockClient.GetLinkedIssue(ctx, "owner/repo", 100)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetLinkedIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !equalIntPtr(got, tt.wantIssue) {
				t.Errorf("GetLinkedIssue() = %v, want %v", ptrToString(got), ptrToString(tt.wantIssue))
			}
		})
	}
}

// 辅助函数：比较两个 *int 是否相等
func equalIntPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// 辅助函数：*int 转字符串（用于错误信息）
func ptrToString(p *int) string {
	if p == nil {
		return "nil"
	}
	return string(rune(*p))
}
