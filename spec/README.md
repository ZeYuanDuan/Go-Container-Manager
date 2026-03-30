# AIDMS Container Manager — Specification

本目錄包含開發所需的規格與設計文件。

## 文件一覽

| 文件                                           | 說明                                          |
| ---------------------------------------------- | --------------------------------------------- |
| [api.md](./api.md)                             | RESTful API 規格（認證、檔案、容器、任務）    |
| [config.md](./config.md)                       | 環境變數與設定規格（Twelve-Factor App）       |
| [environments.md](./environments.md)           | 預定義 Environment 白名單與 Image 映射        |
| [structure.md](./structure.md)                 | 專案目錄結構與模組職責                        |
| [schemas/user.md](./schemas/user.md)           | Users 資料表 Schema、Seed 與 Domain 設計      |
| [schemas/files.md](./schemas/files.md)         | Files 資料表 Schema 與 Domain 設計            |
| [schemas/container.md](./schemas/container.md) | Containers 資料表 Schema 與 Domain 設計       |
| [schemas/job.md](./schemas/job.md)             | Jobs 資料表 Schema、保留策略與 Domain 設計    |
| [ADR.md](./ADR.md)                             | 架構決策紀錄（Architecture Decision Records） |

## 建議閱讀順序

1. **ADR.md** — 了解架構決策與設計考量
2. **structure.md** — 了解專案結構與分層
3. **api.md** — API 規格與行為
4. **schemas/** — 各實體的資料庫 Schema 與 Domain 介面

---

## 測試對應

| spec 章節     | 測試檔案             |
| ------------- | -------------------- |
| 1. 基礎 API   | health_test.go       |
| 2. 認證與授權 | auth_test.go         |
| 3. 檔案管理   | files_test.go        |
| 4. 容器管理   | containers_test.go   |
| 5. 非同步任務 | jobs_test.go         |
| 一致性驗證    | consistency_test.go  |

詳見 [tests/README.md](../tests/README.md)。

---

## 目錄結構示意

```
spec/
├── README.md           # 本文件（入口）
├── api.md              # API 規格
├── config.md           # 環境變數與設定
├── environments.md     # Environment 白名單與 Image 映射
├── structure.md        # 專案目錄結構
├── ADR.md              # 架構決策
└── schemas/            # 資料表與 Domain 規格
    ├── user.md         # Users 實體（含 Seed）
    ├── files.md        # Files 實體
    ├── container.md    # Containers 實體
    └── job.md          # Jobs 實體（含保留策略）
```
