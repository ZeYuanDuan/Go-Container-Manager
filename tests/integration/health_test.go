//go:build integration

// health_test.go — 健康檢查與環境白名單端點測試
//
// 測試範圍：
//   - GET /health：驗證服務存活探針（Liveness Probe）回傳 200 + {"status":"UP"}
//   - GET /environments：驗證環境白名單回傳完整清單（python-3.10, ubuntu-base）
//
// 這兩個端點皆為公開端點（無需 JWT），是最基礎的冒煙測試。

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 驗證健康檢查端點：無需認證、回傳 200、JSON 包含 status:"UP"。
// 對應 spec/api.md §Health — 供 K8s Liveness Probe 或 Load Balancer 使用。
func TestHealth_ReturnsUP(t *testing.T) {
	resp, body := doRequestNoBody(t, http.MethodGet, "/api/v1/health", "")
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Status != "UP" {
		t.Errorf("status: got %q, want UP", out.Status)
	}
}

// 驗證環境白名單端點：無需認證、回傳 200、JSON 包含 environments 陣列。
// 確認陣列至少包含 "python-3.10"。
// 對應 spec/api.md §Environments — 供前端下拉選單使用。
func TestEnvironments_ReturnsArray(t *testing.T) {
	resp, body := doRequestNoBody(t, http.MethodGet, "/api/v1/environments", "")
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Environments []string `json:"environments"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Environments == nil {
		t.Error("environments: expected non-nil array")
	}
	hasPython := false
	for _, e := range out.Environments {
		if e == "python-3.10" {
			hasPython = true
			break
		}
	}
	if !hasPython {
		t.Errorf("environments: expected to contain python-3.10, got %v", out.Environments)
	}
}

// 驗證環境白名單包含所有預期環境：python-3.10 和 ubuntu-base 都應存在。
// 此測試確保兩種預定義環境都在白名單中。
func TestEnvironments_ContainsBothEnvs(t *testing.T) {
	resp, body := doRequestNoBody(t, http.MethodGet, "/api/v1/environments", "")
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Environments []string `json:"environments"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	expected := map[string]bool{"python-3.10": false, "ubuntu-base": false}
	for _, e := range out.Environments {
		if _, ok := expected[e]; ok {
			expected[e] = true
		}
	}
	for env, found := range expected {
		if !found {
			t.Errorf("environments: missing %q, got %v", env, out.Environments)
		}
	}
}
