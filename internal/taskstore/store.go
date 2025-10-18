package taskstore

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
)

type Task struct {
	ID          string
	Title       string
	Status      TaskStatus
	RepoOwner   string
	RepoName    string
	IssueNumber int
	Actor       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Logs        []LogEntry
}

type LogEntry struct {
	Timestamp time.Time
	Level     string // info, error, success, hint
	Message   string
}

type Store struct {
	db *sql.DB
	mu sync.RWMutex // 保护并发数据库访问
}

// createTables 创建数据库表结构和索引
func createTables(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id           TEXT PRIMARY KEY,
		title        TEXT NOT NULL,
		status       TEXT NOT NULL CHECK(status IN ('pending','running','completed','failed')),
		repo_owner   TEXT NOT NULL,
		repo_name    TEXT NOT NULL,
		issue_number INTEGER NOT NULL,
		actor        TEXT NOT NULL,
		created_at   DATETIME NOT NULL,
		updated_at   DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS logs (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id   TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		level     TEXT NOT NULL CHECK(level IN ('info','error','success','hint')),
		message   TEXT NOT NULL,
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
	CREATE INDEX IF NOT EXISTS idx_logs_task_id ON logs(task_id);
	`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}
	return nil
}

// NewStore 创建新的 SQLite 任务存储
func NewStore(dbPath string) (*Store, error) {
	// 打开数据库连接
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 配置 SQLite 连接池（单连接避免锁竞争）
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// 启用外键约束
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// 创建表结构
	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &Store{db: db}, nil
}

// Close 关闭数据库连接
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Create 创建新任务（事务保证任务和日志原子插入）
func (s *Store) Create(task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 开启事务
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 设置时间戳
	task.CreatedAt = time.Now()
	task.UpdatedAt = time.Now()

	// 插入任务
	_, err = tx.Exec(`
		INSERT INTO tasks (id, title, status, repo_owner, repo_name, issue_number, actor, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.ID, task.Title, task.Status, task.RepoOwner, task.RepoName, task.IssueNumber, task.Actor, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert task: %w", err)
	}

	// 插入初始日志（如果有）
	for _, logEntry := range task.Logs {
		_, err = tx.Exec(`
			INSERT INTO logs (task_id, timestamp, level, message)
			VALUES (?, ?, ?, ?)
		`, task.ID, logEntry.Timestamp, logEntry.Level, logEntry.Message)
		if err != nil {
			return fmt.Errorf("failed to insert log: %w", err)
		}
	}

	// 提交事务
	return tx.Commit()
}

// Get 获取指定 ID 的任务（包含日志）
func (s *Store) Get(id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task := &Task{}
	err := s.db.QueryRow(`
		SELECT id, title, status, repo_owner, repo_name, issue_number, actor, created_at, updated_at
		FROM tasks WHERE id = ?
	`, id).Scan(&task.ID, &task.Title, &task.Status, &task.RepoOwner, &task.RepoName, &task.IssueNumber, &task.Actor, &task.CreatedAt, &task.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		log.Printf("Error getting task %s: %v", id, err)
		return nil, false
	}

	// 加载日志
	task.Logs = s.loadLogs(id)
	return task, true
}

// loadLogs 加载任务的所有日志（按时间升序）
func (s *Store) loadLogs(taskID string) []LogEntry {
	rows, err := s.db.Query(`
		SELECT timestamp, level, message FROM logs WHERE task_id = ? ORDER BY timestamp ASC
	`, taskID)
	if err != nil {
		log.Printf("Error loading logs for task %s: %v", taskID, err)
		return nil
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var logEntry LogEntry
		if err := rows.Scan(&logEntry.Timestamp, &logEntry.Level, &logEntry.Message); err != nil {
			log.Printf("Error scanning log for task %s: %v", taskID, err)
			continue
		}
		logs = append(logs, logEntry)
	}
	return logs
}

// List 列出所有任务（按创建时间倒序）
// 注意: 为性能优化，返回的 Task 不包含日志（Logs 字段为空切片）
// 如需日志详情，请调用 Get(id)
func (s *Store) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 只查询 tasks 表（性能优化：不加载日志）
	rows, err := s.db.Query(`
		SELECT id, title, status, repo_owner, repo_name, issue_number, actor, created_at, updated_at
		FROM tasks ORDER BY created_at DESC
	`)
	if err != nil {
		log.Printf("Error listing tasks: %v", err)
		return nil
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task := &Task{}
		err := rows.Scan(&task.ID, &task.Title, &task.Status, &task.RepoOwner, &task.RepoName, &task.IssueNumber, &task.Actor, &task.CreatedAt, &task.UpdatedAt)
		if err != nil {
			log.Printf("Error scanning task: %v", err)
			continue
		}
		// 注意：List 不加载日志，需要详细信息时调用 Get
		tasks = append(tasks, task)
	}
	return tasks
}

// UpdateStatus 更新任务状态
func (s *Store) UpdateStatus(id string, status TaskStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?
	`, status, time.Now(), id)
	if err != nil {
		log.Printf("Error updating status for task %s: %v", id, err)
	}
}

// AddLog 添加任务日志（事务保证日志插入和时间戳更新一致性）
func (s *Store) AddLog(id string, level, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 开启事务
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction for AddLog: %v", err)
		return
	}
	defer tx.Rollback()

	// 插入日志
	_, err = tx.Exec(`
		INSERT INTO logs (task_id, timestamp, level, message)
		VALUES (?, ?, ?, ?)
	`, id, time.Now(), level, message)
	if err != nil {
		log.Printf("Error inserting log for task %s: %v", id, err)
		return
	}

	// 更新任务 updated_at
	_, err = tx.Exec(`
		UPDATE tasks SET updated_at = ? WHERE id = ?
	`, time.Now(), id)
	if err != nil {
		log.Printf("Error updating timestamp for task %s: %v", id, err)
		return
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction for AddLog: %v", err)
	}
}
