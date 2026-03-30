//go:build integration

// jobs_test.go — 非同步任務查詢端點測試
//
// 測試範圍：
//   - GET /jobs/{id}：查詢自己建立的 Job → 200 + 完整 Job 詳情
//   - GET /jobs/{id}：查詢不存在的 Job → 404 JOB_NOT_FOUND
//
// 所有容器操作（create/start/stop/delete）皆為非同步，回傳 job_id 供查詢狀態。
// Job 回應包含：job_id, target_resource_id, type, status, error_message, created_at, completed_at。
//
// 對應 spec/api.md §Jobs。

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// 驗證查詢自己建立的 Job：先建立容器（觸發 CREATE_CONTAINER Job），
// 再查詢該 Job，回傳 200 + 完整欄位 (job_id, type, status, target_resource_id 等)。
func TestJobs_Get_Own_Returns200(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	jobID, _ := createContainer(t, token, "job-test-"+time.Now().Format("150405"), "python-3.10", []string{"echo", "1"})

	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/jobs/"+jobID, token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		JobID            string  `json:"job_id"`
		Type             string  `json:"type"`
		Status           string  `json:"status"`
		TargetResourceID string  `json:"target_resource_id"`
		ErrorMessage     *string `json:"error_message"`
		CreatedAt        string  `json:"created_at"`
		CompletedAt      *string `json:"completed_at"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.JobID == "" || out.Type == "" || out.Status == "" {
		t.Errorf("missing required fields: %+v", out)
	}
}

// 驗證查詢不存在的 Job：使用假 UUID，回傳 404 JOB_NOT_FOUND。
// 也涵蓋「其他使用者的 Job 對當前使用者不可見」的情境（FindByIDAndUserID 會過濾 user_id）。
func TestJobs_Get_NotFound_Returns404(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	fakeID := "550e8400-e29b-41d4-a716-446655440000"
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/jobs/"+fakeID, token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, bodyBytes, "JOB_NOT_FOUND")
}

// 驗證跨使用者 Job 隔離：user1 建立容器產生的 Job，admin 無法查詢 → 404。
// FindByIDAndUserID 查詢會過濾 user_id，因此其他使用者的 Job 等同不存在。
func TestJobs_Get_OtherUserJob_Returns404(t *testing.T) {
	token1 := mustLogin(t, "user1", "password123")
	jobID, _ := createContainer(t, token1, "job-isolation-"+time.Now().Format("150405.000"), "python-3.10", []string{"echo", "1"})

	token2 := mustLogin(t, "admin", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/jobs/"+jobID, token2)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, bodyBytes, "JOB_NOT_FOUND")
}

// 驗證 Job 完成後 completed_at 非空：建立容器等 Job 完成，確認 completed_at 有值。
func TestJobs_StatusTransition_CompletedAtSet(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	jobID, _ := createContainer(t, token, "job-completed-"+time.Now().Format("150405.000"), "python-3.10", []string{"echo", "1"})

	// Job should be SUCCESS by now (createContainer polls until SUCCESS)
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/jobs/"+jobID, token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Status      string  `json:"status"`
		CompletedAt *string `json:"completed_at"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Status != "SUCCESS" {
		t.Errorf("status: got %q, want SUCCESS", out.Status)
	}
	if out.CompletedAt == nil || *out.CompletedAt == "" {
		t.Error("completed_at: expected non-null for completed job")
	}
}
