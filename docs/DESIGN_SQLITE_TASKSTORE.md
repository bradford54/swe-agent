# SQLite TaskStore 实现设计文档

## 背景

当前 `taskstore.Store` 使用内存存储（`map[string]*Task`），重启后数据丢失。需要使用 SQLite 实现本地持久化。

## 设计原则

- **KISS**: 最简单的 SQLite 实现，无缓存层、无复杂抽象
- **YAGNI**: 只实现持久化，不添加多余功能
- **SOLID**: 保持现有接口，单一职责原则
- **零破坏**: 对 Handler 和 Executor 透明

## 数据库设计

### 表结构

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

-- 日志表
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

### 数据类型映射

| Go 类型 | SQLite 类型 | 说明 |
|---------|-------------|------|
| `string` | `TEXT` | ID、状态、仓库信息等 |
| `int` | `INTEGER` | Issue 号、日志 ID |
| `time.Time` | `DATETIME` | 创建/更新时间 |
| `TaskStatus` | `TEXT` | 枚举类型用 CHECK 约束 |

## 实现方案

### 1. 依赖库选择

**推荐**: `modernc.org/sqlite` (纯 Go 实现)

```go
import (
    "database/sql"
    _ "modernc.org/sqlite"
)
```

**理由**:
- 纯 Go 实现，无需 CGO
- 跨平台编译无障碍（Windows/Linux/macOS）
- 性能接近 `mattn/go-sqlite3`
- 符合 Go 标准库 `database/sql` 接口

### 2. Store 结构体改造

```go
// internal/taskstore/store.go

type Store struct {
    db *sql.DB
    mu sync.RWMutex  // 保护并发数据库访问
}

func NewStore(dbPath string) (*Store, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }

    // 配置连接池
    db.SetMaxOpenConns(1)  // SQLite 建议单连接
    db.SetMaxIdleConns(1)
    db.SetConnMaxLifetime(0)

    // 启用外键约束
    if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
        return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
    }

    // 创建表
    if err := createTables(db); err != nil {
        return nil, fmt.Errorf("failed to create tables: %w", err)
    }

    return &Store{db: db}, nil
}

func (s *Store) Close() error {
    return s.db.Close()
}
```

### 3. CRUD 实现

#### Create (事务原子性)

```go
func (s *Store) Create(task *Task) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    tx, err := s.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    task.CreatedAt = time.Now()
    task.UpdatedAt = time.Now()

    // 插入任务
    _, err = tx.Exec(`
        INSERT INTO tasks (id, title, status, repo_owner, repo_name, issue_number, actor, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, task.ID, task.Title, task.Status, task.RepoOwner, task.RepoName, task.IssueNumber, task.Actor, task.CreatedAt, task.UpdatedAt)
    if err != nil {
        return err
    }

    // 插入初始日志（如果有）
    for _, log := range task.Logs {
        _, err = tx.Exec(`
            INSERT INTO logs (task_id, timestamp, level, message)
            VALUES (?, ?, ?, ?)
        `, task.ID, log.Timestamp, log.Level, log.Message)
        if err != nil {
            return err
        }
    }

    return tx.Commit()
}
```

#### Get (关联查询)

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
        log.Printf("Error getting task: %v", err)
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
        return nil
    }
    defer rows.Close()

    var logs []LogEntry
    for rows.Next() {
        var log LogEntry
        if err := rows.Scan(&log.Timestamp, &log.Level, &log.Message); err != nil {
            continue
        }
        logs = append(logs, log)
    }
    return logs
}
```

#### List (优化查询)

```go
func (s *Store) List() []*Task {
    s.mu.RLock()
    defer s.mu.RUnlock()

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
            continue
        }
        // 注意: List 不加载日志(性能优化)，需要时调用 Get
        tasks = append(tasks, task)
    }
    return tasks
}
```

#### UpdateStatus

```go
func (s *Store) UpdateStatus(id string, status TaskStatus) {
    s.mu.Lock()
    defer s.mu.Unlock()

    _, err := s.db.Exec(`
        UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?
    `, status, time.Now(), id)
    if err != nil {
        log.Printf("Error updating status: %v", err)
    }
}
```

#### AddLog

```go
func (s *Store) AddLog(id string, level, message string) {
    s.mu.Lock()
    defer s.mu.Unlock()

    tx, _ := s.db.Begin()
    defer tx.Rollback()

    // 插入日志
    _, err := tx.Exec(`
        INSERT INTO logs (task_id, timestamp, level, message)
        VALUES (?, ?, ?, ?)
    `, id, time.Now(), level, message)
    if err != nil {
        log.Printf("Error adding log: %v", err)
        return
    }

    // 更新任务 updated_at
    _, err = tx.Exec(`
        UPDATE tasks SET updated_at = ? WHERE id = ?
    `, time.Now(), id)
    if err != nil {
        log.Printf("Error updating task timestamp: %v", err)
        return
    }

    tx.Commit()
}
```

## 集成方式

### 1. 主程序初始化

```go
// cmd/main.go

import (
    "github.com/cexll/swe/internal/taskstore"
)

func main() {
    // 获取数据库路径（环境变量或默认值）
    dbPath := os.Getenv("TASKSTORE_DB_PATH")
    if dbPath == "" {
        dbPath = "./data/tasks.db"
    }

    // 确保数据目录存在
    os.MkdirAll(filepath.Dir(dbPath), 0755)

    // 初始化 Store
    store, err := taskstore.NewStore(dbPath)
    if err != nil {
        log.Fatalf("Failed to initialize taskstore: %v", err)
    }
    defer store.Close()

    // ... 其余代码不变
    executor := executor.New(provider, appAuth).WithStore(store)
    handler := webhook.NewHandler(cfg.WebhookSecret, cfg.TriggerKeyword, dispatcher, store, appAuth)
}
```

### 2. Docker 配置

```dockerfile
# Dockerfile

# 添加数据目录
RUN mkdir -p /app/data

# 设置环境变量
ENV TASKSTORE_DB_PATH=/app/data/tasks.db

# 挂载卷
VOLUME ["/app/data"]
```

```yaml
# docker-compose.yml

services:
  swe-agent:
    image: swe-agent
    volumes:
      - ./data:/app/data  # 持久化数据
    environment:
      TASKSTORE_DB_PATH: /app/data/tasks.db
```

## 测试策略

### 1. 单元测试

```go
// internal/taskstore/store_test.go

func TestSQLiteStore(t *testing.T) {
    // 使用临时文件
    tmpDB := filepath.Join(t.TempDir(), "test.db")
    store, err := NewStore(tmpDB)
    require.NoError(t, err)
    defer store.Close()

    // 测试 Create
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
    require.NoError(t, err)

    // 测试 Get
    retrieved, ok := store.Get("task-123")
    require.True(t, ok)
    require.Equal(t, "Test Task", retrieved.Title)

    // 测试 UpdateStatus
    store.UpdateStatus("task-123", StatusRunning)
    retrieved, _ = store.Get("task-123")
    require.Equal(t, StatusRunning, retrieved.Status)

    // 测试 AddLog
    store.AddLog("task-123", "info", "Test log")
    retrieved, _ = store.Get("task-123")
    require.Len(t, retrieved.Logs, 1)
}
```

### 2. 持久化验证测试

```go
func TestPersistence(t *testing.T) {
    tmpDB := filepath.Join(t.TempDir(), "test.db")

    // 第一次创建
    {
        store, _ := NewStore(tmpDB)
        task := &Task{ID: "task-1", Title: "Test"}
        store.Create(task)
        store.Close()
    }

    // 重新打开，验证数据仍在
    {
        store, _ := NewStore(tmpDB)
        defer store.Close()

        retrieved, ok := store.Get("task-1")
        require.True(t, ok)
        require.Equal(t, "Test", retrieved.Title)
    }
}
```

## 性能考虑

### 优化措施

1. **连接池配置**
   ```go
   db.SetMaxOpenConns(1)  // SQLite 单连接避免锁竞争
   ```

2. **索引优化**
   - `idx_tasks_created_at`: 优化 List() 排序
   - `idx_logs_task_id`: 优化日志查询

3. **懒加载**
   - `List()` 不加载日志，减少查询开销
   - `Get()` 按需加载日志

4. **事务批量写入**
   - `Create()` 和 `AddLog()` 使用事务保证原子性

### 性能基准

| 操作 | 预期性能 | 说明 |
|------|----------|------|
| Create | < 5ms | 单任务插入 |
| Get | < 3ms | 包含日志加载 |
| List(100) | < 20ms | 不含日志 |
| UpdateStatus | < 2ms | 单字段更新 |
| AddLog | < 5ms | 事务插入 |

## 迁移计划

### Phase 1: 开发环境验证 (2小时)
- [ ] 修改 `internal/taskstore/store.go`
- [ ] 编写单元测试
- [ ] 本地运行验证

### Phase 2: 测试环境部署 (1小时)
- [ ] 更新 Dockerfile
- [ ] 配置数据卷挂载
- [ ] 执行集成测试

### Phase 3: 生产部署 (30分钟)
- [ ] 更新环境变量配置
- [ ] 部署新版本
- [ ] 监控任务持久化情况

## 回滚方案

如果出现问题，保留原内存实现：

```go
// internal/taskstore/store_memory.go (备份原实现)

type MemoryStore struct {
    mu    sync.RWMutex
    tasks map[string]*Task
}
```

修改 `main.go` 切换回内存版本：

```go
// 回滚: 使用内存版本
store := taskstore.NewMemoryStore()
```

## 后续扩展

遵循 YAGNI 原则，以下功能**不在首版实现**:

- ❌ 内存缓存层（当前性能已足够）
- ❌ 多数据库支持（只需 SQLite）
- ❌ 数据备份功能（可用文件系统备份）
- ❌ 查询过滤器（List 已满足需求）

当**真实需求**出现时再添加。

## 总结

**核心优势**:
- ✅ KISS: 最简 SQLite 实现，无过度设计
- ✅ YAGNI: 只实现持久化，不添加无关功能
- ✅ SOLID: 保持接口不变，符合 SRP
- ✅ 零破坏: 对现有代码透明
- ✅ 易测试: 使用标准库，单元测试简单
- ✅ 易维护: 纯 Go 实现，无 CGO 依赖

**风险控制**:
- ✅ 渐进式迁移
- ✅ 完整测试覆盖
- ✅ 简单回滚机制
