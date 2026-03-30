# AIDMS Container Management API Specification

## Overview

| Property           | Value                           |
| ------------------ | ------------------------------- |
| **Base URL**       | `/api/v1`                       |
| **Authentication** | Bearer Token (JWT)              |
| **Auth Header**    | `Authorization: Bearer <token>` |

> **路徑規範**：本文檔所列路徑皆為相對於 Base URL 的路徑。完整 URL = Base URL + 路徑（例如 `POST /login` 的完整路徑為 `POST /api/v1/login`）。除登入與 Health 外，所有 API 皆需在 Header 帶入 `Authorization: Bearer <token>`。

> **ID 格式**：所有資源 ID（`file_id`、`container_id`、`job_id`、`user_id`）統一採用 **UUIDv4** 標準格式（如 `550e8400-e29b-41d4-a716-446655440000`）。

### 錯誤回應格式 (Error Response Format)

所有錯誤皆須使用以下標準格式回傳：

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message"
  }
}
```

| Field     | Description                               |
| --------- | ----------------------------------------- |
| `code`    | 機器可讀的錯誤代碼（如 `FILE_NOT_FOUND`） |
| `message` | 人類可讀的錯誤描述                        |

---

## Table of Contents

1. [基礎 API](#1-基礎-api)
2. [認證與授權](#2-認證與授權-authentication)
3. [檔案管理](#3-檔案管理-file-management)
4. [容器管理](#4-容器管理-container-operations)
5. [非同步任務追蹤](#5-非同步任務追蹤-job-status)

---

## API Quick Reference

| Method   | Path                               | Description                |
| -------- | ---------------------------------- | -------------------------- |
| `GET`    | `/health`                          | 健康檢查（無需認證）       |
| `GET`    | `/environments`                    | 取得環境白名單（無需認證） |
| `POST`   | `/login`                           | 獲取 JWT 存取權杖          |
| `POST`   | `/files/upload`                    | 上傳檔案                   |
| `GET`    | `/files`                           | 取得檔案清單               |
| `DELETE` | `/files/{file_id}`                 | 刪除檔案                   |
| `GET`    | `/containers`                      | 列出所有容器               |
| `POST`   | `/containers`                      | 創建容器                   |
| `GET`    | `/containers/{container_id}`       | 取得單一容器狀態           |
| `POST`   | `/containers/{container_id}/start` | 啟動容器                   |
| `POST`   | `/containers/{container_id}/stop`  | 停止容器                   |
| `DELETE` | `/containers/{container_id}`       | 刪除容器                   |
| `GET`    | `/jobs/{job_id}`                   | 查詢任務狀態               |

---

## 1. 基礎 API

### `GET /health`

健康檢查端點，供 K8s Liveness Probe 或 Load Balancer 使用。**無需認證**。

**Response** — `200 OK`

```json
{
  "status": "UP"
}
```

---

### `GET /environments`

取得後端預定義的環境白名單，供前端下拉選單等 UI 使用。**無需認證**。

**Response** — `200 OK`

```json
{
  "environments": ["python-3.10", "ubuntu-base"]
}
```

---

## 2. 認證與授權 (Authentication)

### `POST /login`

獲取存取權杖 (JWT)。實作中會決定後續操作的 `user_id`。

**Request**

| Header         | Value              |
| -------------- | ------------------ |
| `Content-Type` | `application/json` |

```json
{
  "username": "user1",
  "password": "password123"
}
```

**Response** — `200 OK`

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5c...",
  "expires_in": 3600
}
```

---

## 3. 檔案管理 (File Management)

系統根據 Token 中的 `user_id`，自動將檔案存入對應的物理路徑。

### `POST /files/upload`

上傳數據檔案或應用程式碼。

**Request**

| Header         | Value                 |
| -------------- | --------------------- |
| `Content-Type` | `multipart/form-data` |

| Field  | Type   | Description |
| ------ | ------ | ----------- |
| `file` | binary | 二進位檔案  |

**限制條件 (File Spec)**（詳見 [ADR 012](./ADR.md#adr-012檔案上傳限制與格式驗證)）

| 項目           | 限制                                                                                                       |
| -------------- | ---------------------------------------------------------------------------------------------------------- |
| 單檔大小上限   | 50 MB                                                                                                      |
| 允許 MIME Type | `image/jpeg`, `image/png`, `text/csv`, `application/json`, `text/plain`（嚴格白名單，以 Magic Bytes 驗證） |

**檔名衝突策略**：資料庫存儲原始檔名 (`original_filename`)，實體檔案路徑採 `{filename}_{uuid}.{ext}` 以確保唯一性並保留副檔名供編輯器識別（如 `data.csv` → `uploads/{user_id}/data_{uuid}.csv`，`test.png` → `uploads/{user_id}/test_{uuid}.png`）。

**Response** — `201 Created`

```json
{
  "file_id": "550e8400-e29b-41d4-a716-446655440000",
  "filename": "data.csv",
  "size_bytes": 1024576,
  "created_at": "2026-03-20T19:28:31Z"
}
```

| Field      | 說明                                                                                          |
| ---------- | --------------------------------------------------------------------------------------------- |
| `filename` | 使用者上傳時的**原始檔名**，對應 DB `original_filename`；實體儲存路徑採 `{filename}_{uuid}.{ext}`，不對外揭露 |

**Error** — `413 Payload Too Large`

```json
{
  "error": {
    "code": "FILE_TOO_LARGE",
    "message": "File size exceeds 50MB limit"
  }
}
```

**Error** — `415 Unsupported Media Type`

```json
{
  "error": {
    "code": "UNSUPPORTED_MIME_TYPE",
    "message": "File type not allowed. Allowed: image/jpeg, image/png, text/csv, application/json, text/plain"
  }
}
```

---

### `GET /files`

取得當前使用者的所有檔案清單。

**Query Parameters**

| Parameter | Type | Default | Description                                        |
| --------- | ---- | ------- | -------------------------------------------------- |
| `page`    | int  | 1       | 頁碼（從 1 開始）                                  |
| `limit`   | int  | 20      | 每頁筆數（超出 100 時後端自動 cap 為 100，不報錯） |

**分頁行為**：若 `page` 超出總頁數，回傳 `200 OK` 與空陣列 `[]`，並附上正確的 `total`。

**Response** — `200 OK`

```json
{
  "data": [
    {
      "file_id": "550e8400-e29b-41d4-a716-446655440000",
      "filename": "train_data.csv",
      "size_bytes": 10485760,
      "created_at": "2026-03-20T19:28:31Z"
    },
    {
      "file_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
      "filename": "model.py",
      "size_bytes": 4096,
      "created_at": "2026-03-20T19:35:12Z"
    }
  ],
  "total": 42
}
```

| Field        | Description                    |
| ------------ | ------------------------------ |
| `data`       | 檔案 Metadata 陣列             |
| `total`      | 符合條件的總筆數（不分頁）     |
| `file_id`    | 檔案 ID                        |
| `filename`   | 對應 DB 的 `original_filename` |
| `size_bytes` | 以 Byte 為單位                 |

---

### `DELETE /files/{file_id}`

刪除指定檔案。

| Path Parameter | Description |
| -------------- | ----------- |
| `file_id`      | 檔案 ID     |

**Response** — `204 No Content`

成功刪除，無回傳內容。

**Error** — `404 Not Found`

```json
{
  "error": {
    "code": "FILE_NOT_FOUND",
    "message": "File does not exist or has been deleted"
  }
}
```

**Error** — `403 Forbidden`

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to delete this file"
  }
}
```

---

## 4. 容器管理 (Container Operations)

> 所有耗時的容器操作皆採用**非同步 (Asynchronous)** 設計，回傳 `job_id` 供後續查詢。

### `GET /containers`

取得當前使用者所擁有的所有容器清單。

**Query Parameters**

| Parameter | Type | Default | Description                                        |
| --------- | ---- | ------- | -------------------------------------------------- |
| `page`    | int  | 1       | 頁碼（從 1 開始）                                  |
| `limit`   | int  | 20      | 每頁筆數（超出 100 時後端自動 cap 為 100，不報錯） |

**分頁行為**：若 `page` 超出總頁數，回傳 `200 OK` 與空陣列 `[]`，並附上正確的 `total`。

**Response** — `200 OK`

```json
{
  "data": [
    {
      "container_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
      "name": "my-training-task",
      "environment": "python-3.10",
      "status": "Running",
      "created_at": "2026-03-20T20:00:00Z"
    }
  ],
  "total": 5
}
```

**`status` 可能值**

| Value     | Description    |
| --------- | -------------- |
| `Created` | 已創建但未執行 |
| `Running` | 執行中         |
| `Exited`  | 已停止         |
| `Dead`    | 異常           |

---

### `POST /containers`

根據指定的環境標籤，為使用者創建獨立的容器實體，並在背景自動掛載該使用者的儲存資料夾。

**Request**

| Header         | Value              |
| -------------- | ------------------ |
| `Content-Type` | `application/json` |

```json
{
  "name": "my-training-task",
  "environment": "python-3.10",
  "command": ["python", "/workspace/train_model.py"]
}
```

| Field         | Description                |
| ------------- | -------------------------- |
| `name`        | 容器名稱                   |
| `environment` | 環境標籤，由後端白名單定義 |
| `command`     | 啟動時執行的指令陣列       |

**`environment` 可能值**（後端白名單，詳見 [environments.md](./environments.md)）

| Value         |
| ------------- |
| `python-3.10` |
| `ubuntu-base` |

**Response** — `202 Accepted`

```json
{
  "message": "Container creation job submitted",
  "job_id": "550e8400-e29b-41d4-a716-446655440001"
}
```

**Error** — `409 Conflict`

若同一使用者下容器名稱已存在（觸發 `UNIQUE (user_id, name)` 約束）：

```json
{
  "error": {
    "code": "CONTAINER_NAME_DUPLICATE",
    "message": "Container name already exists for this user"
  }
}
```

---

### `GET /containers/{container_id}`

查詢特定容器的詳細設定與當前運行狀態。

| Path Parameter | Description |
| -------------- | ----------- |
| `container_id` | 容器 ID     |

**Response** — `200 OK`

```json
{
  "container_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
  "name": "my-training-task",
  "environment": "python-3.10",
  "command": ["python", "/workspace/train_model.py"],
  "status": "Exited",
  "mount_path": "/workspace",
  "created_at": "2026-03-20T20:00:00Z"
}
```

**Error** — `404 Not Found`

```json
{
  "error": {
    "code": "CONTAINER_NOT_FOUND",
    "message": "Container does not exist"
  }
}
```

**Error** — `403 Forbidden`

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to access this container"
  }
}
```

---

### `POST /containers/{container_id}/start`

喚醒並啟動已創建或已停止的容器。依賴創建時定義的 `command`。

**Request Body**

無。

**Response** — `202 Accepted`

```json
{
  "message": "Container start job submitted",
  "job_id": "550e8400-e29b-41d4-a716-446655440002"
}
```

**Error** — `409 Conflict`

若容器狀態已是 `Running`，或正有其他 Job 正在操作該容器，觸發並發控制保護。

```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Container is already running or another operation is in progress"
  }
}
```

**Error** — `404 Not Found`

```json
{
  "error": {
    "code": "CONTAINER_NOT_FOUND",
    "message": "Container does not exist"
  }
}
```

**Error** — `403 Forbidden`

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to access this container"
  }
}
```

---

### `POST /containers/{container_id}/stop`

停止運行中的容器。

**Response** — `202 Accepted`

```json
{
  "message": "Container stop job submitted",
  "job_id": "550e8400-e29b-41d4-a716-446655440003"
}
```

**Error** — `409 Conflict`

若容器尚未啟動或已停止。

```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Container is not running or already stopped"
  }
}
```

**Error** — `404 Not Found`

```json
{
  "error": {
    "code": "CONTAINER_NOT_FOUND",
    "message": "Container does not exist"
  }
}
```

**Error** — `403 Forbidden`

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to access this container"
  }
}
```

---

### `DELETE /containers/{container_id}`

刪除指定的容器資源。

**刪除策略**：**必須先 Stop 才能 Delete**。容器狀態為 `Running` 時，本 API 將回傳 `409 Conflict`。系統不提供強制刪除，以確保資源狀態機的單向性與安全性。

**Response** — `202 Accepted`

```json
{
  "message": "Container delete job submitted",
  "job_id": "550e8400-e29b-41d4-a716-446655440004"
}
```

**Error** — `409 Conflict`

若容器狀態為 `Running`，拒絕刪除操作。請先呼叫 `POST /containers/{container_id}/stop` 停止容器後再刪除。

```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Container must be stopped before deletion. Call POST /containers/{container_id}/stop first"
  }
}
```

**Error** — `404 Not Found`

```json
{
  "error": {
    "code": "CONTAINER_NOT_FOUND",
    "message": "Container does not exist"
  }
}
```

**Error** — `403 Forbidden`

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to delete this container"
  }
}
```

---

## 5. 非同步任務追蹤 (Job Status)

此組 API 用於滿足「設計並實作長耗時任務的非同步處理機制，例如回傳 Job ID 供查詢狀態」的進階要求。

### `GET /jobs/{job_id}`

輪詢 (Polling) 查詢非同步任務的執行結果。**權限**：僅能查詢自己建立的 Job（比對 Token 中的 `user_id` 與 Job 的 `user_id`），否則回傳 `403 Forbidden` 或 `404 Not Found`（為避免資訊洩漏，建議回傳 404）。

**Response** — `200 OK`

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440005",
  "target_resource_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
  "type": "CREATE_CONTAINER",
  "status": "SUCCESS",
  "error_message": null,
  "created_at": "2026-03-20T20:01:00Z",
  "completed_at": "2026-03-20T20:01:05Z"
}
```

**`type` 可能值**

| Value              |
| ------------------ |
| `CREATE_CONTAINER` |
| `START_CONTAINER`  |
| `STOP_CONTAINER`   |
| `DELETE_CONTAINER` |

**`status` 可能值**

| Value     | Description                                                         |
| --------- | ------------------------------------------------------------------- |
| `PENDING` | 任務已排隊，尚未執行                                                |
| `RUNNING` | 任務正在背景執行中                                                  |
| `SUCCESS` | 任務順利完成                                                        |
| `FAILED`  | 任務失敗（`error_message` 會有具體原因，例如 Docker Daemon 無回應） |

**Error** — `404 Not Found`

任務不存在、已過期被清理、或屬於其他使用者（為避免資訊洩漏，不區分原因，參見 [ADR 011](./ADR.md#adr-011job-保留策略)）。

```json
{
  "error": {
    "code": "JOB_NOT_FOUND",
    "message": "Job does not exist or has expired"
  }
}
```
