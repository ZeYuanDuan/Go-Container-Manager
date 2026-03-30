.PHONY: test test-integration test-api test-coverage migrate-up migrate-down migrate-fix-dirty clean-test-env docker-up

test:
	go test ./...

# Unit test coverage report
test-coverage:
	go test ./internal/... ./pkg/... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out
	@echo "HTML report: go tool cover -html=coverage.out"

test-integration:
	go test ./tests/integration/... -tags=integration -v

# API test script: runs success + error cases (broader than api.http) against a running server
# Requires: server running, curl, jq. Run 'make clean-test-env' before starting server for clean state.
test-api:
	@./scripts/api_test.sh

# Clean test environment: aidms containers, PG reset, file storage. Run before and after test-api.
clean-test-env:
	@./scripts/clean_test_env.sh

# Docker Compose up with STORAGE_HOST_PATH set (required for file upload + workload containers)
# Persists to .env so "docker compose ps" etc. do not warn about unset variable
docker-up:
	@mkdir -p storage/uploads
	@path="$$(pwd)/storage/uploads"; \
	if grep -q '^STORAGE_HOST_PATH=' .env 2>/dev/null; then \
		(sed 's|^STORAGE_HOST_PATH=.*|STORAGE_HOST_PATH='"$$path"'|' .env > .env.tmp && mv .env.tmp .env) || true; \
	else \
		echo "STORAGE_HOST_PATH=$$path" >> .env; \
	fi
	@docker compose up -d

# Migrations (optional - also run automatically on app startup)
# Loads .env for DATABASE_URL if not set.
migrate-up:
	@bash -c 'set -a; [ -f .env ] && . ./.env; set +a; migrate -path migrations -database "$${DATABASE_URL}" up'

migrate-down:
	@bash -c 'set -a; [ -f .env ] && . ./.env; set +a; migrate -path migrations -database "$${DATABASE_URL}" down'

# Fix "Dirty database version" error. Sync schema_migrations to actual DB state.
# If tables already exist (e.g. users, files), force to latest version (6).
migrate-fix-dirty:
	@bash -c 'set -a; [ -f .env ] && . ./.env; set +a; migrate -path migrations -database "$${DATABASE_URL}" force 6'
	@$(MAKE) migrate-up
