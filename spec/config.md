# 設定檔規格

遵循 [The Twelve-Factor App](https://12factor.net/config)，採用**環境變數**作為設定來源，搭配 `.env` 檔案供本地開發使用。

## 必要環境變數

| 變數名稱 | 說明 | 範例 |
| -------- | ---- | ---- |
| `DATABASE_URL` | PostgreSQL 連線字串 | `postgres://user:pass@localhost:5432/aidms?sslmode=disable` |
| `JWT_SECRET` | JWT 簽章密鑰 | 至少 32 字元的隨機字串 |
| `STORAGE_ROOT` | 使用者檔案儲存根目錄 | `./storage/uploads` |
| `SERVER_PORT` | HTTP 監聽埠 | `8080` |

## 可選環境變數

| 變數名稱 | 說明 | 預設值 |
| -------- | ---- | ------ |
| `STORAGE_HOST_PATH` | Workload 容器 bind mount 用；Server 在 Docker 時設為 host 端路徑 | 未設時等同 `STORAGE_ROOT` |
| `LOG_LEVEL` | 日誌等級 | `info` |
| `WORKER_POOL_SIZE` | Job Worker 數量 (ADR 009-1) | `4` |

## .env 範例

```env
DATABASE_URL=postgres://aidms:aidms@localhost:5432/aidms?sslmode=disable
JWT_SECRET=your-super-secret-key-at-least-32-chars
STORAGE_ROOT=./storage/uploads
SERVER_PORT=8080
```

> **注意**：`.env` 應加入 `.gitignore`，切勿將含密鑰的設定檔提交版控。
