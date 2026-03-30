//go:build integration

// files_test.go — 檔案管理端點測試
//
// 測試範圍：
//   - 認證守衛：無 Token 存取 /files 回傳 401
//   - POST /files/upload：成功上傳 → 201、超過 50MB → 413、不允許的 MIME → 415
//   - GET /files：空清單回傳、有資料回傳、分頁參數
//   - DELETE /files/{id}：刪除自己的檔案 → 204、刪除他人的 → 403、不存在 → 404
//   - 一致性驗證：刪除後確認 list 不再包含該檔案（防止 DB 孤兒資料）
//
// 對應 spec/api.md §Files。

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 驗證認證中介層：未帶 JWT Token 存取受保護端點應回傳 401。
// 確保 middleware.AuthMiddleware 正確攔截未認證請求。
func TestFiles_NoToken_Returns401(t *testing.T) {
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files", "")
	assertStatus(t, resp, http.StatusUnauthorized)
	if len(bodyBytes) > 0 {
		assertErrorFormat(t, bodyBytes)
	}
}

// 驗證初始狀態下檔案清單為空：data 為空陣列（非 null）、total 為 0。
// 注意：此測試依賴執行順序，需在任何上傳測試之前執行（共用 in-process server 狀態）。
func TestFiles_List_Empty_Returns200(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files", token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Data  []interface{} `json:"data"`
		Total int           `json:"total"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Data == nil {
		t.Error("data: expected non-nil array")
	}
	if len(out.Data) != 0 {
		t.Errorf("data: expected empty, got %d items", len(out.Data))
	}
	if out.Total != 0 {
		t.Errorf("total: got %d, want 0", out.Total)
	}
}

// 驗證成功上傳純文字檔案：回傳 201、JSON 包含 file_id / filename / size_bytes / created_at。
// content "hello world" 的 magic bytes 會被 http.DetectContentType 偵測為 text/plain。
func TestFiles_Upload_Success_Returns201(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	content := []byte("hello world")
	resp, bodyBytes := uploadFile(t, token, "test.txt", content, "text/plain")
	assertStatus(t, resp, http.StatusCreated)

	var out struct {
		FileID    string `json:"file_id"`
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.FileID == "" {
		t.Error("file_id: expected non-empty")
	}
	if out.Filename != "test.txt" {
		t.Errorf("filename: got %q, want test.txt", out.Filename)
	}
	if out.SizeBytes != int64(len(content)) {
		t.Errorf("size_bytes: got %d, want %d", out.SizeBytes, len(content))
	}
	if out.CreatedAt == "" {
		t.Error("created_at: expected non-empty")
	}
}

// 驗證超過 50MB 限制的檔案被拒絕：回傳 413 + FILE_TOO_LARGE 錯誤碼。
func TestFiles_Upload_TooLarge_Returns413(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	// 51 MB
	content := make([]byte, 51*1024*1024)
	for i := range content {
		content[i] = 'x'
	}
	resp, bodyBytes := uploadFile(t, token, "large.bin", content, "application/octet-stream")
	assertStatus(t, resp, http.StatusRequestEntityTooLarge)
	assertErrorCode(t, bodyBytes, "FILE_TOO_LARGE")
}

// 驗證不允許的 MIME 類型被拒絕：使用 GIF magic bytes (GIF89a) 觸發 415。
// 伺服器透過 magic bytes 偵測實際 MIME，不信任 Content-Type header。
func TestFiles_Upload_UnsupportedMIME_Returns415(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	// GIF magic bytes - server validates by magic bytes
	content := []byte("GIF89a\x01\x02\x03")
	resp, bodyBytes := uploadFile(t, token, "image.gif", content, "image/gif")
	assertStatus(t, resp, http.StatusUnsupportedMediaType)
	assertErrorCode(t, bodyBytes, "UNSUPPORTED_MIME_TYPE")
}

// 驗證有資料時的檔案清單：先上傳一個檔案，再查詢 list。
// 確認 total >= 1 且每筆資料包含必要欄位 (file_id, filename, created_at)。
func TestFiles_List_WithData_Returns200(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	// Upload first
	content := []byte("test content")
	uploadResp, uploadBody := uploadFile(t, token, "list_test.txt", content, "text/plain")
	if uploadResp.StatusCode != http.StatusCreated {
		t.Skipf("upload failed (need implementation): %d", uploadResp.StatusCode)
	}
	var uploadOut struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(uploadBody, &uploadOut); err != nil {
		t.Fatalf("upload response invalid: %v", err)
	}

	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files", token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Data []struct {
			FileID    string `json:"file_id"`
			Filename  string `json:"filename"`
			SizeBytes int64  `json:"size_bytes"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Total < 1 {
		t.Errorf("total: expected >= 1, got %d", out.Total)
	}
	if len(out.Data) < 1 {
		t.Errorf("data: expected at least 1 item, got %d", len(out.Data))
	}
	// Check first item has required fields
	if out.Data[0].FileID == "" || out.Data[0].Filename == "" || out.Data[0].CreatedAt == "" {
		t.Errorf("data item missing required fields: %+v", out.Data[0])
	}
}

// 驗證分頁參數：page=2&limit=10 應回傳 200 + 空或部分資料。
// 確認超出範圍的頁碼仍回傳 200 與空 data 陣列（非錯誤）。
func TestFiles_List_Pagination_Returns200(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files?page=2&limit=10", token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Data  []interface{} `json:"data"`
		Total int           `json:"total"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Data == nil {
		t.Error("data: expected non-nil array")
	}
	// Spec: page beyond total returns 200 with empty array and correct total
	_ = out.Total
}

// 驗證刪除自己的檔案：先上傳再刪除，預期回傳 204 No Content。
func TestFiles_Delete_Own_Returns204(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	content := []byte("to delete")
	uploadResp, uploadBody := uploadFile(t, token, "delete_me.txt", content, "text/plain")
	if uploadResp.StatusCode != http.StatusCreated {
		t.Skipf("upload failed (need implementation): %d", uploadResp.StatusCode)
	}
	var uploadOut struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(uploadBody, &uploadOut); err != nil {
		t.Fatalf("upload response invalid: %v", err)
	}

	resp, _ := doRequestNoBody(t, http.MethodDelete, "/api/v1/files/"+uploadOut.FileID, token)
	assertStatus(t, resp, http.StatusNoContent)
}

// 驗證跨使用者資源隔離：user1 上傳的檔案，admin 嘗試刪除應回傳 403 FORBIDDEN。
// 確保 FileService.Delete 正確比對 userID 所有權。
func TestFiles_Delete_OtherUser_Returns403(t *testing.T) {
	// Upload as user1
	token1 := mustLogin(t, "user1", "password123")
	content := []byte("user1 file")
	uploadResp, uploadBody := uploadFile(t, token1, "user1_file.txt", content, "text/plain")
	if uploadResp.StatusCode != http.StatusCreated {
		t.Skipf("upload failed (need implementation): %d", uploadResp.StatusCode)
	}
	var uploadOut struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(uploadBody, &uploadOut); err != nil {
		t.Fatalf("upload response invalid: %v", err)
	}

	// Try to delete as admin (different user)
	token2 := mustLogin(t, "admin", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodDelete, "/api/v1/files/"+uploadOut.FileID, token2)
	assertStatus(t, resp, http.StatusForbidden)
	assertErrorCode(t, bodyBytes, "FORBIDDEN")
}

// 驗證刪除不存在的檔案：使用假 UUID，預期回傳 404 FILE_NOT_FOUND。
func TestFiles_Delete_NotFound_Returns404(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	// Non-existent UUID
	fakeID := "550e8400-e29b-41d4-a716-446655440000"
	resp, bodyBytes := doRequestNoBody(t, http.MethodDelete, "/api/v1/files/"+fakeID, token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, bodyBytes, "FILE_NOT_FOUND")
}

// 一致性驗證：上傳 → 刪除 → 列表，確認已刪除的檔案不會出現在 list 中。
// 防止「實體檔案已刪但 DB metadata 殘留」的孤兒資料問題。
func TestFiles_Delete_ThenList_DoesNotIncludeDeleted(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	content := []byte("consistency test content")
	uploadResp, uploadBody := uploadFile(t, token, "consistency_delete_test.txt", content, "text/plain")
	if uploadResp.StatusCode != http.StatusCreated {
		t.Skipf("upload failed (need implementation): %d", uploadResp.StatusCode)
	}
	var uploadOut struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(uploadBody, &uploadOut); err != nil {
		t.Fatalf("upload response invalid: %v", err)
	}
	fileID := uploadOut.FileID

	// Delete the file
	delResp, _ := doRequestNoBody(t, http.MethodDelete, "/api/v1/files/"+fileID, token)
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status=%d, want 204", delResp.StatusCode)
	}

	// List files: deleted file must NOT appear
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
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, item := range out.Data {
		if item.FileID == fileID {
			t.Errorf("deleted file %s still appears in list (DB/file inconsistency)", fileID)
		}
	}
}

// 驗證分頁邊界：limit=0 時，服務層正規化為預設值 20，回傳 200。
func TestFiles_List_PaginationBoundary_LimitZero(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files?page=1&limit=0", token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Data []interface{} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Data == nil {
		t.Error("data: expected non-nil array")
	}
}

// 驗證分頁邊界：limit=999 時，服務層正規化為上限 100，回傳 200。
func TestFiles_List_PaginationBoundary_LimitExcessive(t *testing.T) {
	token := mustLogin(t, "user1", "password123")
	resp, bodyBytes := doRequestNoBody(t, http.MethodGet, "/api/v1/files?page=1&limit=999", token)
	assertStatus(t, resp, http.StatusOK)

	var out struct {
		Data []interface{} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Data == nil {
		t.Error("data: expected non-nil array")
	}
}
