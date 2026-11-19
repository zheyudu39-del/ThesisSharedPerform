# =====================================================================
# Thesis Shared · 开发命令入口
# =====================================================================

API_DIR   := apps/api
WEB_DIR   := apps/web
MIG_DIR   := $(API_DIR)/db/migrations
DB_URL    ?= postgres://thesis:thesis_dev@localhost:5432/thesis?sslmode=disable

.PHONY: help
help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---- 后端 ----
.PHONY: api-run
api-run: ## 启动 Go API（默认 :8080）
	cd $(API_DIR) && POSTGRES_DSN="$(DB_URL)" go run ./cmd/server

.PHONY: api-build
api-build: ## 编译 Go API
	cd $(API_DIR) && go build -o bin/server ./cmd/server

.PHONY: api-test
api-test: ## 运行 Go 测试
	cd $(API_DIR) && go test ./...

.PHONY: api-fmt
api-fmt: ## 格式化 Go 代码
	cd $(API_DIR) && gofmt -w .

.PHONY: sqlc
sqlc: ## 由 SQL 生成类型安全 Go 代码
	cd $(API_DIR) && sqlc generate

# ---- 数据库 ----
.PHONY: db-up
db-up: ## 执行全部迁移
	cd $(API_DIR) && GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir db/migrations up

.PHONY: db-down
db-down: ## 回滚最近一次迁移
	cd $(API_DIR) && GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir db/migrations down

.PHONY: db-status
db-status: ## 查看迁移状态
	cd $(API_DIR) && GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir db/migrations status

# ---- 前端 ----
.PHONY: web-dev
web-dev: ## 启动 Next.js 开发服务器（:3000）
	cd $(WEB_DIR) && pnpm dev

.PHONY: web-build
web-build: ## 构建前端
	cd $(WEB_DIR) && pnpm build

.PHONY: web-check
web-check: ## 前端类型检查
	cd $(WEB_DIR) && pnpm typecheck

# ---- 基础设施 ----
.PHONY: infra-up
infra-up: ## 启动 PostgreSQL / Redis / MinIO 容器
	docker compose -f infra/docker/docker-compose.yml up -d

.PHONY: infra-down
infra-down: ## 停止容器
	docker compose -f infra/docker/docker-compose.yml down

# ---- 复合 ----
.PHONY: dev
dev: ## 同时启动 API 与前端（需两个终端，或用此命令后台跑 API）
	@echo "启动 API（后台）…"
	@cd $(API_DIR) && POSTGRES_DSN="$(DB_URL)" go run ./cmd/server & \
	cd $(WEB_DIR) && pnpm dev
