# Containers Table Schema

## DDL (PostgreSQL)

```sql
CREATE TABLE containers (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id),
    name VARCHAR(255) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    command JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'Created',
    mount_path VARCHAR(255) NOT NULL DEFAULT '/workspace',
    docker_container_id VARCHAR(64) NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, name)
);

CREATE INDEX idx_containers_user_id ON containers (user_id);
CREATE INDEX idx_containers_status ON containers (status);
```

## Column Reference

| Column               | Type         | Description                                      |
| -------------------- | ------------ | ------------------------------------------------ |
| `id`                 | VARCHAR(36)  | 系統生成的 UUIDv4 (如 `550e8400-e29b-41d4-a716-446655440000`)，對應 API container_id |
| `user_id`            | VARCHAR(36)  | 擁有者 ID，用於權限隔離                          |
| `name`               | VARCHAR(255) | 容器名稱，同一使用者下唯一                        |
| `environment`        | VARCHAR(64)  | 環境標籤 (如 `python-3.10`)，對應後端 Image 白名單 |
| `command`            | JSONB        | 啟動指令陣列 (如 `["python", "/workspace/train.py"]`)，PostgreSQL 原生 JSON 支援 |
| `status`             | VARCHAR(32)  | 狀態：`Created`, `Running`, `Exited`, `Dead`     |
| `mount_path`         | VARCHAR(255) | 掛載點，固定為 `/workspace` (ADR 008)            |
| `docker_container_id`| VARCHAR(64)  | Docker 容器 ID，創建完成後由 runtime 填入；刪除後為 NULL |
| `created_at`         | TIMESTAMPTZ  | 建立時間（含時區）                             |
| `updated_at`         | TIMESTAMPTZ  | 最後更新時間（含時區），狀態變更時更新         |

## Indexes

| Index                   | Column      | Purpose                                      |
| ----------------------- | ----------- | -------------------------------------------- |
| `idx_containers_user_id`| `user_id`   | `GET /containers` 依 user 篩選               |
| `idx_containers_status` | `status`    | 並發控制、狀態查詢                            |
| `UNIQUE (user_id, name)`| user_id, name | 同一使用者下容器名稱唯一，避免重複創建     |

---

## Container State Machine

本節定義容器狀態的合法流轉與 409 Conflict 防護規則，為 [ADR 010](../ADR.md#adr-010基於狀態機的並發控制-concurrency-control) 並發控制的實作依據。

### 狀態定義

| 狀態     | 說明                         |
| -------- | ---------------------------- |
| `Created`| 已創建但未執行（Docker 容器尚未建立或已移除） |
| `Running`| 執行中                       |
| `Exited` | 已停止（容器內程序正常或手動結束） |
| `Dead`   | 異常（Docker Daemon 錯誤、資源不足等） |

### 合法狀態流轉

```
Created  ──(Start)──► Running
Running  ──(Stop)──► Exited      # 或容器內程序自然結束
Exited   ──(Start)──► Running    # 重啟
Created  ──(Delete)──► (移除)
Exited   ──(Delete)──► (移除)
Dead     ──(Delete)──► (移除)
Running  ──(異常)──► Dead        # Docker 層級錯誤
```

### 409 Conflict 防護規則

| API | 擋下條件 | 回傳訊息 |
|-----|----------|----------|
| `POST /containers/{id}/start` | 狀態為 `Running` | "Container is already running or another operation is in progress" |
| `POST /containers/{id}/stop`  | 狀態為 `Created`、`Exited`、`Dead` | "Container is not running or already stopped" |
| `DELETE /containers/{id}`     | 狀態為 `Running` | "Container must be stopped before deletion. Call POST /containers/{id}/stop first" |

**補充**：若該容器已有 PENDING 或 RUNNING 的 Job 正在操作，上述任一 API 亦應回傳 409，避免與背景任務競態。

---

## Domain Design (Go)

本節說明如何依 DB Schema 設計 Domain 層的實體與 Repository 介面。JSON 欄位命名與 [api.md](../api.md) 一致。

### Schema → Entity Mapping

| DB Column          | Go Field          | JSON Tag        | 說明                    |
| ------------------ | ----------------- | --------------- | ----------------------- |
| `id`               | `ID`              | `container_id`  | 對外輸出                |
| `user_id`          | `UserID`          | `-`             | 內部權限隔離用          |
| `name`             | `Name`            | `name`          | 對外輸出                |
| `environment`      | `Environment`     | `environment`   | 對外輸出                |
| `command`          | `Command`         | `command`       | 對外輸出，JSON 陣列     |
| `status`           | `Status`          | `status`        | 對外輸出                |
| `mount_path`       | `MountPath`       | `mount_path`    | 對外輸出                |
| `docker_container_id` | `DockerContainerID` | `-`          | 內部使用，不對外輸出   |
| `created_at`       | `CreatedAt`       | `created_at`    | 對外輸出                |
| `updated_at`       | `UpdatedAt`       | `-`             | 內部使用，Debug 時可選輸出 |

### Entity

```go
package domain

import "time"

// Container 代表系統中的容器實體
type Container struct {
    ID                 string    `json:"container_id" db:"id"`
    UserID             string    `json:"-" db:"user_id"`
    Name               string    `json:"name" db:"name"`
    Environment        string    `json:"environment" db:"environment"`
    Command            []string  `json:"command" db:"command"`
    Status             string    `json:"status" db:"status"`
    MountPath          string    `json:"mount_path" db:"mount_path"`
    DockerContainerID   string    `json:"-" db:"docker_container_id"`
    CreatedAt          time.Time `json:"created_at" db:"created_at"`
    UpdatedAt          time.Time `json:"-" db:"updated_at"`
}
```

### Repository Interface

`ContainerRepository` 定義數據訪問層 (DAL) 的抽象介面。並發控制時需配合 `SELECT ... FOR UPDATE` 取得 Row-Level Lock。

| Method | DB Operation | 對應 API |
|--------|--------------|----------|
| `Save(ctx, container)` | INSERT | `POST /containers` |
| `FindByUserID(ctx, userID, page, limit)` | SELECT ... WHERE user_id = ? LIMIT/OFFSET | `GET /containers` |
| `FindByID(ctx, containerID)` | SELECT ... WHERE id = ? | `GET /containers/{id}` |
| `FindByIDForUpdate(ctx, containerID)` | SELECT ... WHERE id = ? FOR UPDATE | 並發控制鎖定 |
| `Update(ctx, container)` | UPDATE | 狀態更新、Docker ID 寫回 |
| `Delete(ctx, containerID)` | DELETE | `DELETE /containers/{id}` |

```go
type ContainerRepository interface {
    Save(ctx context.Context, container *Container) error
    FindByUserID(ctx context.Context, userID string, page, limit int) ([]*Container, int, error)
    FindByID(ctx context.Context, containerID string) (*Container, error)
    FindByIDForUpdate(ctx context.Context, containerID string) (*Container, error)
    Update(ctx context.Context, container *Container) error
    Delete(ctx context.Context, containerID string) error
}
```
