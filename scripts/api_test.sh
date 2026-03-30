#!/usr/bin/env bash
# ============================================================================
# AIDMS Container Manager — API 端對端測試腳本
# ============================================================================
#
# 用途：對已啟動的 server 執行完整 API 契約測試（成功 + 錯誤案例；api.http 僅含成功案例供手動檢視）。
# 需求：curl, jq, docker（清理用）
# 執行：./scripts/api_test.sh 或 make test-api
# BASE_URL 預設 http://127.0.0.1:8080（Docker Compose 埠映射）或本機 server 皆適用
# 若 server 在他處，可覆寫：BASE_URL=http://host:port ./scripts/api_test.sh
# 建議：先 `make clean-test-env` 清理環境再啟動 server，最後 `make test-api`。
#
# ── 測試流程概覽 ──
#
# 0. 前置清理
#    清除所有 aidms- 前綴的 Docker workload 容器（排除 aidms-postgres），
#    確保測試從乾淨狀態開始。
#
# 1. 健康檢查與環境白名單（公開端點，無需認證）
#    - GET /health → 200 + {"status":"UP"}
#    - GET /environments → 200 + environments 陣列
#
# 2. 認證
#    - POST /login 正確帳密 → 200 + JWT token（後續測試皆使用此 token）
#    - POST /login 錯誤密碼 → 401
#    - POST /login 缺少 body → 400
#
# 3. 檔案管理
#    - GET /files 無 Token → 401（認證守衛）
#    - GET /files 有 Token → 200
#    - GET /files 分頁參數 → 200
#    - POST /files/upload 上傳 .txt（text/plain）→ 201
#    - POST /files/upload 上傳 .json（application/json）→ 201
#    - POST /files/upload 上傳 .csv（text/csv）→ 201
#    - POST /files/upload 上傳 .jpg（image/jpeg）→ 201
#    - POST /files/upload 上傳 .png（image/png）→ 201
#    - POST /files/upload 上傳 .gif（不允許的 MIME）→ 415
#    - POST /files/upload 上傳 51MB → 413
#    - GET /files 有資料 → 200
#    - DELETE /files/{id} 刪除自己的 → 204
#    - 一致性驗證：刪除後 list 不含已刪檔案
#    - DELETE /files/{id} 不存在 → 404
#    - 跨使用者隔離（user1 ↔ user2）：
#      · user2 的 list 不含 user1 的檔案
#      · user2 不能刪除 user1 的檔案 → 403
#      · user1 的 list 不含 user2 的檔案
#      · user1 不能刪除 user2 的檔案 → 403
#
# 4. 容器管理
#    - GET /containers → 200
#    - GET /containers 分頁 → 200
#    - 4a. 多環境測試：同一使用者分別建立 python-3.10 和 ubuntu-base 容器，確認都能成功
#    - POST /containers 建立（sleep 120 長命令）→ 202，polling Job 取得 container_id
#    - POST /containers 重複名稱 → 409 CONTAINER_NAME_DUPLICATE
#    - POST /containers 無效環境 → 400
#    - GET /containers/{id} 自己的 → 200
#    - GET /containers/{id} 不存在 → 404
#    - GET /containers/{id} 其他使用者的 → 403
#    - 跨使用者隔離（user1 ↔ user2）：
#      · user2 的 list 不含 user1 的容器
#      · user2 不能 Stop user1 的容器 → 403
#      · user2 建立容器，user1 不能 GET → 403
#      · user1 的 list 不含 user2 的容器
#      · user1 不能 Stop user2 的容器 → 403
#    - Jobs 跨使用者隔離：user2 不能查詢 user1 的 Job → 404
#    - 容器生命週期：Stop → Start → Start 衝突(409) → Stop → Stop 衝突(409)
#    - Delete 保護：運行中刪除 → 409，停止後刪除 → 202
#    - 一致性驗證：刪除後 list 不含已刪容器
#    - DELETE /containers/{id} 不存在 → 404
#
# 5. Jobs
#    - GET /jobs/{id} 自己的 → 200
#    - GET /jobs/{id} 不存在 → 404
#
# 6. 後置清理
#    刪除測試期間建立的所有檔案與容器（Stop + Delete），
#    清除殘留的 aidms- Docker 容器，恢復到測試前狀態。
#
# ============================================================================

command -v jq >/dev/null || { echo "jq is required. Install: brew install jq"; exit 1; }
command -v docker >/dev/null || { echo "docker is required for cleanup"; }

# Do not use set -e: we want to run all tests and report summary
# Default: 127.0.0.1 works for both Docker (port-mapped) and local server
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
BASE_URL="${BASE_URL%/}"
API="${BASE_URL}/api/v1"

FAILED=0
PASSED=0

# Track created resources for cleanup (containers from user2 use CREATED_CONTAINER_IDS_USER2)
CREATED_CONTAINER_IDS=()
CREATED_CONTAINER_IDS_USER2=()

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[1;33m'
nc='\033[0m'

ok() { echo -e "${green}  OK${nc} $1"; PASSED=$((PASSED+1)); }
fail() { echo -e "${red}FAIL${nc} $1"; FAILED=$((FAILED+1)); }
skip() { echo -e "${yellow}SKIP${nc} $1"; }

# Poll job until SUCCESS or FAILED
poll_job() {
  local token=$1 job_id=$2 max=${3:-100}
  local i s
  for ((i=0; i<max; i++)); do
    out=$(curl -s -H "Authorization: Bearer $token" "$API/jobs/$job_id")
    s=$(echo "$out" | jq -r '.status')
    [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && echo "$s" && return 0
    sleep 0.1
  done
  echo "TIMEOUT"
  return 1
}

# Stop and delete container, wait for delete job
stop_and_delete_container() {
  local token=$1 cid=$2
  out=$(curl -s -w "\n%{http_code}" -X POST -H "Authorization: Bearer $token" "$API/containers/$cid/stop")
  if [[ $(echo "$out" | tail -1) -eq 202 ]]; then
    stop_job=$(echo "$out" | sed '$d' | jq -r '.job_id')
    poll_job "$token" "$stop_job" 50 >/dev/null
  fi
  out=$(curl -s -w "\n%{http_code}" -X DELETE -H "Authorization: Bearer $token" "$API/containers/$cid")
  if [[ $(echo "$out" | tail -1) -eq 202 ]]; then
    del_job=$(echo "$out" | sed '$d' | jq -r '.job_id')
    poll_job "$token" "$del_job" 50 >/dev/null
  fi
}

check() {
  local status=$1 want=$2 desc=$3 body=$4 errcode=$5
  if [[ $status -eq $want ]]; then
    if [[ -n $errcode && -n $body ]]; then
      code=$(echo "$body" | jq -r '.error.code // empty')
      if [[ "$code" == "$errcode" ]]; then ok "$desc"; else fail "$desc (wrong error code: $code)"; fi
    else
      ok "$desc"
    fi
  else
    fail "$desc (status $status, want $want)"
  fi
}

req() {
  curl -s -w "\n%{http_code}" -X "$1" ${4:+-H "Content-Type: $4"} ${3:+-H "Authorization: Bearer $3"} "$2" ${5:+-d "$5"}
}

req_no_content() {
  curl -s -w "\n%{http_code}" -X "$1" ${3:+-H "Authorization: Bearer $3"} "$2"
}

req_file() {
  local method=$1 url=$2 token=$3 path=$4
  curl -s -w "\n%{http_code}" -X "$method" -H "Authorization: Bearer $token" -F "file=@$path" "$url"
}

body_and_status() {
  local out=$1
  echo "$out" | sed '$d'
  echo "$out" | tail -1
}

echo "=== AIDMS API Test Script ==="
echo "Base URL: $BASE_URL"
echo ""

# Wait for server to be ready (helps when server just started via docker compose)
echo "Waiting for server..."
for i in {1..15}; do
  if curl -s -o /dev/null -w "%{http_code}" "$API/health" | grep -q 200; then
    echo "Server ready."
    break
  fi
  [[ $i -eq 15 ]] && { echo "Server not reachable at $API/health after 15 tries. Is it running?"; exit 1; }
  sleep 1
done
echo ""

# --- 0. 前置清理：移除 aidms- 前綴的 workload 容器（排除 aidms-postgres、aidms-server） ---
# aidms-postgres：DB 不動；aidms-server：API 服務；其餘為測試產生的 workload 容器
echo "--- 0. Pre-test cleanup (aidms workload containers) ---"
count=0
for name in $(docker ps -a --format '{{.Names}}' 2>/dev/null | grep '^aidms-' || true); do
  [[ "$name" == "aidms-postgres" || "$name" == "aidms-server" ]] && continue
  docker rm -f "$name" 2>/dev/null && count=$((count+1))
done
[[ $count -gt 0 ]] && ok "Cleaned $count aidms workload container(s)" || ok "No aidms workload containers to clean"
echo ""

# --- 1. 健康檢查與環境白名單 ---
# 公開端點（無需 JWT），驗證服務存活與環境設定正確。
echo "--- 1. Health & Environments ---"
out=$(curl -s -w "\n%{http_code}" "$API/health")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /health"
[[ $(echo "$body" | jq -r '.status') == "UP" ]] || fail "GET /health status field"

out=$(curl -s -w "\n%{http_code}" "$API/environments")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /environments"

# --- 2. 認證測試 ---
# 驗證登入流程：正確帳密取得 Token、錯誤密碼 401、缺少 body 400。
# 取得的 TOKEN 供後續所有受保護端點使用。
echo ""
echo "--- 2. Authentication ---"
out=$(req POST "$API/login" "" "application/json" '{"username":"user1","password":"password123"}')
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "POST /login (valid)"
TOKEN=$(echo "$body" | jq -r '.token')
[[ -n "$TOKEN" && "$TOKEN" != "null" ]] || fail "POST /login missing token"

out=$(req POST "$API/login" "" "application/json" '{"username":"user1","password":"wrongpassword"}')
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 401 "POST /login (invalid password)"

out=$(curl -s -w "\n%{http_code}" -X POST "$API/login")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 400 "POST /login (no body)" "$body" ""

# --- 3. 檔案管理測試 ---
# 涵蓋：認證守衛、上傳（成功/超大/不支援 MIME）、列表（空/有資料/分頁）、
# 刪除（自己/他人/不存在）、一致性驗證、跨使用者隔離。
echo ""
echo "--- 3. Files ---"
out=$(req_no_content GET "$API/files")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 401 "GET /files (no token)"

out=$(req_no_content GET "$API/files" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /files (with token)"

out=$(req_no_content GET "$API/files?page=2&limit=10" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /files (pagination)"

# 上傳成功：純文字檔（text/plain），預期 201 + file_id
TMP_DIR=$(mktemp -d)
TESTDATA_DIR="$(cd "$(dirname "$0")/../tests/testdata" 2>/dev/null && pwd)"
echo "hello world" > "$TMP_DIR/test.txt"
out=$(req_file POST "$API/files/upload" "$TOKEN" "$TMP_DIR/test.txt")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 201 "POST /files/upload (text/plain)"
FILE_ID=$(echo "$body" | jq -r '.file_id')
[[ -n "$FILE_ID" && "$FILE_ID" != "null" ]] || fail "POST /files/upload missing file_id"

# 上傳成功：JSON 檔（application/json），預期 201
echo '{"model":"resnet50","epochs":100}' > "$TMP_DIR/config.json"
out=$(req_file POST "$API/files/upload" "$TOKEN" "$TMP_DIR/config.json")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 201 "POST /files/upload (application/json)"
JSON_FILE_ID=$(echo "$body" | jq -r '.file_id')

# 上傳成功：CSV 檔（偵測為 text/plain），預期 201
printf 'id,name,score\n1,alice,95\n2,bob,87\n' > "$TMP_DIR/data.csv"
out=$(req_file POST "$API/files/upload" "$TOKEN" "$TMP_DIR/data.csv")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 201 "POST /files/upload (CSV as text/plain)"
CSV_FILE_ID=$(echo "$body" | jq -r '.file_id')

# 上傳成功：JPEG 圖片（image/jpeg magic bytes），預期 201
if [[ -f "$TESTDATA_DIR/test.jpg" ]]; then
  out=$(req_file POST "$API/files/upload" "$TOKEN" "$TESTDATA_DIR/test.jpg")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  check "$status" 201 "POST /files/upload (image/jpeg)"
  JPEG_FILE_ID=$(echo "$body" | jq -r '.file_id')
else
  skip "POST /files/upload (image/jpeg) — tests/testdata/test.jpg not found"
fi

# 上傳成功：PNG 圖片（image/png magic bytes），預期 201
if [[ -f "$TESTDATA_DIR/test.png" ]]; then
  out=$(req_file POST "$API/files/upload" "$TOKEN" "$TESTDATA_DIR/test.png")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  check "$status" 201 "POST /files/upload (image/png)"
  PNG_FILE_ID=$(echo "$body" | jq -r '.file_id')
else
  skip "POST /files/upload (image/png) — tests/testdata/test.png not found"
fi

# 清理：刪除本輪 MIME 測試上傳的檔案（JSON, CSV, JPEG, PNG）
for fid in "$JSON_FILE_ID" "$CSV_FILE_ID" "$JPEG_FILE_ID" "$PNG_FILE_ID"; do
  [[ -n "$fid" && "$fid" != "null" ]] && req_no_content DELETE "$API/files/$fid" "$TOKEN" >/dev/null
done

# 上傳不允許的 MIME（GIF magic bytes）→ 415
printf 'GIF89a\x01\x02\x03' > "$TMP_DIR/image.gif"
out=$(req_file POST "$API/files/upload" "$TOKEN" "$TMP_DIR/image.gif")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 415 "POST /files/upload (GIF)" "$body" "UNSUPPORTED_MIME_TYPE"

# 上傳超過 50MB 限制（51MB）→ 413
dd if=/dev/zero of="$TMP_DIR/large.bin" bs=1M count=51 2>/dev/null
out=$(req_file POST "$API/files/upload" "$TOKEN" "$TMP_DIR/large.bin")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 413 "POST /files/upload (>50MB)" "$body" "FILE_TOO_LARGE"

# 列表有資料（上傳後）
out=$(req_no_content GET "$API/files" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /files (with data)"

# 刪除自己的檔案 → 204
out=$(req_no_content DELETE "$API/files/$FILE_ID" "$TOKEN")
status=$(echo "$out" | tail -1)
check "$status" 204 "DELETE /files/{id} (own)"

# 一致性驗證：刪除後 list 不含已刪檔案（防 DB 孤兒資料）
out=$(req_no_content GET "$API/files" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /files (after delete)"
found=$(echo "$body" | jq -r --arg fid "$FILE_ID" '.data[] | select(.file_id == $fid) | .file_id // empty')
[[ -z "$found" ]] && ok "Files consistency: deleted file not in list" || fail "Files consistency: deleted file still in list"

# 刪除不存在的檔案 → 404
out=$(req_no_content DELETE "$API/files/550e8400-e29b-41d4-a716-446655440000" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 404 "DELETE /files/{id} (not found)" "$body" "FILE_NOT_FOUND"

# 跨使用者檔案隔離：user1 和 user2 各自上傳，互相不可存取對方的檔案
echo "hello from user1" > "$TMP_DIR/user1file.txt"
out=$(req_file POST "$API/files/upload" "$TOKEN" "$TMP_DIR/user1file.txt")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
if [[ $status -ne 201 ]]; then
  skip "Files isolation - user1 upload failed"
else
  FILE_ID_USER1=$(echo "$body" | jq -r '.file_id')
  out=$(req POST "$API/login" "" "application/json" '{"username":"user2","password":"password123"}')
  body2=$(echo "$out" | sed '$d'); status2=$(echo "$out" | tail -1)
  if [[ $status2 -ne 200 ]]; then
    skip "Files isolation - user2 login failed"
  else
    TOKEN_USER2=$(echo "$body2" | jq -r '.token')
    # user2 list must NOT contain user1's file
    out=$(req_no_content GET "$API/files" "$TOKEN_USER2")
    body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
    check "$status" 200 "GET /files (user2 list)"
    found=$(echo "$body" | jq -r --arg fid "$FILE_ID_USER1" '.data[] | select(.file_id == $fid) | .file_id')
    [[ -z "$found" ]] || fail "GET /files (user2 must not see user1's file)"
    # user2 cannot delete user1's file
    out=$(req_no_content DELETE "$API/files/$FILE_ID_USER1" "$TOKEN_USER2")
    body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
    check "$status" 403 "DELETE /files/{id} (user2 cannot delete user1's)" "$body" "FORBIDDEN"

    # user2 uploads; user1 must NOT see user2's file, cannot delete it
    echo "hello from user2" > "$TMP_DIR/user2file.txt"
    out=$(req_file POST "$API/files/upload" "$TOKEN_USER2" "$TMP_DIR/user2file.txt")
    body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
    if [[ $status -eq 201 ]]; then
      FILE_ID_USER2=$(echo "$body" | jq -r '.file_id')
      # user1 list must NOT contain user2's file
      out=$(req_no_content GET "$API/files" "$TOKEN")
      body=$(echo "$out" | sed '$d')
      found=$(echo "$body" | jq -r --arg fid "$FILE_ID_USER2" '.data[] | select(.file_id == $fid) | .file_id')
      [[ -z "$found" ]] && ok "GET /files (user1 must not see user2's file)" || fail "GET /files (user1 must not see user2's file)"
      # user1 cannot delete user2's file
      out=$(req_no_content DELETE "$API/files/$FILE_ID_USER2" "$TOKEN")
      body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
      check "$status" 403 "DELETE /files/{id} (user1 cannot delete user2's)" "$body" "FORBIDDEN"
    else
      skip "Files isolation - user2 upload failed"
    fi
  fi
fi
rm -rf "$TMP_DIR"

# --- 4. 容器管理測試 ---
# 涵蓋：CRUD、生命週期（Create → Start → Stop → Delete）、並發控制（409 Conflict）、
# 多環境支援、跨使用者隔離、一致性驗證。
echo ""
echo "--- 4. Containers ---"
out=$(req_no_content GET "$API/containers" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /containers"

out=$(req_no_content GET "$API/containers?page=1&limit=20" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /containers (pagination)"

# 4a. 多環境測試：同一使用者分別建立 python-3.10 和 ubuntu-base 容器，確認都能成功
echo ""
echo "--- 4a. Multi-environment containers (single user) ---"
for env in "python-3.10" "ubuntu-base"; do
  name="api-test-${env}-$(date +%s)"
  out=$(req POST "$API/containers" "$TOKEN" "application/json" "{\"name\":\"$name\",\"environment\":\"$env\",\"command\":[\"sleep\",\"5\"]}")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  if [[ $status -ne 202 ]]; then
    fail "POST /containers ($env) status=$status"
  else
    job_id=$(echo "$body" | jq -r '.job_id')
    result=$(poll_job "$TOKEN" "$job_id")
    if [[ "$result" == "SUCCESS" ]]; then
      cid=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/jobs/$job_id" | jq -r '.target_resource_id // empty')
      [[ -n "$cid" && "$cid" != "null" ]] && CREATED_CONTAINER_IDS+=("$cid")
      ok "Container $env created and ran successfully"
    else
      fail "Container $env create job failed (result=$result)"
    fi
  fi
done
echo ""

# 建立長命令容器 (sleep 120) 用於後續 Stop/Start 生命週期測試
NAME="api-test-$(date +%s)"
out=$(req POST "$API/containers" "$TOKEN" "application/json" "{\"name\":\"$NAME\",\"environment\":\"python-3.10\",\"command\":[\"sleep\",\"120\"]}")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 202 "POST /containers (create)"
JOB_ID=$(echo "$body" | jq -r '.job_id')
[[ -n "$JOB_ID" && "$JOB_ID" != "null" ]] || fail "POST /containers missing job_id"

# 輪詢 Job 取得 container_id
CONTAINER_ID=""
for i in {1..100}; do
  out=$(req_no_content GET "$API/jobs/$JOB_ID" "$TOKEN")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  [[ $status -eq 200 ]] || { fail "GET /jobs poll"; break; }
  s=$(echo "$body" | jq -r '.status')
  CONTAINER_ID=$(echo "$body" | jq -r '.target_resource_id // empty')
  if [[ "$s" == "SUCCESS" && -n "$CONTAINER_ID" && "$CONTAINER_ID" != "null" ]]; then break; fi
  if [[ "$s" == "FAILED" ]]; then fail "Container create job failed"; break; fi
  sleep 0.1
done
[[ -n "$CONTAINER_ID" && "$CONTAINER_ID" != "null" ]] || fail "Could not get container_id from job"
CREATED_CONTAINER_IDS+=("$CONTAINER_ID")

# 重複名稱 → 409
out=$(req POST "$API/containers" "$TOKEN" "application/json" "{\"name\":\"$NAME\",\"environment\":\"python-3.10\",\"command\":[\"echo\",\"1\"]}")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 409 "POST /containers (duplicate name)" "$body" "CONTAINER_NAME_DUPLICATE"

# 無效環境 → 400
out=$(req POST "$API/containers" "$TOKEN" "application/json" '{"name":"invalid-env","environment":"nonexistent","command":["echo","1"]}')
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 400 "POST /containers (invalid env)"

# 查詢自己的容器 → 200
out=$(req_no_content GET "$API/containers/$CONTAINER_ID" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /containers/{id} (own)"

# 查詢不存在的容器 → 404
out=$(req_no_content GET "$API/containers/550e8400-e29b-41d4-a716-446655440000" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 404 "GET /containers/{id} (not found)" "$body" "CONTAINER_NOT_FOUND"

# 查詢其他使用者的容器 → 403
out=$(req POST "$API/login" "" "application/json" '{"username":"admin","password":"password123"}')
body2=$(echo "$out" | sed '$d'); status2=$(echo "$out" | tail -1)
if [[ $status2 -eq 200 ]]; then
  TOKEN_ADMIN=$(echo "$body2" | jq -r '.token')
  out=$(req_no_content GET "$API/containers/$CONTAINER_ID" "$TOKEN_ADMIN")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  check "$status" 403 "GET /containers/{id} (other user)" "$body" "FORBIDDEN"
else
  skip "GET /containers (other user) - admin login failed"
fi

# 容器列表隔離：user2 的清單不可包含 user1 的容器
if [[ -n "$TOKEN_USER2" && "$TOKEN_USER2" != "null" ]]; then
  out=$(req_no_content GET "$API/containers" "$TOKEN_USER2")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  check "$status" 200 "GET /containers (user2 list)"
  found=$(echo "$body" | jq -r --arg cid "$CONTAINER_ID" '.data[] | select(.container_id == $cid) | .container_id')
  [[ -z "$found" ]] || fail "GET /containers (user2 must not see user1's container)"
else
  out=$(req POST "$API/login" "" "application/json" '{"username":"user2","password":"password123"}')
  body2=$(echo "$out" | sed '$d'); status2=$(echo "$out" | tail -1)
  if [[ $status2 -eq 200 ]]; then
    TOKEN_USER2=$(echo "$body2" | jq -r '.token')
    out=$(req_no_content GET "$API/containers" "$TOKEN_USER2")
    body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
    check "$status" 200 "GET /containers (user2 list)"
    found=$(echo "$body" | jq -r --arg cid "$CONTAINER_ID" '.data[] | select(.container_id == $cid) | .container_id')
    [[ -z "$found" ]] || fail "GET /containers (user2 must not see user1's container)"
  else
    skip "GET /containers list isolation - user2 login failed"
  fi
fi

# 容器操作隔離：user2 不能 Stop user1 的容器 → 403
if [[ -n "$TOKEN_USER2" && "$TOKEN_USER2" != "null" ]]; then
  out=$(req_no_content POST "$API/containers/$CONTAINER_ID/stop" "$TOKEN_USER2")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  check "$status" 403 "POST /containers/{id}/stop (other user)" "$body" "FORBIDDEN"
fi

# 跨使用者容器隔離：user2 建立容器，user1 不能存取（GET/Stop 皆 403，list 不可見）
if [[ -n "$TOKEN_USER2" && "$TOKEN_USER2" != "null" ]]; then
  NAME_USER2="api-test-user2-$(date +%s)"
  out=$(req POST "$API/containers" "$TOKEN_USER2" "application/json" "{\"name\":\"$NAME_USER2\",\"environment\":\"python-3.10\",\"command\":[\"echo\",\"1\"]}")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  if [[ $status -eq 202 ]]; then
    JOB_USER2=$(echo "$body" | jq -r '.job_id')
    CONTAINER_ID_USER2=""
    for i in {1..100}; do
      out=$(req_no_content GET "$API/jobs/$JOB_USER2" "$TOKEN_USER2")
      body=$(echo "$out" | sed '$d'); s=$(echo "$body" | jq -r '.status')
      CONTAINER_ID_USER2=$(echo "$body" | jq -r '.target_resource_id // empty')
      [[ "$s" == "SUCCESS" && -n "$CONTAINER_ID_USER2" && "$CONTAINER_ID_USER2" != "null" ]] && break
      [[ "$s" == "FAILED" ]] && { skip "Container isolation - user2 create failed"; break; }
      sleep 0.1
    done
    if [[ -n "$CONTAINER_ID_USER2" && "$CONTAINER_ID_USER2" != "null" ]]; then
      CREATED_CONTAINER_IDS_USER2+=("$CONTAINER_ID_USER2")
      # user1 cannot GET user2's container
      out=$(req_no_content GET "$API/containers/$CONTAINER_ID_USER2" "$TOKEN")
      body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
      check "$status" 403 "GET /containers/{id} (user1 cannot get user2's)" "$body" "FORBIDDEN"
      # user1 list must NOT contain user2's container
      out=$(req_no_content GET "$API/containers" "$TOKEN")
      body=$(echo "$out" | sed '$d')
      found=$(echo "$body" | jq -r --arg cid "$CONTAINER_ID_USER2" '.data[] | select(.container_id == $cid) | .container_id')
      [[ -z "$found" ]] && ok "GET /containers (user1 must not see user2's container)" || fail "GET /containers (user1 must not see user2's container)"
      # user1 cannot Stop user2's container
      out=$(req_no_content POST "$API/containers/$CONTAINER_ID_USER2/stop" "$TOKEN")
      body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
      check "$status" 403 "POST /containers/{id}/stop (user1 cannot stop user2's)" "$body" "FORBIDDEN"
      # Cleanup: user2 stops and deletes own container
      out=$(req_no_content POST "$API/containers/$CONTAINER_ID_USER2/stop" "$TOKEN_USER2")
      if [[ $(echo "$out" | tail -1) -eq 202 ]]; then
        stop_job=$(echo "$out" | sed '$d' | jq -r '.job_id')
        for j in {1..30}; do
          out=$(req_no_content GET "$API/jobs/$stop_job" "$TOKEN_USER2")
          s=$(echo "$out" | sed '$d' | jq -r '.status')
          [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && break
          sleep 0.1
        done
        out=$(req_no_content DELETE "$API/containers/$CONTAINER_ID_USER2" "$TOKEN_USER2")
      fi
    fi
  else
    skip "Container isolation - user2 create failed"
  fi
else
  skip "Container isolation - user2 token not set"
fi

# Jobs 跨使用者隔離：user2 不能查詢 user1 的 Job → 404
JOB_ID_USER1="$JOB_ID"
if [[ -n "$TOKEN_USER2" && "$TOKEN_USER2" != "null" && -n "$JOB_ID_USER1" ]]; then
  out=$(req_no_content GET "$API/jobs/$JOB_ID_USER1" "$TOKEN_USER2")
  body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
  check "$status" 404 "GET /jobs/{id} (other user)" "$body" "JOB_NOT_FOUND"
fi

# 容器生命週期測試：Stop（Running → Exited）
out=$(req_no_content POST "$API/containers/$CONTAINER_ID/stop" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 202 "POST /containers/{id}/stop"
STOP_JOB=$(echo "$body" | jq -r '.job_id')
for i in {1..50}; do
  out=$(req_no_content GET "$API/jobs/$STOP_JOB" "$TOKEN")
  body=$(echo "$out" | sed '$d')
  s=$(echo "$body" | jq -r '.status')
  [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && break
  sleep 0.1
done

# 容器生命週期測試：Start（Exited → Running）
out=$(req_no_content POST "$API/containers/$CONTAINER_ID/start" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 202 "POST /containers/{id}/start"
START_JOB=$(echo "$body" | jq -r '.job_id')
for i in {1..50}; do
  out=$(req_no_content GET "$API/jobs/$START_JOB" "$TOKEN")
  body=$(echo "$out" | sed '$d')
  s=$(echo "$body" | jq -r '.status')
  [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && break
  sleep 0.1
done

# 並發控制：已在運行的容器再次 Start → 409 CONFLICT
out=$(req_no_content POST "$API/containers/$CONTAINER_ID/start" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 409 "POST /containers/{id}/start (already running)" "$body" "CONFLICT"

# 容器生命週期測試：Stop（Running → Exited）
out=$(req_no_content POST "$API/containers/$CONTAINER_ID/stop" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 202 "POST /containers/{id}/stop"
STOP_JOB=$(echo "$body" | jq -r '.job_id')
for i in {1..50}; do
  out=$(req_no_content GET "$API/jobs/$STOP_JOB" "$TOKEN")
  body=$(echo "$out" | sed '$d')
  s=$(echo "$body" | jq -r '.status')
  [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && break
  sleep 0.2
done

# 並發控制：已停止的容器再次 Stop → 409 CONFLICT
out=$(req_no_content POST "$API/containers/$CONTAINER_ID/stop" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 409 "POST /containers/{id}/stop (already stopped)" "$body" "CONFLICT"

# 刪除保護：運行中的容器不允許刪除 → 409，停止後可刪除 → 202
NAME2="api-test-del-$(date +%s)"
out=$(req POST "$API/containers" "$TOKEN" "application/json" "{\"name\":\"$NAME2\",\"environment\":\"python-3.10\",\"command\":[\"sleep\",\"60\"]}")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
if [[ $status -eq 202 ]]; then
  JOB2=$(echo "$body" | jq -r '.job_id')
  CID=""
  for i in {1..100}; do
    out=$(req_no_content GET "$API/jobs/$JOB2" "$TOKEN")
    body=$(echo "$out" | sed '$d'); s=$(echo "$body" | jq -r '.status')
    CID=$(echo "$body" | jq -r '.target_resource_id // empty')
    [[ "$s" == "SUCCESS" && -n "$CID" && "$CID" != "null" ]] && break
    [[ "$s" == "FAILED" ]] && { fail "Container create failed"; CID=""; break; }
    sleep 0.1
  done
  if [[ -n "$CID" && "$CID" != "null" ]]; then
    CREATED_CONTAINER_IDS+=("$CID")
    out=$(req_no_content POST "$API/containers/$CID/start" "$TOKEN")
    [[ $(echo "$out" | tail -1) -eq 202 ]] && sleep 0.5
    out=$(req_no_content DELETE "$API/containers/$CID" "$TOKEN")
    body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
    check "$status" 409 "DELETE /containers/{id} (while running)" "$body" "CONFLICT"
    out=$(req_no_content POST "$API/containers/$CID/stop" "$TOKEN")
    stop_body=$(echo "$out" | sed '$d'); stop_job=$(echo "$stop_body" | jq -r '.job_id')
    for j in {1..30}; do
      out=$(req_no_content GET "$API/jobs/$stop_job" "$TOKEN")
      s=$(echo "$out" | sed '$d' | jq -r '.status')
      [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && break
      sleep 0.2
    done
    out=$(req_no_content DELETE "$API/containers/$CID" "$TOKEN")
    status=$(echo "$out" | tail -1)
    check "$status" 202 "DELETE /containers/{id} (when stopped)"
    # Poll delete job and verify consistency
    del_body=$(echo "$out" | sed '$d')
    del_job=$(echo "$del_body" | jq -r '.job_id')
    for k in {1..50}; do
      out=$(req_no_content GET "$API/jobs/$del_job" "$TOKEN")
      s=$(echo "$out" | sed '$d' | jq -r '.status')
      [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && break
      sleep 0.2
    done
    out=$(req_no_content GET "$API/containers" "$TOKEN")
    body=$(echo "$out" | sed '$d')
    found=$(echo "$body" | jq -r --arg cid "$CID" '.data[] | select(.container_id == $cid) | .container_id // empty')
    [[ -z "$found" ]] && ok "Containers consistency: deleted container not in list" || fail "Containers consistency: deleted container still in list"
  fi
else
  skip "DELETE (while running) - create failed"
fi

# 刪除不存在的容器 → 404
out=$(req_no_content DELETE "$API/containers/550e8400-e29b-41d4-a716-446655440000" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 404 "DELETE /containers/{id} (not found)" "$body" "CONTAINER_NOT_FOUND"

# 刪除主測試容器（已在前面停止）+ 一致性驗證
out=$(req_no_content DELETE "$API/containers/$CONTAINER_ID" "$TOKEN")
status=$(echo "$out" | tail -1)
check "$status" 202 "DELETE /containers/{id} (cleanup)"
# Poll delete job and verify consistency
del_body=$(echo "$out" | sed '$d')
del_job=$(echo "$del_body" | jq -r '.job_id')
for k in {1..50}; do
  out=$(req_no_content GET "$API/jobs/$del_job" "$TOKEN")
  s=$(echo "$out" | sed '$d' | jq -r '.status')
  [[ "$s" == "SUCCESS" || "$s" == "FAILED" ]] && break
  sleep 0.2
done
out=$(req_no_content GET "$API/containers" "$TOKEN")
body=$(echo "$out" | sed '$d')
found=$(echo "$body" | jq -r --arg cid "$CONTAINER_ID" '.data[] | select(.container_id == $cid) | .container_id // empty')
[[ -z "$found" ]] && ok "Containers consistency: deleted main container not in list" || fail "Containers consistency: deleted main container still in list"

# --- 5. 非同步任務查詢測試 ---
# 驗證 Job 端點：查詢自己的 Job → 200、查詢不存在的 Job → 404。
echo ""
echo "--- 5. Jobs ---"
# 建立另一個容器以取得 job_id
out=$(req POST "$API/containers" "$TOKEN" "application/json" "{\"name\":\"job-test-$(date +%s)\",\"environment\":\"python-3.10\",\"command\":[\"echo\",\"1\"]}")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
JOB_ID=$(echo "$body" | jq -r '.job_id')
job_cid=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/jobs/$JOB_ID" | jq -r '.target_resource_id // empty')
[[ -z "$job_cid" || "$job_cid" == "null" ]] && poll_job "$TOKEN" "$JOB_ID" 50 >/dev/null && job_cid=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/jobs/$JOB_ID" | jq -r '.target_resource_id // empty')
[[ -n "$job_cid" && "$job_cid" != "null" ]] && CREATED_CONTAINER_IDS+=("$job_cid")
out=$(req_no_content GET "$API/jobs/$JOB_ID" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 200 "GET /jobs/{id} (own)"

out=$(req_no_content GET "$API/jobs/550e8400-e29b-41d4-a716-446655440000" "$TOKEN")
body=$(echo "$out" | sed '$d'); status=$(echo "$out" | tail -1)
check "$status" 404 "GET /jobs/{id} (not found)" "$body" "JOB_NOT_FOUND"

# --- 6. 後置清理：還原到測試前狀態 ---
# 刪除測試期間建立的檔案和容器（Stop + Delete），清除殘留 Docker 容器。
echo ""
echo "--- 6. Post-test cleanup ---"
# 刪除測試期間建立的檔案（user1 和 user2）
[[ -n "$FILE_ID_USER1" && "$FILE_ID_USER1" != "null" ]] && curl -s -X DELETE -H "Authorization: Bearer $TOKEN" "$API/files/$FILE_ID_USER1" >/dev/null
[[ -n "$FILE_ID_USER2" && "$FILE_ID_USER2" != "null" && -n "$TOKEN_USER2" ]] && curl -s -X DELETE -H "Authorization: Bearer $TOKEN_USER2" "$API/files/$FILE_ID_USER2" >/dev/null
# Stop and delete all tracked containers
for cid in "${CREATED_CONTAINER_IDS[@]}"; do [[ -n "$cid" ]] && stop_and_delete_container "$TOKEN" "$cid"; done
for cid in "${CREATED_CONTAINER_IDS_USER2[@]}"; do [[ -n "$cid" && -n "$TOKEN_USER2" ]] && stop_and_delete_container "$TOKEN_USER2" "$cid"; done
# Clean any remaining aidms workload containers (exclude postgres, server)
for name in $(docker ps -a --format '{{.Names}}' 2>/dev/null | grep '^aidms-' || true); do
  [[ "$name" == "aidms-postgres" || "$name" == "aidms-server" ]] && continue
  docker rm -f "$name" 2>/dev/null || true
done
echo "  Cleanup complete (Files, Containers, Docker)"
echo ""

echo "=== Summary: $PASSED passed, $FAILED failed ==="
echo "For full PG reset: run 'make clean-test-env' (stop server first)."
[[ $FAILED -eq 0 ]] && exit 0 || exit 1
