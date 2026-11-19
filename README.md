# Thesis Shared · 学术论文社区与协作平台

以「关注学术大V的转发」作为信息筛选机制的学术社区，提供从共读文献到共同成稿的一体化协作空间。

- 产品需求：[docs/product-requirements.md](docs/product-requirements.md)
- 技术架构：[docs/technical-architecture.md](docs/technical-architecture.md)
- 数据模型：[docs/data-model.md](docs/data-model.md)
- 设计语言：[docs/ink-and-seal-design.html](docs/ink-and-seal-design.html)

---

## 技术栈

| 层 | 选型 |
|---|---|
| 前端 | Next.js 15（App Router）+ TypeScript + Tailwind CSS 4 |
| 设计系统 | Ink & Seal（纸 · 墨 · 印），Fraunces / Inter / IBM Plex Mono |
| 后端 | **Go 1.25** + chi 路由 + pgx 连接池 |
| 数据库 | PostgreSQL 18（UUIDv7 主键、citext、pg_trgm） |
| 数据访问 | sqlc（SQL → 类型安全 Go 代码）+ goose 迁移 |
| 对象存储 | MinIO / S3 兼容（预签名 URL） |
| 缓存队列 | Redis 7 + asynq（后续接入） |

> 说明：技术架构文档原定后端为 NestJS，经决策改为 **Go**，本文档与架构文档已同步。

---

## 目录结构

```
thesis-shared/
├─ apps/
│  ├─ api/                      # Go 后端
│  │  ├─ cmd/server/            # 入口
│  │  ├─ internal/
│  │  │  ├─ config/             # 环境配置
│  │  │  ├─ db/                 # 连接池 + sqlc 生成代码
│  │  │  ├─ handler/            # HTTP 处理器
│  │  │  └─ server/             # 路由装配
│  │  ├─ db/migrations/         # goose 迁移（SQL）
│  │  └─ db/queries/            # sqlc 查询（SQL）
│  └─ web/                      # Next.js 前端
│     └─ src/
│        ├─ app/                # App Router 页面
│        ├─ components/         # 组件（卡片、徽章、顶栏）
│        └─ lib/                # API 客户端
├─ infra/docker/                # Compose 与 Dockerfile
└─ docs/                        # 产品 / 架构 / 数据 / 设计文档
```

---

## 快速开始

### 1. 准备数据库

本机已有 PostgreSQL 时（推荐路径 A）：

```bash
# 以超级用户创建角色与库（密码按需修改）
psql -U postgres -h localhost -d postgres \
  -c "CREATE ROLE thesis LOGIN PASSWORD 'thesis_dev' CREATEDB;" \
  -c "CREATE DATABASE thesis OWNER thesis;"
```

或使用容器（路径 B）：

```bash
make infra-up
```

### 2. 执行迁移

```bash
# 安装 goose（一次即可）
go install github.com/pressly/goose/v3/cmd/goose@latest

make db-up      # 应用全部迁移
make db-status  # 查看状态
```

### 3. 启动后端

```bash
make api-run
# → http://localhost:8080/api/v1/health
```

预期输出：

```json
{"status":"ok","env":"development","database":"up","version":"dev"}
```

### 4. 启动前端

```bash
pnpm install
make web-dev
# → http://localhost:3000
```

前端通过 `next.config.ts` 的 rewrites 把 `/api/*` 代理到 Go 服务，浏览器无需跨域配置。

---

## 环境变量

复制 `.env.example` 为 `.env` 并按需修改。关键项：

| 变量 | 说明 | 默认 |
|---|---|---|
| `POSTGRES_DSN` | 数据库连接串 | `postgres://thesis:thesis_dev@localhost:5432/thesis` |
| `API_ADDR` | API 监听地址 | `:8080` |
| `API_INTERNAL_URL` | 前端服务端渲染时访问 API 的地址 | `http://localhost:8080` |
| `JWT_SECRET` | 令牌签名密钥 | 开发默认值，生产必须更换 |

---

## 常用命令

```bash
make help        # 列出全部命令
make db-up       # 迁移
make api-run     # 后端
make web-dev     # 前端
make api-test    # 后端测试
make web-check   # 前端类型检查
```

---

## 当前进度

**已完成（M1 地基）**

- [x] 数据库完整 schema（29 表 / 12 枚举 / UUIDv7 / 触发器 / 索引）
- [x] 迁移可执行且可回滚（goose）
- [x] Go API 骨架：健康检查、数据库连通探测、优雅关闭
- [x] 前端骨架：Ink & Seal 设计 token、信息流首页、内联预览卡片
- [x] 端到端链路验证（PG → Go → Next.js SSR）

**下一步（M1 功能）**

- [ ] 账号注册 / 登录 / JWT 会话
- [ ] 论文元数据解析（DOI / arXiv）
- [ ] 信息流三档 API（关注流 / 全站流 / 热门流）
- [ ] 发帖与转发
- [ ] 文件上传 + 内联预览真实渲染（pdf.js）
