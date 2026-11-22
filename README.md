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

### 系统依赖

转码流水线需要这些外部程序，缺失时对应格式无法预览（会如实显示失败原因）：

| 程序 | 用途 | 必需性 |
|---|---|---|
| `pdftoppm` / `pdfinfo`（poppler-utils） | PDF 逐页渲染成 PNG | PDF 预览必需 |
| `pdftotext`（poppler-utils） | 提取文本层供全文检索 | 可选，缺失只影响检索 |
| `soffice`（LibreOffice） | Word / PPT / Excel 转 PDF 后预览 | Office 预览必需 |

```bash
# macOS
brew install poppler && brew install --cask libreoffice
# Debian / Ubuntu
apt install poppler-utils libreoffice-writer libreoffice-impress libreoffice-calc
# Windows（PowerShell）
winget install TheDocumentFoundation.LibreOffice
# poppler for Windows: 下载解压后把 bin 加入 PATH
```

后端会在 `PATH` 与各平台标准安装位置查找 `soffice`，
未安装时 Office 文件会标记为 `failed` 并在 `errorReason` 里说明原因。

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

**M1 · 地基与账号**

- [x] 数据库完整 schema（UUIDv7 / 触发器 / 索引 / tsvector 检索）
- [x] 迁移可执行且可回滚（goose）
- [x] Go API 骨架：健康检查、数据库连通探测、优雅关闭
- [x] 前端骨架：Ink & Seal 设计 token、信息流首页、内联预览卡片
- [x] 账号注册 / 登录 / 登出，Argon2id + HttpOnly Cookie + CSRF 双提交
- [x] 论文元数据解析（DOI → Crossref / arXiv 自动抓取）
- [x] 信息流三档 API（关注流 / 全站流 / 热门流），游标分页
- [x] 发帖、转发、点赞、收藏、评论
- [x] 文件上传 + PDF 页图预览（SeaweedFS + pdftoppm）
- [x] 图片 / Markdown / 代码 / LaTeX 站内预览（V-04）
- [x] Word / PPT / Excel 转 PDF 后预览（LibreOffice headless，V-03）
- [x] 统一检索（tsvector + trigram 回退）与通知中心
- [x] SSR / SEO：论文详情与个人主页改为 Server Component，含 sitemap / robots

**M2 · 群组**

- [x] 群组 CRUD、成员与角色（owner > admin > member）
- [x] 邀请码、入群申请与审批、群内动态

**M3 · 协同编辑**

- [x] 文档 CRUD、文档级权限与成员覆盖
- [x] Node 协同服务（Hocuspocus 4.8 + Yjs），快照落 S3
- [x] TipTap 编辑器：实时协作、协作者光标、版本快照、批注

**M4 · 策展与开放**

- [x] 精选策展：策展集 CRUD、条目增删排序、上下架时间
- [x] 文档导出：PDF / Word / Markdown / LaTeX（保留章节结构，中文经 ctex）
- [x] 文档导入：Word / LaTeX 转 HTML 写进协同编辑器，支持图片、表格与公式渲染
- [x] 开放 API：API Key 管理、`/openapi/v1` 只读端点、scopes、按密钥限流

**M5 · 补齐遗留**

- [x] 帖子编辑与删除（仅作者或运营）、帖子附件
- [x] 通知补全：reply / mention / repost / join_request / doc_edit
- [x] 举报与屏蔽（F-06），屏蔽在查询层生效
- [x] 收藏列表（F-05）
- [x] 引用管理（D-06）：BibTeX 导入 + GB/T 7714 / APA / IEEE 渲染
- [x] 群文件库（G-05）—— 含从帖子附件一键存入
- [x] 文档快照回滚（D-04）—— 服务端主动卸载内存文档，在线用户自动重连生效
- [x] 搜索筛选（S-03）：时间、标签、作者，条件保留在 URL
- [x] PDF 全文检索（S-04）：自写最小文本提取器，支持含文本层的 PDF
- [x] 个人主页发帖 / 转发 Tab（A-03）、用户可被搜索（S-01）

**待做**

- [ ] AI 辅助（摘要 / 相关论文推荐）——按当前排期暂缓
- [ ] 第三方登录（A-02）、机构邮箱认证（A-04）、大V授予后台（A-05）
- [ ] 论文手动补录（P-04）、元数据编辑与合并（P-02）
- [ ] 论文详情页「相关讨论」列表（P-03）
- [ ] 收藏夹分组与公开/私密（F-05）、话题标签页（F-07）
- [ ] 文档大纲面板（D-01）、批注创建入口（D-05）
- [ ] 快照 50 条上限与里程碑命名（D-04）、离线编辑（D-03）
- [ ] 邮件通知与浏览器推送、免打扰时段（N-02 / N-03 / N-06）
- [ ] Redis 缓存与后台任务、埋点、可观测性
- [ ] 扫描版 PDF 的全文检索（需 OCR，当前仅支持含文本层的 PDF）
