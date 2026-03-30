//go:build integration

// auth_test.go — 認證端點測試
//
// 測試範圍：
//   - POST /login 正確帳密 → 200 + JWT token + expires_in
//   - POST /login 錯誤密碼 → 401 + 標準錯誤格式
//   - POST /login 缺少 body → 400（或 422）+ 標準錯誤格式
//
// 對應 spec/api.md §Auth。不測試註冊（使用者由 Migration Seed 建立）。

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	appjwt "github.com/alsonduan/go-container-manager/pkg/jwt"
)

// 驗證正確帳密登入：回傳 200、JSON 包含非空 token 與正整數 expires_in。
// Seed 使用者為 user1/password123（Migration 000005）。
func TestLogin_ValidCredentials_ReturnsToken(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"username": "user1",
		"password": "password123",
	})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/login", "", bytes.NewReader(body), "application/json")
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Token == "" {
		t.Error("token: expected non-empty")
	}
	if out.ExpiresIn <= 0 {
		t.Errorf("expires_in: expected positive, got %d", out.ExpiresIn)
	}
}

// 驗證錯誤密碼：回傳 401，且錯誤回應符合標準格式 {"error":{"code":"...","message":"..."}}。
func TestLogin_InvalidPassword_Returns401(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"username": "user1",
		"password": "wrongpassword",
	})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/login", "", bytes.NewReader(body), "application/json")
	assertStatus(t, resp, http.StatusUnauthorized)
	if len(bodyBytes) > 0 {
		assertErrorFormat(t, bodyBytes)
	}
}

// 驗證缺少 request body：回傳 400 或 422，且錯誤回應符合標準格式。
// 接受兩種狀態碼是因為不同 JSON binding 實作可能回傳不同錯誤碼。
func TestLogin_MissingBody_Returns400(t *testing.T) {
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/login", "", bytes.NewReader(nil), "")
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 400 or 422", resp.StatusCode)
	}
	if len(bodyBytes) > 0 {
		assertErrorFormat(t, bodyBytes)
	}
}

// 驗證過期 Token：使用已過期的 JWT 存取受保護端點，回傳 401 UNAUTHORIZED。
func TestAuth_ExpiredToken_Returns401(t *testing.T) {
	// defaultJWTSecret from app/handler.go — used when JWT_SECRET env is not set
	const jwtSecret = "test-secret-key-at-least-32-characters-long"
	token, err := appjwt.GenerateToken("fake-user-id", jwtSecret, -time.Second)
	if err != nil {
		t.Fatalf("generate expired token: %v", err)
	}
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files", token)
	assertStatus(t, resp, http.StatusUnauthorized)
	assertErrorCode(t, bodyBytes, "UNAUTHORIZED")
}

// 驗證無效格式 Token：使用亂碼 JWT，回傳 401 UNAUTHORIZED。
func TestAuth_MalformedToken_Returns401(t *testing.T) {
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files", "not.a.real.jwt")
	assertStatus(t, resp, http.StatusUnauthorized)
	assertErrorCode(t, bodyBytes, "UNAUTHORIZED")
}

// 驗證錯誤 Authorization scheme：使用 Basic 而非 Bearer，回傳 401 UNAUTHORIZED。
func TestAuth_WrongScheme_Returns401(t *testing.T) {
	// Manually set Authorization header with Basic scheme
	url := baseURL + "/api/v1/files"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Basic dXNlcjE6cGFzcw==")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", resp.StatusCode)
	}
}
