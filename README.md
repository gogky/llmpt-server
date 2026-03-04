# LLM-PT Server

HuggingFace 模型 P2P 加速的中心服务端，包含 Tracker（BT 追踪器）、Web API（模型元数据管理）以及前端监控面板。

## 1. 基础环境

| 组件 | 版本要求 |
|------|---------|
| Go | 1.23+ |
| Node.js | 18+ |
| Docker & Docker Compose | 最新稳定版 |

> MongoDB 和 Redis 通过 Docker Compose 自动启动，无需单独安装。

## 2. 快速开始

### 2.1 复制配置文件

```bash
cp .env.example .env
```

编辑 `.env` 根据实际环境修改配置。程序启动时会自动从项目根目录加载 `.env` 文件；如果文件不存在，则使用系统环境变量或代码中的默认值。

### 2.2 本地开发（推荐）

```bash
# 安装所有依赖（Go + 前端）
make deps

# 一键启动开发环境：数据库容器 + 后端服务
make dev

# （另开终端）启动前端开发服务器
make frontend-dev
```

前端开发服务器运行在 `http://localhost:5173`，Vite 会自动将 `/api` 请求代理到后端 8080 端口。

### 2.3 生产部署

```bash
# 1. 安装依赖
make deps

# 2. 构建前端 + 启动全部容器（MongoDB + Redis + Nginx）
make deploy

# 3. 启动后端服务（另开终端，或使用 systemd/pm2 守护）
make start-backend
```

部署完成后通过 `http://<服务器IP>` 即可访问。

## 3. 配置说明

所有配置项都可以通过 `.env` 文件或系统环境变量设置。参考 [.env.example](.env.example) 了解全部可用选项。

### 关键配置

| 环境变量 | 默认值 | 说明 |
|---------|-------|------|
| `SERVER_PORT` | `8080` | Web API 监听端口 |
| `TRACKER_PORT` | `8081` | Tracker 监听端口 |
| `TRACKER_URL` | `http://localhost/announce` | 写入 .torrent 文件的 announce URL，**生产环境必须改为公网地址** |
| `MONGODB_URI` | `mongodb://admin:admin123@localhost:27017` | MongoDB 连接字符串 |
| `REDIS_HOST` | `localhost` | Redis 地址 |
| `ENVIRONMENT` | `development` | 运行环境 |

> **⚠️ 生产部署注意：** `TRACKER_URL` 必须设置为服务器的公网 IP 或域名（如 `http://your-domain.com/announce`），该地址会被写入 `.torrent` 种子文件供客户端连接。

## 4. 架构说明

### Nginx 反向代理

Nginx 对外提供统一的 80 端口，请求路由规则：

| 路径 | 目标 |
|-----|------|
| `/announce` | → Tracker (默认 8081) |
| `/api/*` | → Web API (默认 8080) |
| `/health` | → Web API 健康检查 |
| `/*` (其他) | → 前端静态文件 (Vue SPA) |

### 端口安全

- MongoDB (27017) 和 Redis (6379) **仅绑定 `127.0.0.1`**，禁止外网直连
- 仅 Nginx 80 端口对外开放

### 服务组件

```
┌──────────────────────────────────────────────┐
│                  Nginx (:80)                  │
│  /announce → Tracker  |  /api → Web API      │
│               /* → 前端静态文件               │
└──────┬─────────────────────┬─────────────────┘
       │                     │
  ┌────▼────┐          ┌─────▼─────┐
  │ Tracker │          │  Web API  │
  │ (:8081) │          │  (:8080)  │
  └────┬────┘          └─────┬─────┘
       │                     │
       └──────┬──────────────┘
              │
    ┌─────────▼──────────┐
    │  MongoDB + Redis   │
    │ (127.0.0.1 only)   │
    └────────────────────┘
```

## 5. 常用命令

```bash
make help              # 查看所有可用命令
make dev               # 本地开发：启动数据库 + 后端
make frontend-dev      # 启动前端开发服务器
make frontend-build    # 构建前端生产包
make deploy            # 生产部署：构建前端 + 启动全部容器
make start-backend     # 启动后端服务（Tracker + Web API）
make db-up             # 仅启动数据库容器
make db-down           # 停止所有容器
make db-logs           # 查看容器日志
make redis-cli         # 连接 Redis CLI
make mongo-cli         # 连接 MongoDB CLI
make deps              # 安装所有依赖
make lint              # 代码格式化 + 检查
make clean             # 清理临时文件和构建产物
```
