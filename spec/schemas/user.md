# Users Table Schema

## DDL (PostgreSQL)

```sql
CREATE TABLE users (
    id VARCHAR(36) PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_username ON users (username);
```

## Column Reference

| Column         | Type         | Description                          |
| -------------- | ------------ | ------------------------------------ |
| `id`           | VARCHAR(36)  | 系統生成的 UUID，對應 JWT 中的 user_id |
| `username`     | VARCHAR(255) | 登入帳號，唯一                       |
| `password_hash`| VARCHAR(255) | 密碼雜湊值（bcrypt 等）               |
| `created_at`   | TIMESTAMPTZ | 建立時間（含時區）                   |

## Indexes

| Index             | Column     | Purpose                    |
| ----------------- | ---------- | -------------------------- |
| `idx_users_username` | `username` | `POST /login` 查詢身分核實 |

---

## Seed Data

本系統不實作註冊功能，使用者由管理員透過 DB 初始化建立。建議在 `configs/` 或 `deployments/` 下提供 Seed 腳本或 migration，於首次部署時插入預設帳號。

**範例 Seed (SQL)**：

```sql
-- 密碼為 "password123" 的 bcrypt 雜湊（實作時請依實際演算法調整）
-- ID 採用 UUIDv4 格式
INSERT INTO users (id, username, password_hash) VALUES
    ('550e8400-e29b-41d4-a716-446655440000', 'admin', '$2a$10$...'),
    ('6ba7b810-9dad-11d1-80b4-00c04fd430c8', 'user1', '$2a$10$...');
```

**建議**：Seed 腳本應支援環境變數注入（如 `ADMIN_PASSWORD`），避免將明文密碼寫入版控。

---

## Domain Design (Go)

本節說明如何依 DB Schema 設計 Domain 層的實體與 Repository 介面。使用者由管理員透過 Seed Data 建立，不對外輸出完整實體。

### Schema → Entity Mapping

| DB Column      | Go Field     | 說明                         |
| -------------- | ------------ | ---------------------------- |
| `id`           | `ID`         | 內部使用，JWT 核發時帶入     |
| `username`     | `Username`   | 登入時比對                   |
| `password_hash`| `PasswordHash` | 內部驗證，絕不輸出        |
| `created_at`   | `CreatedAt`  | 內部使用                     |

### Entity

```go
package domain

import "time"

// User 代表系統中的使用者實體
type User struct {
    ID           string    `db:"id"`
    Username     string    `db:"username"`
    PasswordHash string    `db:"password_hash"`
    CreatedAt    time.Time `db:"created_at"`
}
```

### Repository Interface

`UserRepository` 定義數據訪問層 (DAL) 的抽象介面，對應登入所需操作：

| Method                    | DB Operation                    | 對應 API      |
| ------------------------- | ------------------------------- | ------------- |
| `FindByUsername(ctx, username)` | SELECT ... WHERE username = ? | `POST /login` |

```go
type UserRepository interface {
    FindByUsername(ctx context.Context, username string) (*User, error)
}
```
