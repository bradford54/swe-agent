# SQLite TaskStore 技术规格文档

**Issue**: #2
**功能**: 将内存 TaskStore 改造为 SQLite 持久化存储
**生成日期**: 2025-10-18

---

## 1. 问题陈述

### 业务问题
当前 `internal/taskstore/store.go` 使用内存 `map[string]*Task` 存储任务数据，导致服务重启后所有任务历史丢失，无法追踪历史任务执行情况。

### 当前状态
- **数据结构**: `Store.tasks map[string]*Task`（内存存储）
- **并发保护**: `sync.RWMutex`
- **接口方法**:
  - `NewStore() *Store`
  - `Create(task *Task)`
  - `Get(id string) (*Task, bool)`
  - `List() []*Task`
  - `UpdateStatus(id string, status TaskStatus)`
  - `AddLog(id string, level, message string)`

### 预期结果
- 使用 SQLite 本地数据库实现持久化存储
- 保持所有现有接口方法签名不变
- 服务重启后数据仍然存在
- 对 `executor` 和 `webhook` 模块完全透明（零破坏）

---

## 2. 解决方案概览

### 核心策略
将 `Store` 结构体从内存存储改造为基于 `database/sql` + `modernc.org/sqlite` 的 SQL 存储，保持接口不变。

### 主要变更
1. **依赖添加**: `modernc.org/sqlite v1.33.1`（纯 Go，无 CGO）
2. **Store 结构体**: 用 `db *sql.DB` 替换 `tasks map[string]*Task`
3. **数据库初始化**: 自动创建表结构和索引
4. **CRUD 实现**: 用 SQL 查询替换 map 操作
5. **main.go 集成**: 传入 `dbPath` 参数并调用 `Close()`

### 成功标准
- ✅ 所有现有单元测试通过（如果有）
- ✅ 持久化验证测试通过（重启后数据仍存在）
- ✅ 单元测试覆盖率 ≥ 80%
- ✅ 性能基准达标：Create < 5ms, Get < 3ms, List(100) < 20ms

---

## 3. 技术实现

### 3.1 数据库变更

#### 表结构设计

```sql
-- 任务主表
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

-- 日志表（外键关联任务）
CREATE TABLE IF NOT EXISTS logs (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id   TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    level     TEXT NOT NULL CHECK(level IN ('info','error','success','hint')),
    message   TEXT NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- 性能优化索引
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_logs_task_id ON logs(task_id);
```

#### 数据类型映射

| Go 类型 | SQLite 类型 | 存储格式 |
|---------|-------------|----------|
| `string` (ID) | `TEXT PRIMARY KEY` | 原值 |
| `TaskStatus` | `TEXT CHECK(...)` | 字符串枚举 |
| `int` (IssueNumber) | `INTEGER` | 数字 |
| `time.Time` | `DATETIME` | RFC3339 字符串 |
| `[]LogEntry` | 关联表 `logs` | 外键关联 |

#### Migration 脚本

**不需要**独立迁移脚本，数据库初始化通过 `createTables()` 自动执行：

```go
func createTables(db *sql.DB) error {
    schema := `
    CREATE TABLE IF NOT EXISTS tasks (
        id TEXT PRIMARY KEY,
        title TEXT NOT NULL,
        status TEXT NOT NULL CHECK(status IN ('pending','running','completed','failed')),
        repo_owner TEXT NOT NULL,
        repo_name TEXT NOT NULL,
        issue_number INTEGER NOT NULL,
        actor TEXT NOT NULL,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL
    );

    CREATE TABLE IF NOT EXISTS logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        task_id TEXT NOT NULL,
        timestamp DATETIME NOT NULL,
        level TEXT NOT NULL CHECK(level IN ('info','error','success','hint')),
        message TEXT NOT NULL,
        FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
    );

    CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at DESC);
    CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
    CREATE INDEX IF NOT EXISTS idx_logs_task_id ON logs(task_id);
    `
    _, err := db.Exec(schema)
    return err
}
```

---

### 3.2 代码变更

#### 文件 1: `internal/taskstore/store.go`（完全重写）

**变更类型**: 重写现有文件

**新增导入**:
```go
import (
    "database/sql"
    "fmt"
    "log"
    "sync"
    "time"

    _ "modernc.org/sqlite" // SQLite 驱动
)
```

**结构体变更**:
```go
// 修改前
type Store struct {
    mu    sync.RWMutex
    tasks map[string]*Task
}

// 修改后
type Store struct {
    db *sql.DB
    mu sync.RWMutex // 保留以保护并发数据库访问
}
```

**方法实现**:

##### `NewStore(dbPath string) (*Store, error)`

```go
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
```

##### `Close() error`（新增方法）

```go
func (s *Store) Close() error {
    if s.db == nil {
        return nil
    }
    return s.db.Close()
}
```

##### `Create(task *Task) error`

```go
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
```

**错误处理**: 返回 `error`（接口签名从 `func(task *Task)` 改为 `func(task *Task) error`）

##### `Get(id string) (*Task, bool)`

```go
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
            log.Printf("Error scanning log: %v", err)
            continue
        }
        logs = append(logs, logEntry)
    }
    return logs
}
```

##### `List() []*Task`

```go
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
```

##### `UpdateStatus(id string, status TaskStatus)`

```go
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
```

##### `AddLog(id string, level, message string)`

```go
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
```

---

#### 文件 2: `cmd/main.go`（3 处修改）

**变更位置**: 第 54 行附近（taskStore 初始化）

**修改前**:
```go
// Initialize in-memory task store for UI
taskStore := newTaskStore()
```

**修改后**:
```go
// Initialize SQLite task store for UI
dbPath := os.Getenv("TASKSTORE_DB_PATH")
if dbPath == "" {
    dbPath = "./data/tasks.db"
}

// Ensure data directory exists
if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
    return fmt.Errorf("failed to create data directory: %w", err)
}

taskStore, err := newTaskStore(dbPath)
if err != nil {
    return fmt.Errorf("failed to initialize task store: %w", err)
}
defer taskStore.Close()

log.Printf("Task store initialized: %s", dbPath)
```

**新增导入**:
```go
import (
    // ... 现有导入
    "path/filepath"
)
```

---

#### 文件 3: `go.mod`（添加依赖）

**添加**:
```
require modernc.org/sqlite v1.33.1
```

**执行命令**:
```bash
go get modernc.org/sqlite@v1.33.1
go mod tidy
```

---

### 3.3 API 变更

#### 接口兼容性变更

| 方法 | 修改前 | 修改后 | 影响 |
|------|--------|--------|------|
| `NewStore()` | `NewStore() *Store` | `NewStore(dbPath string) (*Store, error)` | 需要传入参数并处理错误 |
| `Create()` | `Create(task *Task)` | `Create(task *Task) error` | **破坏性变更**：返回错误 |
| `Get()` | 不变 | 不变 | 无影响 |
| `List()` | 不变 | 不变 | 无影响 |
| `UpdateStatus()` | 不变 | 不变 | 无影响 |
| `AddLog()` | 不变 | 不变 | 无影响 |
| `Close()` | 无此方法 | `Close() error` | **新增方法** |

**注意**: `Create()` 返回 `error` 是**必要的破坏性变更**，因为 SQL 插入可能失败。

---

### 3.4 配置变更

#### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `TASKSTORE_DB_PATH` | `./data/tasks.db` | SQLite 数据库文件路径 |

**不需要修改** `internal/config/config.go`，因为配置通过 `os.Getenv()` 直接在 `main.go` 读取。

---

## 4. 实现顺序

### Phase 1: 核心存储改造（预计 1 小时）
1. ✅ 添加 `modernc.org/sqlite` 依赖
2. ✅ 重写 `internal/taskstore/store.go`：
   - 修改 `Store` 结构体
   - 实现 `createTables()` 函数
   - 实现 `NewStore(dbPath)` 和 `Close()`
   - 实现 SQL 版本的 CRUD 方法
3. ✅ 修改 `cmd/main.go` 初始化逻辑

### Phase 2: 单元测试（预计 45 分钟）
1. ✅ 创建 `internal/taskstore/store_test.go`
2. ✅ 测试 CRUD 操作
3. ✅ 测试并发安全性
4. ✅ 测试持久化验证（重启后数据仍在）
5. ✅ 测试错误处理

### Phase 3: 集成验证（预计 30 分钟）
1. ✅ 本地运行服务，验证任务创建/查询
2. ✅ 重启服务，验证数据持久化
3. ✅ 检查 Web UI 任务列表功能
4. ✅ 验证 `executor` 和 `webhook` 无感知

### Phase 4: 文档更新（预计 15 分钟）
1. ✅ 更新 `DESIGN_SQLITE_TASKSTORE.md`（如有需要）
2. ✅ 更新 `.env.example` 添加 `TASKSTORE_DB_PATH`
3. ✅ 更新 `Dockerfile` 添加数据卷挂载

**每个阶段必须独立可部署和测试。**

---

## 5. 验证计划

### 5.1 单元测试

**文件**: `internal/taskstore/store_test.go`

**测试用例**:

```go
package taskstore

import (
    "path/filepath"
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
}

func TestSQLiteStore_Get(t *testing.T) {
    tmpDB := filepath.Join(t.TempDir(), "test.db")
    store, _ := NewStore(tmpDB)
    defer store.Close()

    task := &Task{ID: "task-456", Title: "Get Test", Status: StatusRunning, RepoOwner: "owner", RepoName: "repo", IssueNumber: 2, Actor: "user"}
    store.Create(task)

    retrieved, ok := store.Get("task-456")
    if !ok {
        t.Fatal("Task not found")
    }
    if retrieved.Title != "Get Test" {
        t.Errorf("Expected title 'Get Test', got '%s'", retrieved.Title)
    }
}

func TestSQLiteStore_List(t *testing.T) {
    tmpDB := filepath.Join(t.TempDir(), "test.db")
    store, _ := NewStore(tmpDB)
    defer store.Close()

    // 创建多个任务
    for i := 0; i < 5; i++ {
        task := &Task{
            ID: fmt.Sprintf("task-%d", i),
            Title: fmt.Sprintf("Task %d", i),
            Status: StatusPending,
            RepoOwner: "owner",
            RepoName: "repo",
            IssueNumber: i,
            Actor: "user",
        }
        store.Create(task)
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
}

func TestSQLiteStore_UpdateStatus(t *testing.T) {
    tmpDB := filepath.Join(t.TempDir(), "test.db")
    store, _ := NewStore(tmpDB)
    defer store.Close()

    task := &Task{ID: "task-789", Title: "Status Test", Status: StatusPending, RepoOwner: "owner", RepoName: "repo", IssueNumber: 3, Actor: "user"}
    store.Create(task)

    store.UpdateStatus("task-789", StatusCompleted)

    retrieved, _ := store.Get("task-789")
    if retrieved.Status != StatusCompleted {
        t.Errorf("Expected status 'completed', got '%s'", retrieved.Status)
    }
}

func TestSQLiteStore_AddLog(t *testing.T) {
    tmpDB := filepath.Join(t.TempDir(), "test.db")
    store, _ := NewStore(tmpDB)
    defer store.Close()

    task := &Task{ID: "task-log", Title: "Log Test", Status: StatusPending, RepoOwner: "owner", RepoName: "repo", IssueNumber: 4, Actor: "user"}
    store.Create(task)

    store.AddLog("task-log", "info", "First log entry")
    store.AddLog("task-log", "error", "Second log entry")

    retrieved, _ := store.Get("task-log")
    if len(retrieved.Logs) != 2 {
        t.Errorf("Expected 2 logs, got %d", len(retrieved.Logs))
    }
    if retrieved.Logs[0].Message != "First log entry" {
        t.Errorf("Unexpected log message: %s", retrieved.Logs[0].Message)
    }
}

func TestSQLiteStore_Persistence(t *testing.T) {
    tmpDB := filepath.Join(t.TempDir(), "test.db")

    // 第一次创建
    {
        store, _ := NewStore(tmpDB)
        task := &Task{ID: "persist-1", Title: "Persistence Test", Status: StatusPending, RepoOwner: "owner", RepoName: "repo", IssueNumber: 5, Actor: "user"}
        store.Create(task)
        store.Close()
    }

    // 重新打开，验证数据仍在
    {
        store, _ := NewStore(tmpDB)
        defer store.Close()

        retrieved, ok := store.Get("persist-1")
        if !ok {
            t.Fatal("Task not found after reopening database")
        }
        if retrieved.Title != "Persistence Test" {
            t.Errorf("Data corrupted after reopening: %s", retrieved.Title)
        }
    }
}
```

**覆盖率目标**: ≥ 80%

---

### 5.2 集成测试

**手动验证步骤**:

1. **启动服务**:
   ```bash
   export TASKSTORE_DB_PATH=./data/tasks.db
   go run cmd/main.go
   ```

2. **触发任务创建**（通过 webhook 或手动）:
   ```bash
   curl -X POST http://localhost:8000/webhook \
     -H "Content-Type: application/json" \
     -d '{"action": "created", "issue": {"number": 1, "title": "Test"}}'
   ```

3. **验证任务列表**:
   ```bash
   curl http://localhost:8000/tasks
   ```

4. **重启服务**:
   ```bash
   # 停止服务（Ctrl+C）
   go run cmd/main.go
   ```

5. **再次查询任务列表**，验证数据仍在。

---

### 5.3 业务逻辑验证

**验证项**:
- ✅ 任务创建后立即可查询
- ✅ 任务状态更新实时生效
- ✅ 日志按时间顺序排列
- ✅ 服务重启后历史任务仍可访问
- ✅ Web UI `/tasks` 和 `/tasks/{id}` 正常显示

---

## 6. 错误处理策略

### SQL 错误分类

| 错误类型 | 处理方式 | 示例 |
|----------|----------|------|
| 连接失败 | 返回错误，服务启动失败 | `sql.Open()` 失败 |
| 表创建失败 | 返回错误，服务启动失败 | `createTables()` 失败 |
| 插入重复 ID | 记录日志并返回错误 | PRIMARY KEY 冲突 |
| 查询无结果 | 返回 `(nil, false)` | `sql.ErrNoRows` |
| 事务失败 | 回滚并记录日志 | `tx.Commit()` 失败 |

### 日志记录

**使用 `log.Printf()` 记录所有 SQL 错误**:

```go
if err != nil {
    log.Printf("Error updating status for task %s: %v", id, err)
}
```

**不向上抛出非关键错误**（如 `UpdateStatus`、`AddLog` 失败）以避免中断业务流程。

---

## 7. 性能优化

### 数据库配置

```go
db.SetMaxOpenConns(1)  // SQLite 单连接避免锁竞争
db.SetMaxIdleConns(1)
db.SetConnMaxLifetime(0)
```

### 索引策略

- `idx_tasks_created_at DESC`: 优化 `List()` 的 `ORDER BY created_at DESC`
- `idx_tasks_status`: 预留，未来可能按状态筛选
- `idx_logs_task_id`: 优化 `loadLogs()` 的 `WHERE task_id = ?`

### 懒加载

- `List()` **不加载日志**，减少查询开销
- `Get()` 按需加载日志

### 事务使用

- `Create()`: 事务保证任务和日志原子插入
- `AddLog()`: 事务保证日志插入和时间戳更新一致性

---

## 8. 测试数据

### 示例任务

```go
task := &Task{
    ID:          "550e8400-e29b-41d4-a716-446655440000",
    Title:       "Implement user authentication",
    Status:      StatusRunning,
    RepoOwner:   "cexll",
    RepoName:    "swe-agent",
    IssueNumber: 42,
    Actor:       "octocat",
    Logs: []LogEntry{
        {Timestamp: time.Now(), Level: "info", Message: "Task started"},
        {Timestamp: time.Now().Add(1 * time.Second), Level: "success", Message: "Authentication module created"},
    },
}
```

### 边界条件

- 空日志任务
- 超长消息（10KB）
- 并发创建 100 个任务
- 数据库文件路径包含中文字符

---

## 9. 部署注意事项

### Docker 配置

**Dockerfile 修改**:
```dockerfile
# 创建数据目录
RUN mkdir -p /app/data

# 设置环境变量
ENV TASKSTORE_DB_PATH=/app/data/tasks.db

# 挂载卷
VOLUME ["/app/data"]
```

**docker-compose.yml**:
```yaml
services:
  swe-agent:
    image: swe-agent
    volumes:
      - ./data:/app/data  # 持久化数据
    environment:
      TASKSTORE_DB_PATH: /app/data/tasks.db
```

### 数据备份

**定期备份数据库文件**:
```bash
cp ./data/tasks.db ./backups/tasks-$(date +%Y%m%d-%H%M%S).db
```

### 升级路径

如果未来需要迁移到 PostgreSQL/MySQL：
1. 实现新的 `Store` 接口实现（如 `PostgresStore`）
2. 修改 `main.go` 初始化逻辑切换实现
3. 使用数据迁移脚本导出 SQLite 数据并导入新数据库

---

## 10. 风险评估

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|----------|
| `Create()` 接口破坏性变更 | 中 | 高 | 修改所有调用点处理错误 |
| SQLite 文件权限问题 | 中 | 低 | 确保数据目录权限 0755 |
| 并发写入死锁 | 低 | 低 | 单连接 + 事务隔离 |
| 数据库文件损坏 | 高 | 极低 | 定期备份 + WAL 模式 |

---

## 11. 完成标准

- ✅ 所有代码变更已实现
- ✅ 单元测试覆盖率 ≥ 80%
- ✅ 持久化验证测试通过
- ✅ 手动集成测试通过
- ✅ 性能基准达标
- ✅ 代码审查通过
- ✅ 文档已更新

---

## 12. 参考资料

- [SQLite 官方文档](https://www.sqlite.org/docs.html)
- [modernc.org/sqlite GitHub](https://gitlab.com/cznic/sqlite)
- [Go database/sql 教程](https://go.dev/doc/database/sql-tutorial)
- [DESIGN_SQLITE_TASKSTORE.md](file:///Users/a1/work/swe-agent/DESIGN_SQLITE_TASKSTORE.md)

---

**本文档可直接用于代码生成工具自动实现。**
