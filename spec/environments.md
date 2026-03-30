# 預定義容器環境 (Environment) 白名單

基於 [ADR 006](./ADR.md#adr-006容器映像檔image的白名單管理) 與 [ADR 007](./ADR.md#adr-007預定義容器運行環境-environment-的白名單選擇策略)，使用者僅能指定以下 `environment` 標籤，由後端映射至對應 Docker Image。

## 環境對照表

| Environment   | Docker Image       | 定位         |
| ------------- | ------------------ | ------------ |
| `python-3.10` | `python:3.10-slim` | 輕量腳本測試 |
| `ubuntu-base` | `ubuntu:22.04`     | 通用 OS 環境 |

## 說明

| Environment   | 說明 |
| ------------- | ---- |
| `python-3.10` | 輕量級 Python 環境（約 40–50 MB），適合快速驗證資料處理腳本（如讀取 CSV）。 |
| `ubuntu-base` | 無預設語言依賴的 Ubuntu，支援 Shell 腳本或二進位執行檔（如 C++ 編譯產物）。 |

## 實作建議

於 Service 層的 Image Registry（`configs/` 或程式內對照表）維護上述映射。傳入非白名單的 `environment` 時，回傳 `400 Bad Request`。
