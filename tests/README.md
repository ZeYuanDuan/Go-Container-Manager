# AIDMS Container Manager — Tests

## 整合測試 (Integration Tests)

純 HTTP 契約測試，以 [spec/api.md](../spec/api.md) 為規格來源，不依賴任何實作細節。

### 執行方式

```bash
# 整合測試（預設使用 in-process server + testcontainers PostgreSQL）
make test-integration

# 指定外部 server（需先啟動服務 + DB + Seed）
TEST_SERVER_URL=http://localhost:8080 go test ./tests/integration/... -tags=integration -v

# 單元測試（不含 integration）
make test
```

### 依賴

- **In-process 模式**：自動透過 testcontainers 啟動 PostgreSQL，在同一行程內以 `httptest.NewServer` 執行完整 App，無需手動啟動任何外部服務
- **外部 server 模式**：需預先啟動 API 服務（含 PostgreSQL + Docker + Seed 資料：user1/password123、admin/password123）

### 測試對應

| spec 章節     | 測試檔案             |
| ------------- | -------------------- |
| 1. 基礎 API   | health_test.go       |
| 2. 認證與授權 | auth_test.go         |
| 3. 檔案管理   | files_test.go        |
| 4. 容器管理   | containers_test.go   |
| 5. 非同步任務 | jobs_test.go         |
| 一致性驗證    | consistency_test.go  |
