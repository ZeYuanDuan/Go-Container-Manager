//go:build integration

// setup_test.go — 整合測試基礎設施
//
// 提供兩種執行模式：
//   1. In-process 模式（預設）：透過 testcontainers 啟動 PostgreSQL，
//      使用 httptest.NewServer 在同一行程內啟動 API server。
//      不需要外部 PostgreSQL，適合 CI/CD 與本地開發。
//   2. External 模式：設定 TEST_SERVER_URL 環境變數指向已啟動的 server，
//      適合測試已部署的完整環境（需 PostgreSQL + Docker + Seed 資料）。
//
// 共用工具函式：
//   - mustLogin：登入並回傳 JWT Token，失敗則 Fatal
//   - doRequest / doRequestNoBody：發送 HTTP 請求並回傳 response + body
//   - assertStatus / assertJSONHas / assertErrorCode / assertErrorFormat：斷言輔助
//   - createMultipartFile / uploadFile：檔案上傳輔助

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alsonduan/go-container-manager/app"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var baseURL string

// TestMain 是整合測試的進入點，負責環境初始化與清理。
// In-process 模式：啟動 testcontainers PostgreSQL → 設定環境變數 → 建立 App → httptest server。
// External 模式：直接使用 TEST_SERVER_URL，跳過所有初始化。
func TestMain(m *testing.M) {
	var cleanup func()
	if url := os.Getenv("TEST_SERVER_URL"); url != "" {
		baseURL = strings.TrimSuffix(url, "/")
	} else {
		// Start PostgreSQL via testcontainers for in-process integration tests
		ctx := context.Background()
		pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
			postgres.WithDatabase("aidms_test"),
			postgres.WithUsername("test"),
			postgres.WithPassword("test"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
		)
		if err != nil {
			panic("start postgres: " + err.Error())
		}
		connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			panic("postgres connection string: " + err.Error())
		}
		os.Setenv("DATABASE_URL", connStr)

		tmpDir, err := os.MkdirTemp("", "aidms-test-*")
		if err != nil {
			panic("create temp dir: " + err.Error())
		}
		os.Setenv("STORAGE_ROOT", tmpDir)

		a := app.NewHandler()
		server := httptest.NewServer(a.Handler)
		baseURL = server.URL
		cleanup = func() {
			a.Stop()
			server.Close()
			_ = os.RemoveAll(tmpDir)
			_ = pgContainer.Terminate(ctx)
		}
	}
	code := m.Run()
	if cleanup != nil {
		cleanup()
	}
	os.Exit(code)
}

// mustLogin 登入指定使用者並回傳 JWT Token。登入失敗則直接 Fatal 中止測試。
func mustLogin(t *testing.T, user, pass string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/login", "", bytes.NewReader(body), "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("login response invalid: %v", err)
	}
	return out.Token
}

// doRequest 發送 HTTP 請求並回傳 response 與 body bytes。支援設定 Token 與 Content-Type。
func doRequest(t *testing.T, method, urlPath, token string, body io.Reader, contentType string) (*http.Response, []byte) {
	t.Helper()
	url := baseURL + urlPath
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, bodyBytes
}

// doRequestNoBody 是 doRequest 的簡寫，適用於 GET / DELETE 等無 body 的請求。
func doRequestNoBody(t *testing.T, method, urlPath, token string) (*http.Response, []byte) {
	return doRequest(t, method, urlPath, token, nil, "")
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("status: got %d, want %d", resp.StatusCode, want)
	}
}

func assertJSONHas(t *testing.T, body []byte, jsonPath string, expected interface{}) {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Errorf("invalid JSON: %v", err)
		return
	}
	parts := strings.Split(jsonPath, ".")
	var current interface{} = m
	for _, p := range parts {
		if p == "" {
			continue
		}
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[p]
		default:
			t.Errorf("path %q: not a map at %q", jsonPath, p)
			return
		}
	}
	if current != expected {
		t.Errorf("json path %q: got %v, want %v", jsonPath, current, expected)
	}
}

func assertErrorCode(t *testing.T, body []byte, wantCode string) {
	t.Helper()
	assertJSONHas(t, body, "error.code", wantCode)
}

func assertErrorFormat(t *testing.T, body []byte) {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Errorf("invalid JSON: %v", err)
		return
	}
	errObj, ok := m["error"].(map[string]interface{})
	if !ok {
		t.Error("expected error object with code and message")
		return
	}
	if _, ok := errObj["code"]; !ok {
		t.Error("expected error.code")
	}
	if _, ok := errObj["message"]; !ok {
		t.Error("expected error.message")
	}
}

// createMultipartFile 建立 multipart/form-data 的 body，用於模擬檔案上傳。
func createMultipartFile(t *testing.T, filename string, content []byte, _ string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// uploadFile 上傳檔案到 /files/upload 並回傳 response 與 body。
func uploadFile(t *testing.T, token string, filename string, content []byte, contentType string) (*http.Response, []byte) {
	t.Helper()
	buf, ct := createMultipartFile(t, filename, content, contentType)
	return doRequest(t, http.MethodPost, "/api/v1/files/upload", token, buf, ct)
}
