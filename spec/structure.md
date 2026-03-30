# 專案目錄結構

```
go-container-manager/
├── cmd/
│   └── server/
│       └── main.go          # 程式進入點：初始化 DI、資料庫連線、啟動 HTTP Server
├── app/
│   └── handler.go           # DI 組裝：串接所有 Repository → Service → Handler → Router
├── internal/
│   ├── domain/              # 領域模型與介面定義（核心抽象層，遵循 ISP）
│   │   ├── user.go          # User 實體 + UserRepository 介面
│   │   ├── container.go     # Container 實體 + ContainerRepository + ContainerRuntime 介面
│   │   ├── file.go          # File 實體 + FileRepository 介面
│   │   ├── job.go           # Job 實體 + JobRepository 介面 + 非同步任務狀態定義
│   │   └── errors.go        # 領域層哨兵錯誤 (ErrNotFound, ErrDuplicate 等)
│   ├── handler/             # API 傳輸層 (HTTP Handlers)
│   │   ├── middleware/      # 中間件：認證 (JWT)、日誌、Request Body 上限
│   │   ├── router.go        # Gin 路由定義與中間件串接
│   │   ├── response.go      # 統一回應格式與錯誤包裝
│   │   ├── health_hdl.go    # GET /health, GET /environments
│   │   ├── auth_hdl.go      # POST /login
│   │   ├── container_hdl.go # 容器管理 API
│   │   ├── file_hdl.go      # 檔案上傳 API
│   │   └── job_hdl.go       # Job 狀態查詢 API
│   ├── service/             # 業務邏輯層（DI、並發控制）
│   │   ├── auth_svc.go      # 登入邏輯、JWT 核發
│   │   ├── container_svc.go # 容器生命週期、Per-Container 並發控制
│   │   ├── file_svc.go      # 檔案上傳邏輯、MIME 驗證
│   │   ├── job_svc.go       # 非同步任務查詢
│   │   ├── worker.go        # Job Worker Pool：Goroutine 池消費 Channel，執行容器操作
│   │   └── job_cleaner.go   # 過期 Job 定期清理（7 天保留策略）
│   └── repository/          # 基礎設施實作層
│       ├── postgres/         # PostgreSQL 具體實現（含 TxManager）
│       ├── docker/           # Docker SDK 容器操作
│       └── localfile/        # 本地檔案系統操作
├── pkg/                      # 通用工具包
│   ├── jwt/                  # Token 產生與驗證
│   ├── logger/               # 結構化日誌（slog + JSON）
│   └── errors/               # 標準錯誤碼定義（與 API error.code 對應）
├── api/
│   └── swagger/
│       └── swagger.yaml      # OpenAPI 3.0 API 文檔
├── migrations/               # golang-migrate 遷移腳本（執行順序：users → files → containers → jobs → seed）
├── scripts/                  # 測試與維運腳本
│   ├── api_test.sh           # E2E API 測試腳本（curl + jq）
│   └── clean_test_env.sh     # 清理測試環境（Docker 容器 + DB + 檔案）
├── tests/
│   └── integration/          # 整合測試（HTTP 契約測試，testcontainers + httptest）
├── storage/                  # [由系統管理] 使用者檔案存放根目錄（依 STORAGE_ROOT 設定）
├── api.http                  # 手動測試（成功案例）；錯誤案例見 scripts/api_test.sh
├── docker-compose.yml        # PostgreSQL 容器定義
├── Dockerfile                # 多階段建置（builder + runtime）
├── .env.example              # 環境變數範例
├── go.mod                    # 依賴管理
├── Makefile                  # 常用指令（test, test-integration, test-api, migrate-*）
└── README.md                 # 系統架構說明與安裝流程
```
