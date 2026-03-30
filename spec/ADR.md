# Architecture Decision Records (ADR)

---

## 一、系統架構與基礎架構

### ADR 001：系統分層與框架選擇

**決策**：採用 Clean Architecture，以 Gin 框架處理 HTTP 層，強制依賴注入。

**原因**：業務邏輯（Service）不依賴特定資料庫或 Web 框架，達成模組間低耦合與抽象資料存取層。

---

### ADR 002：Gin 框架與層級隔離

**決策**：Handler 層使用 `*gin.Context`，但 Service 與 Repository 層僅接收 `context.Context` 與具體參數。

- `ShouldBindJSON` 驗證 Payload，防止非法參數進入 Service。
- 中間件採洋蔥模型（`c.Next()` / `c.Abort()`），處理日誌、認證、錯誤。

**規則**：`*gin.Context` 不可傳入 Service 或 Repository，避免核心邏輯被框架綁架。

---

### ADR 003：資料持久化與分離儲存

**決策**：實體檔案（CSV、JSON 等）存於宿主機檔案系統；Metadata、容器狀態與 Job 狀態存於關聯式資料庫。

**原因**：兼顧高效能磁碟 I/O 與資料庫的一致性查詢。

---

### ADR 004：採用 PostgreSQL

**決策**：選定 PostgreSQL，捨棄 SQLite。

- `SELECT ... FOR UPDATE`：Per-Container 並發控制所需的 Row-Level Lock。
- `JSONB`：`containers.command` 原生支援半結構化儲存。
- `TIMESTAMPTZ`：所有時間欄位含時區。

**原因**：高併發寫入與嚴格並發控制需求。

**權衡**：增加外部依賴。緩解：`docker compose up -d postgres` 一鍵啟動。

---

## 二、認證與安全

### ADR 005：JWT 認證與資源隔離

**決策**：JWT 認證中間件。所有操作由後端從 Token 解析 `user_id` 決定資源歸屬。

**原因**：防止越權存取。檔案物理路徑由後端管控，防範路徑穿越攻擊。

**補充**：不實作註冊功能，使用者由 Migration Seed 建立。

---

## 三、容器設計

### ADR 006：Image 白名單

**決策**：使用者只能傳遞預定義的 `environment` 標籤（如 `python-3.10`），由後端映射到真實 Docker Image。

**原因**：防止拉取惡意鏡像，確保系統環境穩定。

---

### ADR 007：預定義環境

**決策**：兩組環境映射：

| Environment   | Docker Image       | 定位         |
| ------------- | ------------------ | ------------ |
| `python-3.10` | `python:3.10-slim` | 輕量腳本測試 |
| `ubuntu-base` | `ubuntu:22.04`     | 通用 OS 環境 |

**權衡**：目前不支援 Node.js、Java 等。擴充需修改設定並重新部署。

---

### ADR 008：隱式綁定掛載

**決策**：建立容器時自動將使用者資料夾掛載至容器內 `/workspace`。

**原因**：簡化 API。使用者只需知道上傳的檔案在容器的 `/workspace` 裡。

---

### ADR 009：全面非同步容器操作

**決策**：容器的 Create / Start / Stop / Delete 皆回傳 `202 Accepted` + Job ID。

**原因**：避免 Docker Daemon 處理耗時導致 HTTP Timeout，並為優雅關閉提供狀態恢復基礎。

---

### ADR 010：Worker Pool 與重啟恢復

**決策**：In-process Worker Pool（Goroutines + Channel），不引入外部 MQ。

- **執行模型**：固定數量 Worker 從 Channel 消費 Job；Handler 寫入 DB 後推入 Channel，立即回傳 202。
- **重啟恢復**：啟動時將 `RUNNING` 狀態的 Job 標記為 `FAILED`（System interrupted），並將 `PENDING` 的 Job 重新入隊。

**原因**：單一 binary 即可運行，無需額外基礎設施。

---

### ADR 011：並發控制

**決策**：對容器與檔案資源採用相同的雙層並發控制機制：

1. **Application-level mutex**：`sync.Map` 儲存 Per-Resource `*sync.Mutex`（`containerMu` / `fileMu`），確保同一資源的操作在單一 process 內互斥。
2. **Database-level lock**：`SELECT ... FOR UPDATE` Row-Level Lock，透過 `TxManager.WithTx` 在交易內執行，確保多 process / 多 replica 場景下的一致性。

**鎖粒度**：Per-Resource。刪除容器 X 不會阻塞啟動容器 Y；刪除檔案 A 不會阻塞刪除檔案 B。

**適用範圍**：

| 資源 | 受保護操作 | Mutex 欄位 | Guard 方法 |
|------|-----------|------------|-----------|
| Container | Start / Stop / Delete | `containerMu` | `containerLock` + `findContainerForGuard` |
| File | Delete | `fileMu` | `fileLock` + `findFileForGuard` |

**原因**：確保同一資源在多請求下的一致性，避免 Docker / 檔案系統與 DB 狀態脫鉤。雙層鎖設計使系統在無 TxManager 時（如記憶體 Repo 測試）仍可正確運作。

---

## 四、支援功能

### ADR 012：Job 保留策略

**決策**：Job 紀錄保留 7 天。背景排程定期清理已完成（SUCCESS/FAILED）且 `completed_at` 超過 7 天的 Job。過期 Job 查詢回傳 `404`。

**原因**：避免 Job 表無限增長，7 天足以涵蓋除錯與追蹤需求。

---

### ADR 013：檔案上傳限制

**決策**：

- 單檔上限 50MB，超過回傳 `413`。
- 以 Magic Bytes（前 512 Bytes）驗證 MIME，不信任副檔名。
- 白名單：`image/jpeg`, `image/png`, `text/csv`, `application/json`, `text/plain`，其餘回傳 `415`。
- 實體檔案以 `{filename}_{uuid}.{ext}` 命名儲存（如 `test_{uuid}.png`），保留副檔名供編輯器識別；原始檔名僅存 DB，防範路徑穿越。

**權衡**：50MB 無法滿足大型資料集。未來可改採 Pre-signed URL + S3 分塊上傳。

---

## 五、基礎設施與介面規範

### ADR 014：強制 context.Context

**決策**：所有 Repository 與 Service 方法第一個參數為 `ctx context.Context`。

**原因**：支援 Timeout 傳遞、Graceful Shutdown Cancellation，以及 Go 社群慣例。

---

### ADR 015：優雅關閉

**決策**：攔截 `SIGINT` / `SIGTERM`，依序：

1. **HTTP Server 關閉**：`http.Server.Shutdown(ctx)`，給予 10 秒緩衝。
2. **Worker Pool 撤退**：停止接收新 Job，等待執行中 Worker 完成。Channel 內未消費的 PENDING Job 留在 DB。
3. **重啟恢復**：下次啟動時 RUNNING → FAILED，PENDING 重新入隊（見 ADR 010）。

---

## 六、測試策略

### ADR 016：單元測試覆蓋範圍

**決策**：單元測試集中於 Service 層、Worker、pkg 工具包、Middleware 的業務邏輯與邊界行為，不追求 100% 覆蓋率。

**刻意不覆蓋的區域**：

| 區域 | 原因 |
|---|---|
| Repository（Postgres） | 純 SQL 呼叫，無分支邏輯。由整合測試（testcontainers）驗證。 |
| Repository（Docker） | 薄 adapter，行為取決於 Docker daemon。由 E2E 測試覆蓋。 |
| Handler（路由函式） | 邏輯密度極低（解析→呼叫 Service→回傳 JSON）。整合測試已做 HTTP 契約驗證。 |
| Router / Worker 生命週期 | 靜態接線或 goroutine 管理，適合整合測試。 |

**刻意覆蓋的區域**：

- **FileService.Upload**：MIME 偵測、50MB 邊界、path traversal 防護、DB 失敗時檔案清理。
- **ContainerService.Start/Stop/Delete**：狀態機守衛（六種 409 觸發路徑）、per-container 鎖。
- **Worker.process***：Docker 特殊錯誤處理（NotModified、NotFound）、retry 耗盡。
- **JWT / AppError / AuthMiddleware**：安全敏感行為（過期 Token、algorithm confusion、錯誤碼映射）。

**原則**：單元測試目標是固定行為（pin behavior），而非衝高覆蓋率。有分支、有邊界、有副作用清理的程式碼才值得用 mock 隔離測試。
