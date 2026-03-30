# Files Table Schema

## DDL (PostgreSQL)

```sql
CREATE TABLE files (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id),
    original_filename VARCHAR(255) NOT NULL,
    storage_path VARCHAR(512) NOT NULL,
    mime_type VARCHAR(128) NOT NULL,
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_files_user_id ON files (user_id);
```

## Column Reference

| Column | Type | Description |
|--------|------|-------------|
| `id` | VARCHAR(36) | 系統生成的 UUIDv4 (如 `550e8400-e29b-41d4-a716-446655440000`) |
| `user_id` | VARCHAR(36) | 擁有者 ID，用於權限隔離 |
| `original_filename` | VARCHAR(255) | 使用者上傳時的原始檔名 (如 `data.csv`) |
| `storage_path` | VARCHAR(512) | 伺服器上的物理路徑；採 `{filename}_{uuid}.{ext}` 策略（如 `uploads/{user_id}/data_{uuid}.csv`、`test_{uuid}.png`）以解決檔名衝突並保留副檔名供編輯器識別 |
| `mime_type` | VARCHAR(128) | 驗證後的 MIME 類型（如 `text/csv`），對應 ADR 012 白名單 |
| `size_bytes` | BIGINT | 檔案大小，用於容量限制 |
| `created_at` | TIMESTAMPTZ | 建立時間（含時區） |

## Indexes

| Index | Column | Purpose |
|-------|--------|---------|
| `idx_files_user_id` | `user_id` | `GET /files` 永遠會加上 `WHERE user_id = ?` |

---

## Domain Design (Go)

本節說明如何依 DB Schema 設計 Domain 層的實體與 Repository 介面。JSON 欄位命名與 [api.md](../api.md) 一致。

### Schema → Entity Mapping

| DB Column | Go Field | JSON Tag | 說明 |
|-----------|----------|----------|------|
| `id` | `ID` | `file_id` | 對外輸出，與 API 一致 |
| `user_id` | `UserID` | `-` | 不對外輸出，僅內部權限隔離用 |
| `original_filename` | `OriginalFilename` | `filename` | API 回傳時使用 `filename` |
| `storage_path` | `StoragePath` | `-` | 內部物理路徑，嚴格保密，絕不輸出 |
| `mime_type` | `MimeType` | `-` | 內部使用，下載時可設 Content-Type |
| `size_bytes` | `SizeBytes` | `size_bytes` | 對外輸出 |
| `created_at` | `CreatedAt` | `created_at` | 對外輸出 |

### Entity

```go
package domain

import "time"

// File 代表系統中的一個檔案實體
type File struct {
    ID               string    `json:"file_id" db:"id"`
    UserID           string    `json:"-" db:"user_id"`
    OriginalFilename string    `json:"filename" db:"original_filename"`
    StoragePath      string    `json:"-" db:"storage_path"`
    MimeType         string    `json:"-" db:"mime_type"`
    SizeBytes        int64     `json:"size_bytes" db:"size_bytes"`
    CreatedAt        time.Time `json:"created_at" db:"created_at"`
}
```

### Repository Interface

`FileRepository` 定義數據訪問層 (DAL) 的抽象介面，對應 API 所需操作：

| Method | DB Operation | 對應 API |
|--------|--------------|----------|
| `Save(ctx, file)` | INSERT | `POST /files/upload` |
| `FindByUserID(ctx, userID, page, limit)` | SELECT ... WHERE user_id = ? LIMIT/OFFSET + COUNT | `GET /files`（含分頁） |
| `FindByID(ctx, fileID)` | SELECT ... WHERE id = ? | `DELETE /files/{file_id}` 權限檢查 |
| `Delete(ctx, fileID)` | DELETE ... WHERE id = ? | `DELETE /files/{file_id}` |

```go
type FileRepository interface {
    Save(ctx context.Context, file *File) error
    FindByUserID(ctx context.Context, userID string, page, limit int) ([]*File, int, error)
    FindByID(ctx context.Context, fileID string) (*File, error)
    Delete(ctx context.Context, fileID string) error
}
```
