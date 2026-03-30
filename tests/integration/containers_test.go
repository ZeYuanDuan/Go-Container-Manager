//go:build integration

// containers_test.go — 容器管理端點測試
//
// 測試範圍：
//   - POST /containers：建立容器 → 202、重複名稱 → 409、無效環境 → 400
//   - GET /containers：列出使用者容器 → 200
//   - GET /containers/{id}：查詢自己的 → 200、他人的 → 403、不存在 → 404
//   - POST /containers/{id}/start：啟動已停止容器 → 202、已在運行 → 409
//   - POST /containers/{id}/stop：停止運行中容器 → 202、已停止 → 409
//   - DELETE /containers/{id}：運行中刪除 → 409、停止後刪除 → 202
//   - 一致性驗證：刪除後確認 list 不再包含該容器
//
// 對應 spec/api.md §Containers。

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// createContainer 是測試輔助函式：建立容器並 polling 等待 Job 完成，回傳 jobID 與 containerID。
func createContainer(t *testing.T, token, name, env string, command []string) (jobID, containerID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":        name,
		"environment": env,
		"command":     command,
	})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/containers", token, bytes.NewReader(body), "application/json")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create container: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("create response invalid: %v", err)
	}
	jobID = out.JobID
	// Poll job until SUCCESS to get container_id
	containerID = pollJobForContainerID(t, token, jobID)
	return jobID, containerID
}

// pollJobForContainerID 輪詢 Job 狀態直到 SUCCESS，回傳 target_resource_id（即 container_id）。
func pollJobForContainerID(t *testing.T, token, jobID string) string {
	t.Helper()
	for range 50 {
		resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/jobs/"+jobID, token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get job: status=%d", resp.StatusCode)
		}
		var out struct {
			Status           string `json:"status"`
			TargetResourceID string `json:"target_resource_id"`
		}
		if err := json.Unmarshal(bodyBytes, &out); err != nil {
			t.Fatalf("job response invalid: %v", err)
		}
		if out.Status == "SUCCESS" && out.TargetResourceID != "" {
			return out.TargetResourceID
		}
		if out.Status == "FAILED" {
			t.Fatalf("job failed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job did not complete in time")
	return ""
}

// waitForJobComplete 輪詢直到 Job 狀態為 SUCCESS 或 FAILED。
func waitForJobComplete(t *testing.T, token, jobID string) {
	t.Helper()
	for range 50 {
		resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/jobs/"+jobID, token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get job: status=%d", resp.StatusCode)
		}
		var out struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(bodyBytes, &out); err != nil {
			t.Fatalf("job response invalid: %v", err)
		}
		if out.Status == "SUCCESS" || out.Status == "FAILED" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job did not complete in time")
}

// stopContainerAndWait 發送 Stop 請求並等待 Job 完成。
// 若容器已停止（短命令如 echo 1 已自行退出），409 CONFLICT 視為正常。
func stopContainerAndWait(t *testing.T, token, containerID string) {
	t.Helper()
	resp, bodyBytes := doRequestNoBody(t, http.MethodPost, "/api/v1/containers/"+containerID+"/stop", token)
	if resp.StatusCode == http.StatusConflict {
		return // already stopped — short-lived command exited
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("stop: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("stop response invalid: %v", err)
	}
	waitForJobComplete(t, token, out.JobID)
}

// 驗證成功建立容器：使用 python-3.10 環境、短命令，回傳 202 + 非空 job_id。
// 容器操作全部非同步，不會同步等待 Docker container 建立完成。
func TestContainers_Create_Success_Returns202(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	body, _ := json.Marshal(map[string]any{
		"name":        "test-create-" + time.Now().Format("150405"),
		"environment": "python-3.10",
		"command":     []string{"python", "-c", "print(1)"},
	})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/containers", token, bytes.NewReader(body), "application/json")
	assertStatus(t, resp, http.StatusAccepted)

	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.JobID == "" {
		t.Error("job_id: expected non-empty")
	}
}

// 驗證同一使用者不能建立同名容器：第二次建立相同名稱回傳 409 CONTAINER_NAME_DUPLICATE。
// 對應 containers 表的 UNIQUE(user_id, name) 約束。
func TestContainers_Create_DuplicateName_Returns409(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	name := "dup-name-" + time.Now().Format("150405")
	body, _ := json.Marshal(map[string]any{
		"name":        name,
		"environment": "python-3.10",
		"command":     []string{"echo", "1"},
	})
	resp1, _ := doRequest(t, http.MethodPost, "/api/v1/containers", token, bytes.NewReader(body), "application/json")
	if resp1.StatusCode != http.StatusAccepted {
		t.Skipf("first create failed: %d", resp1.StatusCode)
	}
	// Second create with same name
	resp2, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/containers", token, bytes.NewReader(body), "application/json")
	assertStatus(t, resp2, http.StatusConflict)
	assertErrorCode(t, bodyBytes, "CONTAINER_NAME_DUPLICATE")
}

// 驗證無效環境標籤：使用不在白名單中的 environment，回傳 400 INVALID_ENVIRONMENT。
// 對應環境白名單機制。
func TestContainers_Create_InvalidEnvironment_Returns400(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	body, _ := json.Marshal(map[string]any{
		"name":        "invalid-env-test",
		"environment": "nonexistent-env",
		"command":     []string{"echo", "1"},
	})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/containers", token, bytes.NewReader(body), "application/json")
	assertStatus(t, resp, http.StatusBadRequest)
	if len(bodyBytes) > 0 {
		assertErrorFormat(t, bodyBytes)
	}
}

// 驗證列出容器：回傳 200、JSON 包含 data 陣列（非 null）與 total 欄位。
func TestContainers_List_Returns200(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/containers", token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Data  []any `json:"data"`
		Total int           `json:"total"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Data == nil {
		t.Error("data: expected non-nil array")
	}
}

// 驗證查詢自己的容器詳情：回傳 200 + 完整欄位 (container_id, name, environment, status, mount_path, created_at)。
func TestContainers_Get_Own_Returns200(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "get-own-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "1"})

	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/containers/"+containerID, token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		ContainerID string `json:"container_id"`
		Name        string `json:"name"`
		Environment string `json:"environment"`
		Status      string `json:"status"`
		MountPath   string `json:"mount_path"`
		CreatedAt   string `json:"created_at"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.ContainerID == "" || out.Status == "" || out.MountPath == "" {
		t.Errorf("missing required fields: %+v", out)
	}
}

// 驗證跨使用者隔離：user1 建立的容器，admin 查詢回傳 403 FORBIDDEN。
func TestContainers_Get_OtherUser_Returns403(t *testing.T) {
	token1 := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token1, "get-other-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "1"})

	token2 := mustLogin(t, "admin", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/containers/"+containerID, token2)
	assertStatus(t, resp, http.StatusForbidden)
	assertErrorCode(t, bodyBytes, "FORBIDDEN")
}

// 驗證查詢不存在的容器：使用假 UUID，回傳 404 CONTAINER_NOT_FOUND。
func TestContainers_Get_NotFound_Returns404(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	fakeID := "550e8400-e29b-41d4-a716-446655440000"
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/containers/"+fakeID, token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, bodyBytes, "CONTAINER_NOT_FOUND")
}

// 驗證啟動已停止容器：先建立 → 等待完成 → 停止 → 再啟動，回傳 202 + job_id。
// 使用 sleep 10 確保容器維持 Running 狀態足夠久來執行 stop。
func TestContainers_Start_Success_Returns202(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "start-test-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "10"})
	stopContainerAndWait(t, token, containerID)

	resp, bodyBytes := doRequestNoBody(t, http.MethodPost, "/api/v1/containers/"+containerID+"/start", token)
	assertStatus(t, resp, http.StatusAccepted)

	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.JobID == "" {
		t.Error("job_id: expected non-empty")
	}
}

// 驗證並發控制：對運行中的容器再次啟動，回傳 409 CONFLICT。
// 使用 sleep 60 確保容器不會在測試期間自行退出。
// 防止對同一容器重複操作。
func TestContainers_Start_AlreadyRunning_Returns409(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "start-conflict-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "60"})
	// createContainer already waits for job SUCCESS, so container is Running

	resp, bodyBytes := doRequestNoBody(t, http.MethodPost, "/api/v1/containers/"+containerID+"/start", token)
	assertStatus(t, resp, http.StatusConflict)
	assertErrorCode(t, bodyBytes, "CONFLICT")
}

// 驗證停止運行中容器：建立後直接停止，回傳 202 + job_id。
func TestContainers_Stop_Success_Returns202(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "stop-test-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "60"})

	resp, bodyBytes := doRequestNoBody(t, http.MethodPost, "/api/v1/containers/"+containerID+"/stop", token)
	assertStatus(t, resp, http.StatusAccepted)

	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.JobID == "" {
		t.Error("job_id: expected non-empty")
	}
}

// 驗證並發控制：對已停止的容器再次停止，回傳 409 CONFLICT。
// 使用 sleep 60 確保 stop 測試前容器確實為 Running 狀態。
func TestContainers_Stop_AlreadyStopped_Returns409(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	// Use sleep so container stays RUNNING (echo 1 exits immediately, status would be EXITED)
	_, containerID := createContainer(t, token, "stop-conflict-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "60"})
	stopContainerAndWait(t, token, containerID)

	resp, bodyBytes := doRequestNoBody(t, http.MethodPost, "/api/v1/containers/"+containerID+"/stop", token)
	assertStatus(t, resp, http.StatusConflict)
	assertErrorCode(t, bodyBytes, "CONFLICT")
}

// 驗證刪除保護：運行中的容器不允許刪除，回傳 409 CONFLICT。
// 對應 spec — 必須先 Stop 才能 Delete。
func TestContainers_Delete_WhileRunning_Returns409(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "delete-running-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "60"})

	resp, bodyBytes := doRequestNoBody(t, http.MethodDelete, "/api/v1/containers/"+containerID, token)
	assertStatus(t, resp, http.StatusConflict)
	assertErrorCode(t, bodyBytes, "CONFLICT")
}

// 驗證停止後可以刪除：建立 → 停止 → 刪除，回傳 202 + job_id。
func TestContainers_Delete_WhenStopped_Returns202(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	// Use sleep so container is RUNNING before stop (echo 1 exits immediately)
	_, containerID := createContainer(t, token, "delete-stopped-"+time.Now().Format("150405"), "python-3.10", []string{"sleep", "5"})
	stopContainerAndWait(t, token, containerID)

	resp, bodyBytes := doRequestNoBody(t, http.MethodDelete, "/api/v1/containers/"+containerID, token)
	assertStatus(t, resp, http.StatusAccepted)

	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.JobID == "" {
		t.Error("job_id: expected non-empty")
	}
}

// deleteContainerAndWait 發送刪除請求並等待 Job 完成。
func deleteContainerAndWait(t *testing.T, token, containerID string) {
	t.Helper()
	resp, bodyBytes := doRequestNoBody(t, http.MethodDelete, "/api/v1/containers/"+containerID, token)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("delete: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("delete response invalid: %v", err)
	}
	waitForJobComplete(t, token, out.JobID)
}

// 一致性驗證：建立 → 停止 → 刪除 → 列表，確認已刪除的容器不會出現在 list 中。
// 驗證 Worker 非同步刪除後 DB 紀錄確實被移除（無 Docker/DB 孤兒資料）。
func TestContainers_Delete_ThenList_DoesNotIncludeDeleted(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "consistency-del-"+time.Now().Format("150405"), "python-3.10", []string{"echo", "1"})
	stopContainerAndWait(t, token, containerID)
	deleteContainerAndWait(t, token, containerID)

	// List containers: deleted one must NOT appear
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/containers", token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status=%d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ContainerID string `json:"container_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, item := range out.Data {
		if item.ContainerID == containerID {
			t.Errorf("deleted container %s still appears in list (DB/container inconsistency)", containerID)
		}
	}
}

// 驗證缺少必要欄位 environment：建立容器時只帶 name 和 command，回傳 400。
func TestContainers_Create_MissingEnvironment_Returns400(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	body, _ := json.Marshal(map[string]any{
		"name":    "no-env-container",
		"command": []string{"echo", "1"},
	})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/containers", token, bytes.NewReader(body), "application/json")
	assertStatus(t, resp, http.StatusBadRequest)
	if len(bodyBytes) > 0 {
		assertErrorFormat(t, bodyBytes)
	}
}

// 驗證缺少必要欄位 name：建立容器時只帶 environment 和 command，回傳 400。
func TestContainers_Create_MissingName_Returns400(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	body, _ := json.Marshal(map[string]any{
		"environment": "python-3.10",
		"command":     []string{"echo", "1"},
	})
	resp, bodyBytes := doRequest(t, http.MethodPost, "/api/v1/containers", token, bytes.NewReader(body), "application/json")
	assertStatus(t, resp, http.StatusBadRequest)
	if len(bodyBytes) > 0 {
		assertErrorFormat(t, bodyBytes)
	}
}

// 驗證對不存在的容器執行 Start，回傳 404 CONTAINER_NOT_FOUND。
func TestContainers_Start_NonExistent_Returns404(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	fakeID := "550e8400-e29b-41d4-a716-446655440000"
	resp, bodyBytes := doRequestNoBody(t, http.MethodPost, "/api/v1/containers/"+fakeID+"/start", token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, bodyBytes, "CONTAINER_NOT_FOUND")
}

// 驗證對不存在的容器執行 Stop，回傳 404 CONTAINER_NOT_FOUND。
func TestContainers_Stop_NonExistent_Returns404(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	fakeID := "550e8400-e29b-41d4-a716-446655440000"
	resp, bodyBytes := doRequestNoBody(t, http.MethodPost, "/api/v1/containers/"+fakeID+"/stop", token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, bodyBytes, "CONTAINER_NOT_FOUND")
}
