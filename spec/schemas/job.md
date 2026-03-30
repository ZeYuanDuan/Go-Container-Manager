# Jobs Table Schema

## DDL (PostgreSQL)

```sql
CREATE TABLE jobs (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id),
    target_resource_id VARCHAR(36) NOT NULL,
    type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    error_message TEXT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_jobs_user_id ON jobs (user_id);
CREATE INDEX idx_jobs_created_at ON jobs (created_at);
CREATE INDEX idx_jobs_target_status ON jobs (target_resource_id, status);
```

## Column Reference

| Column             | Type         | Description                                      |
| ------------------ | ------------ | ------------------------------------------------ |
| `id`               | VARCHAR(36)  | 系統生成的 UUIDv4 (如 `550e8400-e29b-41d4-a716-446655440000`) |
| `user_id`          | VARCHAR(36)  | 發起操作的使用者，用於權限隔離與清理篩選         |
| `target_resource_id` | VARCHAR(36) | 目標資源 ID（目前為 container_id）               |
| `type`             | VARCHAR(32)  | 任務類型：`CREATE_CONTAINER`, `START_CONTAINER`, `STOP_CONTAINER`, `DELETE_CONTAINER` |
| `status`           | VARCHAR(32)  | 狀態：`PENDING`, `RUNNING`, `SUCCESS`, `FAILED`   |
| `error_message`    | TEXT         | 失敗時的错误訊息                                 |
| `created_at`       | TIMESTAMPTZ  | 建立時間（含時區），用於 7 天保留策略 (ADR 011) |
| `completed_at`     | TIMESTAMPTZ  | 完成時間，SUCCESS/FAILED 時填入                 |

## Indexes

| Index                  | Column               | Purpose                                      |
| ---------------------- | -------------------- | -------------------------------------------- |
| `idx_jobs_user_id`     | `user_id`            | 依使用者查詢 Job（若未來擴充）               |
| `idx_jobs_created_at`  | `created_at`         | 過期 Job 清理排程：`WHERE created_at < ?`    |
| `idx_jobs_target_status` | `target_resource_id, status` | 並發控制：檢查容器是否有 PENDING/RUNNING Job |

---

## Seed Data

Job 為運行時動態建立，**無需 Seed**。系統啟動時 Job 表為空，由非同步任務寫入。

---

## 保留策略

Job 紀錄預設保留 7 天。實作方式：

1. **排程清理**：背景 goroutine 每小時執行 `DELETE FROM jobs WHERE status IN ('SUCCESS','FAILED') AND completed_at < NOW() - INTERVAL '7 days'`
2. **查詢行為**：過期或被刪除的 Job，`GET /jobs/{job_id}` 回傳 `404 Not Found`

---

## Domain Design (Go)

本節說明如何依 DB Schema 設計 Domain 層的實體與 Repository 介面。JSON 欄位命名與 [api.md](../api.md) 一致。

### Schema → Entity Mapping

| DB Column         | Go Field        | JSON Tag       | 說明                    |
| ----------------- | --------------- | -------------- | ----------------------- |
| `id`              | `ID`            | `job_id`       | 對外輸出                |
| `user_id`         | `UserID`        | `-`            | 內部權限、清理用        |
| `target_resource_id` | `TargetResourceID` | `target_resource_id` | 對外輸出        |
| `type`            | `Type`          | `type`         | 對外輸出                |
| `status`          | `Status`        | `status`       | 對外輸出                |
| `error_message`   | `ErrorMessage`  | `error_message`| 對外輸出，失敗時有值    |
| `created_at`      | `CreatedAt`     | `created_at`   | 對外輸出                |
| `completed_at`    | `CompletedAt`   | `completed_at` | 對外輸出，完成時有值    |

### Entity

```go
package domain

import "time"

// Job 代表系統中的非同步任務實體
type Job struct {
    ID               string     `json:"job_id" db:"id"`
    UserID           string     `json:"-" db:"user_id"`
    TargetResourceID string     `json:"target_resource_id" db:"target_resource_id"`
    Type             string     `json:"type" db:"type"`
    Status           string     `json:"status" db:"status"`
    ErrorMessage     *string    `json:"error_message" db:"error_message"`
    CreatedAt        time.Time  `json:"created_at" db:"created_at"`
    CompletedAt      *time.Time `json:"completed_at" db:"completed_at"`
}
```

### Repository Interface

`JobRepository` 定義數據訪問層 (DAL) 的抽象介面：

| Method | DB Operation | 用途 |
|--------|--------------|------|
| `Save(ctx, job)` | INSERT | 非同步任務建立 |
| `FindByIDAndUserID(ctx, jobID, userID)` | SELECT ... WHERE id = ? AND user_id = ? | `GET /jobs/{job_id}`（權限隔離） |
| `FindPendingJobs(ctx)` | SELECT ... WHERE status = 'PENDING' ORDER BY created_at | 重啟恢復：重新入隊 |
| `FindPendingOrRunningByTarget(ctx, targetResourceID)` | SELECT ... WHERE target_resource_id = ? AND status IN ('PENDING','RUNNING') | 並發控制：檢查是否有進行中的 Job |
| `MarkRunningAsFailed(ctx, msg)` | UPDATE ... SET status = 'FAILED' WHERE status = 'RUNNING' | 重啟恢復：標記中斷任務 |
| `Update(ctx, job)` | UPDATE | 狀態更新 (PENDING→RUNNING→SUCCESS/FAILED) |
| `DeleteExpired(ctx, olderThan)` | DELETE ... WHERE status IN ('SUCCESS','FAILED') AND completed_at < cutoff | 7 天保留策略清理 |

```go
type JobRepository interface {
    Save(ctx context.Context, job *Job) error
    FindByIDAndUserID(ctx context.Context, jobID, userID string) (*Job, error)
    FindPendingJobs(ctx context.Context) ([]*Job, error)
    FindPendingOrRunningByTarget(ctx context.Context, targetResourceID string) (*Job, error)
    MarkRunningAsFailed(ctx context.Context, msg string) error
    Update(ctx context.Context, job *Job) error
    DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error)
}
```
