//go:build integration

// consistency_test.go — 一致性與狀態機測試
//
// 測試範圍：
//   - 檔案 CRUD 狀態機：上傳 → 列表可見 → 刪除 → 列表不可見 → 再刪除回 404
//   - 容器 CRUD 狀態機：建立 → 停止 → 刪除 → 列表不可見 → 查詢回 404
//   - 非同步刪除 Job：必須最終 SUCCESS，避免產生半套刪除狀態
//   - 批次建立/刪除一致性：多檔案、多容器刪除後不應殘留孤兒資料
//
// 目標：驗證 API 在多步驟生命週期後，資料庫與實際資源狀態保持一致。

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// 驗證檔案完整生命週期的一致性：
// 建立（上傳）→ 讀取（列表）→ 刪除 → 驗證不存在（列表不可見、再刪除 404）。
// 確保刪除後不會在 PostgreSQL 殘留孤兒資料。
func TestFiles_CRUD_StateMachine_Consistency(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	content := []byte("state machine test")
	uploadResp, uploadBody := uploadFile(t, token, "crud_test.txt", content, "text/plain")
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: status=%d, want 201", uploadResp.StatusCode)
	}
	var uploadOut struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(uploadBody, &uploadOut); err != nil {
		t.Fatalf("upload response invalid: %v", err)
	}
	fileID := uploadOut.FileID

	// 讀取：列表必須包含剛上傳的檔案
	listResp, listBody := doRequestNoBody(t, http.MethodGet, "/api/v1/files", token)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list: status=%d", listResp.StatusCode)
	}
	var listOut struct {
		Data []struct {
			FileID string `json:"file_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listBody, &listOut); err != nil {
		t.Fatalf("list JSON invalid: %v", err)
	}
	foundInList := false
	for _, item := range listOut.Data {
		if item.FileID == fileID {
			foundInList = true
			break
		}
	}
	if !foundInList {
		t.Error("after upload: file should appear in list")
	}

	// 讀取：理論上 GET 也應可取得該檔案（目前由 list 行為間接覆蓋）

	// 刪除
	delResp, _ := doRequestNoBody(t, http.MethodDelete, "/api/v1/files/"+fileID, token)
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status=%d, want 204", delResp.StatusCode)
	}

	// 一致性：刪除後列表不應再包含該檔案
	listResp2, listBody2 := doRequestNoBody(t, http.MethodGet, "/api/v1/files", token)
	if listResp2.StatusCode != http.StatusOK {
		t.Fatalf("list after delete: status=%d", listResp2.StatusCode)
	}
	var listOut2 struct {
		Data []struct {
			FileID string `json:"file_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listBody2, &listOut2); err != nil {
		t.Fatalf("list JSON invalid: %v", err)
	}
	for _, item := range listOut2.Data {
		if item.FileID == fileID {
			t.Errorf("consistency violation: deleted file %s still in list (PG/file inconsistency)", fileID)
		}
	}

	// 一致性：再次刪除必須回 404（代表沒有殘留孤兒狀態）
	delAgainResp, delAgainBody := doRequestNoBody(t, http.MethodDelete, "/api/v1/files/"+fileID, token)
	if delAgainResp.StatusCode != http.StatusNotFound {
		t.Errorf("delete again: status=%d, want 404", delAgainResp.StatusCode)
	}
	if delAgainResp.StatusCode == http.StatusNotFound {
		assertErrorCode(t, delAgainBody, "FILE_NOT_FOUND")
	}
}

// 驗證容器完整生命週期的一致性：
// 建立 → 執行中 → 停止 → 已停止 → 刪除 → 驗證不存在。
// 確保刪除後不會在 PostgreSQL 殘留資料，也不會殘留 Docker 殭屍容器。
func TestContainers_CRUD_StateMachine_Consistency(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "crud-sm-"+time.Now().Format("150405"), "python-3.10", []string{"echo", "1"})

	// 讀取：列表必須包含剛建立的容器
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/containers", token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status=%d", resp.StatusCode)
	}
	var listOut struct {
		Data []struct {
			ContainerID string `json:"container_id"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &listOut); err != nil {
		t.Fatalf("list JSON invalid: %v", err)
	}
	foundInList := false
	for _, item := range listOut.Data {
		if item.ContainerID == containerID {
			foundInList = true
			if item.Status != "Running" && item.Status != "Exited" {
				t.Errorf("unexpected status %q, want Running or Exited", item.Status)
			}
			break
		}
	}
	if !foundInList {
		t.Error("after create: container should appear in list")
	}

	// 停止（若目前仍在執行）
	stopContainerAndWait(t, token, containerID)

	// 刪除並等待非同步 Job 完成（SUCCESS）
	deleteContainerAndWait(t, token, containerID)

	// 一致性：刪除後列表不應再包含該容器
	resp2, bodyBytes2 := doRequestNoBody(t, http.MethodGet, "/api/v1/containers", token)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("list after delete: status=%d", resp2.StatusCode)
	}
	var listOut2 struct {
		Data []struct {
			ContainerID string `json:"container_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes2, &listOut2); err != nil {
		t.Fatalf("list JSON invalid: %v", err)
	}
	for _, item := range listOut2.Data {
		if item.ContainerID == containerID {
			t.Errorf("consistency violation: deleted container %s still in list (PG/Docker inconsistency)", containerID)
		}
	}

	// 一致性：刪除後查詢單一容器必須回 404
	getResp, getBody := doRequestNoBody(t, http.MethodGet, "/api/v1/containers/"+containerID, token)
	if getResp.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: status=%d, want 404", getResp.StatusCode)
	}
	if getResp.StatusCode == http.StatusNotFound {
		assertErrorCode(t, getBody, "CONTAINER_NOT_FOUND")
	}
}

// 驗證容器刪除 Job 最終必須為 SUCCESS。
// 若 Job 失敗（例如 Docker 已刪除但 DB 更新失敗），可能導致資料不一致。
func TestContainers_DeleteJob_MustSucceed(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	_, containerID := createContainer(t, token, "del-job-"+time.Now().Format("150405"), "python-3.10", []string{"echo", "1"})
	stopContainerAndWait(t, token, containerID)

	resp, bodyBytes := doRequestNoBody(t, http.MethodDelete, "/api/v1/containers/"+containerID, token)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("delete: status=%d, want 202", resp.StatusCode)
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("delete response invalid: %v", err)
	}
	jobID := out.JobID

	// 輪詢直到 Job 完成；最終必須為 SUCCESS
	for range 100 {
		resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/jobs/"+jobID, token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get job: status=%d", resp.StatusCode)
		}
		var jobOut struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(bodyBytes, &jobOut); err != nil {
			t.Fatalf("job response invalid: %v", err)
		}
		if jobOut.Status == "SUCCESS" {
			return
		}
		if jobOut.Status == "FAILED" {
			t.Fatalf("delete job failed (PG/Docker inconsistency risk)")
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("delete job did not complete in time")
}

// 驗證多檔案刪除後不會留下孤兒紀錄。
// 針對 N 個檔案執行完整 create→delete→verify 週期。
func TestConsistency_MultipleFiles_DeleteAll(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	const n = 5
	fileIDs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		content := []byte("multi file " + string(rune('0'+i)))
		resp, body := uploadFile(t, token, "multi_"+string(rune('0'+i))+".txt", content, "text/plain")
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("upload %d: status=%d", i, resp.StatusCode)
		}
		var out struct {
			FileID string `json:"file_id"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("upload %d response invalid: %v", i, err)
		}
		fileIDs = append(fileIDs, out.FileID)
	}

	for _, fileID := range fileIDs {
		resp, _ := doRequestNoBody(t, http.MethodDelete, "/api/v1/files/"+fileID, token)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete %s: status=%d", fileID, resp.StatusCode)
		}
	}

	// 驗證列表中不應再出現任何已刪除檔案
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files", token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status=%d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			FileID string `json:"file_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("list JSON invalid: %v", err)
	}
	idSet := make(map[string]bool)
	for _, id := range fileIDs {
		idSet[id] = true
	}
	for _, item := range out.Data {
		if idSet[item.FileID] {
			t.Errorf("consistency violation: deleted file %s still in list", item.FileID)
		}
	}
}

// 驗證多容器刪除後不會留下孤兒紀錄。
// 流程為建立 N 個容器 → 全部停止 → 全部刪除 → 驗證列表。
func TestConsistency_MultipleContainers_DeleteAll(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	const n = 3
	containerIDs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		_, cid := createContainer(t, token, "multi-c-"+time.Now().Format("150405")+string(rune('a'+i)), "python-3.10", []string{"echo", "1"})
		containerIDs = append(containerIDs, cid)
	}

	for _, cid := range containerIDs {
		stopContainerAndWait(t, token, cid)
		deleteContainerAndWait(t, token, cid)
	}

	// 驗證列表中不應再出現任何已刪除容器
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
		t.Fatalf("list JSON invalid: %v", err)
	}
	idSet := make(map[string]bool)
	for _, id := range containerIDs {
		idSet[id] = true
	}
	for _, item := range out.Data {
		if idSet[item.ContainerID] {
			t.Errorf("consistency violation: deleted container %s still in list", item.ContainerID)
		}
	}
}
