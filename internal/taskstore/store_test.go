package taskstore

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSQLiteStore_Create(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	task := &Task{
		ID:          "task-123",
		Title:       "Test Task",
		Status:      StatusPending,
		RepoOwner:   "owner",
		RepoName:    "repo",
		IssueNumber: 1,
		Actor:       "user",
	}

	err = store.Create(task)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 验证时间戳已设置
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
	if task.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set")
	}
}

func TestSQLiteStore_Get(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	task := &Task{
		ID:          "task-456",
		Title:       "Get Test",
		Status:      StatusRunning,
		RepoOwner:   "owner",
		RepoName:    "repo",
		IssueNumber: 2,
		Actor:       "user",
	}
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, ok := store.Get("task-456")
	if !ok {
		t.Fatal("Task not found")
	}
	if retrieved.Title != "Get Test" {
		t.Errorf("Expected title 'Get Test', got '%s'", retrieved.Title)
	}
	if retrieved.Status != StatusRunning {
		t.Errorf("Expected status 'running', got '%s'", retrieved.Status)
	}

	// 测试不存在的任务
	_, ok = store.Get("nonexistent")
	if ok {
		t.Error("Expected Get to return false for nonexistent task")
	}
}

func TestSQLiteStore_List(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// 创建多个任务
	for i := 0; i < 5; i++ {
		task := &Task{
			ID:          fmt.Sprintf("task-%d", i),
			Title:       fmt.Sprintf("Task %d", i),
			Status:      StatusPending,
			RepoOwner:   "owner",
			RepoName:    "repo",
			IssueNumber: i,
			Actor:       "user",
		}
		if err := store.Create(task); err != nil {
			t.Fatalf("Create failed for task-%d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // 确保时间戳不同
	}

	tasks := store.List()
	if len(tasks) != 5 {
		t.Errorf("Expected 5 tasks, got %d", len(tasks))
	}

	// 验证按创建时间倒序排序
	if tasks[0].ID != "task-4" {
		t.Errorf("Expected first task to be 'task-4', got '%s'", tasks[0].ID)
	}
	if tasks[4].ID != "task-0" {
		t.Errorf("Expected last task to be 'task-0', got '%s'", tasks[4].ID)
	}
}

func TestSQLiteStore_UpdateStatus(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	task := &Task{
		ID:          "task-789",
		Title:       "Status Test",
		Status:      StatusPending,
		RepoOwner:   "owner",
		RepoName:    "repo",
		IssueNumber: 3,
		Actor:       "user",
	}
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 记录更新前的时间戳
	beforeUpdate := task.UpdatedAt
	time.Sleep(10 * time.Millisecond)

	store.UpdateStatus("task-789", StatusCompleted)

	retrieved, _ := store.Get("task-789")
	if retrieved.Status != StatusCompleted {
		t.Errorf("Expected status 'completed', got '%s'", retrieved.Status)
	}
	if !retrieved.UpdatedAt.After(beforeUpdate) {
		t.Error("UpdatedAt should be updated after status change")
	}
}

func TestSQLiteStore_AddLog(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	task := &Task{
		ID:          "task-log",
		Title:       "Log Test",
		Status:      StatusPending,
		RepoOwner:   "owner",
		RepoName:    "repo",
		IssueNumber: 4,
		Actor:       "user",
	}
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	store.AddLog("task-log", "info", "First log entry")
	time.Sleep(10 * time.Millisecond)
	store.AddLog("task-log", "error", "Second log entry")

	retrieved, _ := store.Get("task-log")
	if len(retrieved.Logs) != 2 {
		t.Errorf("Expected 2 logs, got %d", len(retrieved.Logs))
	}
	if retrieved.Logs[0].Message != "First log entry" {
		t.Errorf("Unexpected first log message: %s", retrieved.Logs[0].Message)
	}
	if retrieved.Logs[0].Level != "info" {
		t.Errorf("Expected first log level 'info', got '%s'", retrieved.Logs[0].Level)
	}
	if retrieved.Logs[1].Message != "Second log entry" {
		t.Errorf("Unexpected second log message: %s", retrieved.Logs[1].Message)
	}
	if retrieved.Logs[1].Level != "error" {
		t.Errorf("Expected second log level 'error', got '%s'", retrieved.Logs[1].Level)
	}
}

func TestSQLiteStore_Persistence(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")

	// 第一次创建
	{
		store, err := NewStore(tmpDB)
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}
		task := &Task{
			ID:          "persist-1",
			Title:       "Persistence Test",
			Status:      StatusPending,
			RepoOwner:   "owner",
			RepoName:    "repo",
			IssueNumber: 5,
			Actor:       "user",
		}
		if err := store.Create(task); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		store.AddLog("persist-1", "info", "Test log")
		store.Close()
	}

	// 重新打开，验证数据仍在
	{
		store, err := NewStore(tmpDB)
		if err != nil {
			t.Fatalf("Failed to reopen store: %v", err)
		}
		defer store.Close()

		retrieved, ok := store.Get("persist-1")
		if !ok {
			t.Fatal("Task not found after reopening database")
		}
		if retrieved.Title != "Persistence Test" {
			t.Errorf("Data corrupted after reopening: %s", retrieved.Title)
		}
		if len(retrieved.Logs) != 1 {
			t.Errorf("Expected 1 log after reopening, got %d", len(retrieved.Logs))
		}
		if retrieved.Logs[0].Message != "Test log" {
			t.Errorf("Log message corrupted: %s", retrieved.Logs[0].Message)
		}
	}
}

func TestSQLiteStore_ConcurrentAccess(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// 并发创建任务
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			task := &Task{
				ID:          fmt.Sprintf("concurrent-%d", id),
				Title:       fmt.Sprintf("Concurrent Task %d", id),
				Status:      StatusPending,
				RepoOwner:   "owner",
				RepoName:    "repo",
				IssueNumber: id,
				Actor:       "user",
			}
			if err := store.Create(task); err != nil {
				t.Errorf("Concurrent create failed for task-%d: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	// 验证所有任务都已创建
	tasks := store.List()
	if len(tasks) != 10 {
		t.Errorf("Expected 10 tasks after concurrent creation, got %d", len(tasks))
	}
}

func TestSQLiteStore_CreateWithLogs(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	task := &Task{
		ID:          "task-with-logs",
		Title:       "Task with Initial Logs",
		Status:      StatusPending,
		RepoOwner:   "owner",
		RepoName:    "repo",
		IssueNumber: 6,
		Actor:       "user",
		Logs: []LogEntry{
			{Timestamp: time.Now(), Level: "info", Message: "Initial log 1"},
			{Timestamp: time.Now(), Level: "success", Message: "Initial log 2"},
		},
	}

	if err := store.Create(task); err != nil {
		t.Fatalf("Create with logs failed: %v", err)
	}

	retrieved, ok := store.Get("task-with-logs")
	if !ok {
		t.Fatal("Task not found")
	}
	if len(retrieved.Logs) != 2 {
		t.Errorf("Expected 2 initial logs, got %d", len(retrieved.Logs))
	}
	if retrieved.Logs[0].Message != "Initial log 1" {
		t.Errorf("Unexpected first log: %s", retrieved.Logs[0].Message)
	}
	if retrieved.Logs[1].Message != "Initial log 2" {
		t.Errorf("Unexpected second log: %s", retrieved.Logs[1].Message)
	}
}

func TestStore_CreateGetAndList(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	taskA := &Task{ID: "a", Title: "first", Status: StatusPending, RepoOwner: "owner", RepoName: "repo", IssueNumber: 1, Actor: "user"}
	if err := store.Create(taskA); err != nil {
		t.Fatalf("Create taskA failed: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	taskB := &Task{ID: "b", Title: "second", Status: StatusPending, RepoOwner: "owner", RepoName: "repo", IssueNumber: 2, Actor: "user"}
	if err := store.Create(taskB); err != nil {
		t.Fatalf("Create taskB failed: %v", err)
	}

	got, ok := store.Get("a")
	if !ok {
		t.Fatal("Get should return true for existing task")
	}
	if got.Title != "first" {
		t.Fatalf("Get returned title %q, want %q", got.Title, "first")
	}

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("List length = %d, want 2", len(list))
	}
	if list[0].ID != "b" || list[1].ID != "a" {
		t.Fatalf("List order = [%s, %s], want [b, a]", list[0].ID, list[1].ID)
	}
	if list[0].CreatedAt.Before(list[1].CreatedAt) {
		t.Fatal("List should be sorted by CreatedAt descending")
	}
}

func TestStore_UpdateStatusAndAddLog(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	task := &Task{ID: "task-1", Title: "Test", Status: StatusPending, RepoOwner: "owner", RepoName: "repo", IssueNumber: 1, Actor: "user"}
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	beforeUpdate := task.UpdatedAt
	time.Sleep(10 * time.Millisecond)
	store.UpdateStatus("task-1", StatusFailed)

	got, _ := store.Get("task-1")
	if got.Status != StatusFailed {
		t.Fatalf("Status = %s, want %s", got.Status, StatusFailed)
	}
	if !got.UpdatedAt.After(beforeUpdate) {
		t.Fatal("UpdatedAt should change after status update")
	}

	store.AddLog("task-1", "info", "processing")
	got, _ = store.Get("task-1") // 重新获取以查看日志
	if len(got.Logs) != 1 {
		t.Fatalf("Logs length = %d, want 1", len(got.Logs))
	}
	if got.Logs[0].Level != "info" || got.Logs[0].Message != "processing" {
		t.Fatalf("Log entry = %+v, want level=info message=processing", got.Logs[0])
	}
	if got.Logs[0].Timestamp.IsZero() {
		t.Fatal("Log timestamp should be set")
	}
}
