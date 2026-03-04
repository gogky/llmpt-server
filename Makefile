.PHONY: help db-up db-down db-logs test-db tracker web-server start-backend test-tracker clean dev frontend-install frontend-build frontend-dev deploy

help: ## 显示帮助信息
	@echo "可用命令:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

db-up: ## 启动本地依赖容器（仅数据库，不启动 Nginx）
	docker compose up -d mongodb redis
	@echo "✅ 数据库已启动"
	@echo "MongoDB: localhost:27017"
	@echo "Redis: localhost:6379"

db-down: ## 停止数据库
	docker compose down
	@echo "✅ 数据库已停止"

db-logs: ## 查看数据库日志
	docker compose logs -f

test-db: ## 测试数据库连接
	@echo "🧪 测试数据库连接..."
	cd cmd/test-db && go run main.go

tracker: ## 启动 Tracker Server
	@echo "🚀 启动 Tracker Server..."
	cd cmd/tracker && go run main.go

web-server: ## 启动 Web API Server
	@echo "🚀 启动 Web API Server..."
	cd cmd/web-server && go run main.go

start-backend: ## 并行启动 Tracker 和 Web Server
	@echo "🚀 并行启动所有后端服务..."
	@bash -c ' \
		(cd cmd/tracker && exec go run main.go) & PID1=$$!; \
		(cd cmd/web-server && exec go run main.go) & PID2=$$!; \
		trap "kill -TERM $$PID1 $$PID2 2>/dev/null || true" SIGINT SIGTERM EXIT; \
		wait -n; \
		echo "⚠️ 某个服务已退出，正在清理所有后端进程..."'

test-tracker: ## 测试 Tracker 功能
	@echo "🧪 测试 Tracker..."
	cd cmd/test-tracker && go run main.go

clean: ## 清理临时文件
	go clean
	rm -f cmd/test-db/test-db
	rm -f cmd/tracker/tracker
	rm -f cmd/test-tracker/test-tracker
	rm -rf frontend/dist

build-tracker: ## 编译 Tracker Server
	@echo "🔨 编译 Tracker Server..."
	cd cmd/tracker && go build -o tracker main.go
	@echo "✅ 编译完成: cmd/tracker/tracker"

build-all: ## 编译所有程序
	@echo "🔨 编译所有程序..."
	cd cmd/test-db && go build -o test-db main.go
	cd cmd/tracker && go build -o tracker main.go
	cd cmd/test-tracker && go build -o test-tracker main.go
	@echo "✅ 编译完成"

redis-cli: ## 连接到 Redis CLI
	docker exec -it llmpt-redis redis-cli

mongo-cli: ## 连接到 MongoDB CLI
	docker exec -it llmpt-mongodb mongosh -u admin -p admin123 --authenticationDatabase admin

deps: ## 下载依赖（Go + 前端）
	go mod download
	go mod tidy
	cd frontend && npm install

fmt: ## 格式化代码
	go fmt ./...

vet: ## 代码检查
	go vet ./...

lint: fmt vet ## 代码格式化和检查

frontend-install: ## 安装前端依赖
	@echo "📦 安装前端依赖..."
	cd frontend && npm install
	@echo "✅ 前端依赖安装完成"

frontend-build: ## 构建前端生产包
	@echo "🔨 构建前端..."
	cd frontend && npm run build
	@echo "✅ 前端构建完成: frontend/dist/"

frontend-dev: ## 启动前端开发服务器
	@echo "🚀 启动前端开发服务器..."
	cd frontend && npm run dev

dev: db-up start-backend ## 一键起飞：启动本地开发环境 (数据库容器 + 源码启动所有后端)

deploy: frontend-build ## 部署：构建前端 + 启动所有容器（数据库 + Nginx）+ 后端
	@echo "🚀 启动所有基础设施容器..."
	docker compose up -d
	@echo "✅ 全部容器已启动"
	@echo "🌐 请在另一个终端运行 make start-backend 启动后端服务"
	@echo "📡 网站地址: http://localhost"
