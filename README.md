# Go Container Manager

容器管理系統的 RESTful API 服務。使用者登入後可上傳資料（CSV、JSON、圖片等）到專屬資料夾，透過 API 建立 Docker 容器並自動掛載該使用者的資料夾至 `/workspace`，可選擇預定義的容器環境（`python-3.10` 或 `ubuntu-base`）。所有容器操作皆為非同步，回傳 Job ID 供查詢進度。

技術棧：Go + Gin + PostgreSQL + Docker SDK。

> **理解設計決策**：建議閱讀 [`spec/`](spec/) 目錄，涵蓋架構決策（ADR）、API 規格、Schema 設計等。入口：[`spec/README.md`](spec/README.md)。

---

## Prerequisites

- **Go 1.25+**
- **Docker & Docker Compose**（PostgreSQL 與容器操作）
- **Docker socket 存取權限**（容器操作需要）

---

## Quick Start

### 選項 A：Docker Compose（推薦，避免環境差異）

一次啟動 PostgreSQL 與 Server：

```bash
cd go-container-manager
cp .env.example .env   # 可調整 JWT_SECRET 等
make docker-up         # 或：STORAGE_HOST_PATH=$(pwd)/storage/uploads docker compose up -d
```

> **檔案上傳**：需設定 `STORAGE_HOST_PATH` 為 host 端的 storage 絕對路徑，否則 workload 容器無法掛載。`make docker-up` 會自動設定。

確認服務就緒：

```bash
docker compose ps      # postgres: healthy, server: running
curl http://127.0.0.1:8080/api/v1/health
```

伺服器啟動時會自動執行 database migrations（含 Seed 使用者）。停止服務：`docker compose down`。

### 選項 B：本機執行

**1. 啟動 PostgreSQL**

```bash
docker compose up -d postgres
```

確認 DB 就緒：`docker compose ps`（STATUS 應為 healthy）

**2. 設定環境變數**

```bash
cp .env.example .env
# 預設值已對應 docker-compose 設定（user=aidms, pass=aidms, db=aidms），通常不需修改
```

**3. 啟動伺服器**

```bash
go run ./cmd/server
```

**開發模式（Hot Reload）：**

```bash
go install github.com/air-verse/air@latest
air
```

伺服器啟動時會自動執行 database migrations（含 Seed 使用者），無需手動操作。

### Seed 使用者

| 帳號  | 密碼        | 用途             |
| ----- | ----------- | ---------------- |
| user1 | password123 | 一般使用者       |
| user2 | password123 | 跨使用者隔離測試 |
| admin | password123 | 管理者           |

---

## Environment Variables

| 變數                | 必要 | 說明                                                           | 預設 / 範例                                                   |
| ------------------- | ---- | -------------------------------------------------------------- | ------------------------------------------------------------- |
| `DATABASE_URL`      | ✅   | PostgreSQL 連線字串                                            | `postgres://aidms:aidms@localhost:5432/aidms?sslmode=disable` |
| `JWT_SECRET`        | ✅   | JWT 簽章密鑰（≥32 字元）                                       | `your-super-secret-key-at-least-32-chars`                     |
| `STORAGE_ROOT`      | ✅   | 使用者檔案儲存根目錄                                           | `./storage/uploads`                                           |
| `STORAGE_HOST_PATH` |      | Workload 容器 bind mount 用；Server 在 Docker 時設為 host 路徑 | 未設時等同 `STORAGE_ROOT`                                     |
| `SERVER_PORT`       | ✅   | HTTP 監聽埠                                                    | `8080`                                                        |
| `LOG_LEVEL`         |      | 日誌等級                                                       | `info`                                                        |
| `WORKER_POOL_SIZE`  |      | Job Worker 數量                                                | `4`                                                           |

---

## API 概覽

Base URL：`/api/v1`

| Method   | Path                               | 說明               | 認證 |
| -------- | ---------------------------------- | ------------------ | ---- |
| `GET`    | `/health`                          | 健康檢查           | 否   |
| `GET`    | `/environments`                    | 環境白名單         | 否   |
| `POST`   | `/login`                           | 取得 JWT Token     | 否   |
| `POST`   | `/files/upload`                    | 上傳檔案           | 是   |
| `GET`    | `/files`                           | 檔案清單（分頁）   | 是   |
| `DELETE` | `/files/{file_id}`                 | 刪除檔案           | 是   |
| `GET`    | `/containers`                      | 容器清單（分頁）   | 是   |
| `POST`   | `/containers`                      | 建立容器（非同步） | 是   |
| `GET`    | `/containers/{container_id}`       | 查詢容器狀態       | 是   |
| `POST`   | `/containers/{container_id}/start` | 啟動容器（非同步） | 是   |
| `POST`   | `/containers/{container_id}/stop`  | 停止容器（非同步） | 是   |
| `DELETE` | `/containers/{container_id}`       | 刪除容器（非同步） | 是   |
| `GET`    | `/jobs/{job_id}`                   | 查詢 Job 狀態      | 是   |

所有受保護端點需在 Header 帶入 `Authorization: Bearer <token>`。

---

## API 文檔

### OpenAPI / Swagger

完整的 OpenAPI 3.0 規格在 `api/swagger/swagger.yaml`。可用以下方式閱讀：

```bash
# 方法 1：Swagger Editor（線上）
# 將 swagger.yaml 內容貼到 https://editor.swagger.io/

# 方法 2：VS Code 擴充
# 安裝 "OpenAPI (Swagger) Editor" 或 "Swagger Viewer" 擴充，直接在編輯器中預覽

# 方法 3：Swagger UI Docker（本地）
docker run -p 8081:8080 -e SWAGGER_JSON=/spec/swagger.yaml -v $(pwd)/api/swagger:/spec swaggerapi/swagger-ui
# 開啟 http://localhost:8081
```

### 手動測試 API 端點

專案提供 `api.http` 檔案，支援 **VS Code REST Client** 或 **JetBrains HTTP Client**：

1. 在 IDE 中開啟 `api.http`
2. 先執行 `POST /login` 取得 Token
3. 將 Token 貼到檔案頂部的 `@token` 變數
4. 依序點擊各請求旁的「Send Request」按鈕

預設 `@baseUrl = http://127.0.0.1:8080/api/v1`，適用 Docker Compose 與本機 server。**若 server 在本機其他埠或遠端**：修改檔案頂部 `@baseUrl`（例如 `http://localhost:9000/api/v1`）。

`api.http` 僅含**成功案例**（2xx），便於手動檢視 API 行為。**錯誤案例**（401、403、404、409、413、415）由 `make test-api`（`scripts/api_test.sh`）涵蓋。

---

## 設計文件

深入理解設計決策請閱讀 [`spec/`](spec/) 目錄（入口：[`spec/README.md`](spec/README.md)）：

| 文件                                           | 內容                                                                                                                              |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| [`spec/ADR.md`](spec/ADR.md)                   | **架構決策紀錄（ADR）**— 記錄每項重要設計決策的理由與權衡，包含系統分層、JWT 認證、非同步容器操作、並發控制、優雅關閉、測試策略等 |
| [`spec/structure.md`](spec/structure.md)       | 專案目錄結構與各模組職責                                                                                                          |
| [`spec/api.md`](spec/api.md)                   | 完整 API 規格（請求/回應格式、錯誤碼、分頁行為）                                                                                  |
| [`spec/config.md`](spec/config.md)             | 環境變數與設定規格（Twelve-Factor App）                                                                                           |
| [`spec/environments.md`](spec/environments.md) | 預定義環境白名單與 Docker Image 映射                                                                                              |
| `spec/schemas/`                                | 各資料表 Schema、Domain Entity 與 Repository 介面設計                                                                             |

---

## 專案架構

```
Handler (Gin) → Service (業務邏輯) → Repository (DB / Docker / 檔案系統)
      ↑                  ↑
     domain/ (實體 + 介面定義，無外部依賴)
```

```
go-container-manager/
├── cmd/server/           # 程式進入點
├── app/                  # DI 組裝（串接所有依賴）
├── internal/
│   ├── domain/           # 實體與介面定義
│   ├── handler/          # HTTP Handlers + Middleware
│   ├── service/          # 業務邏輯 + Worker Pool
│   └── repository/       # PostgreSQL / Docker SDK / 檔案系統
├── pkg/                  # 通用工具（JWT、Logger、Errors）
├── api/swagger/          # OpenAPI 3.0 文檔
├── migrations/           # SQL Migration 腳本
├── scripts/              # 測試與維運腳本
└── tests/integration/    # 整合測試
```

詳見 [`spec/structure.md`](spec/structure.md)。

---

## Migrations

Migrations 在伺服器啟動時**自動執行**。手動操作：

```bash
# 需安裝 migrate CLI
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

make migrate-up          # 執行所有 migration
make migrate-down        # 回滾所有 migration
make migrate-fix-dirty   # 修復 dirty database version
```

---

## 測試

本專案包含三層測試：

### 單元測試

針對 Service 層、Worker、Middleware、pkg 工具包的業務邏輯與邊界行為。

```bash
make test                # go test ./...
make test-coverage       # 含覆蓋率報告
```

### 整合測試

HTTP 契約測試，以 `spec/api.md` 為規格來源。透過 testcontainers 自動啟動 PostgreSQL，在同一行程內以 `httptest.NewServer` 執行完整 App，無需手動啟動外部服務。

```bash
make test-integration

# 或指定外部已啟動的 server
TEST_SERVER_URL=http://localhost:8080 go test ./tests/integration/... -tags=integration -v
```

| 測試檔案            | 對應範圍                                  |
| ------------------- | ----------------------------------------- |
| health_test.go      | 健康檢查、環境白名單                      |
| auth_test.go        | JWT 登入                                  |
| files_test.go       | 檔案上傳、列表、刪除、MIME 驗證           |
| containers_test.go  | 容器 CRUD、狀態機、並發控制、跨使用者隔離 |
| jobs_test.go        | 非同步 Job 查詢                           |
| consistency_test.go | CRUD 一致性驗證（刪除後無孤兒資料）       |

### E2E API 測試腳本

對已啟動的 server 執行完整 API 測試（含跨使用者隔離、容器生命週期）。需要 `curl`、`jq`、Docker。

**Docker Compose（預設）：**

```bash
docker compose up -d
docker compose stop server   # 釋放 DB 連線（migrate 需要）
make clean-test-env          # 清理 DB + 檔案
docker compose up -d         # 重新啟動 server
make test-api                # BASE_URL 預設 http://127.0.0.1:8080
```

**本機執行 server：**

```bash
make clean-test-env      # 清理後
# 啟動 server：go run ./cmd/server 或 air
make test-api            # 同上，127.0.0.1:8080 預設即適用
```

**自訂 Base URL**（server 在他處時）：

```bash
BASE_URL=http://my-host:8080 make test-api
```
