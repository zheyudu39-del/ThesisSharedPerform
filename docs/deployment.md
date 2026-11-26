# 部署指南（单机 Docker Compose）

面向：一台 Linux 服务器（Ubuntu 22.04 / 24.04），通过 IP 或域名对外提供访问。

## 0. 架构

```
                    ┌──────────────┐
   浏览器 ──:80──▶  │    nginx     │
                    └──────┬───────┘
        ┌──────────────────┼──────────────────┬─────────────┐
        ▼                  ▼                  ▼             ▼
   /api/  → api:8080   / → web:3000   /collab → ws:1234   /thesis-files/ → s3:8333
                           │                  │             │
                           └──────────┬───────┴─────────────┘
                                      ▼
                              postgres:5432 / s3:8333
```

只有 nginx 映射到宿主机 80 端口，其余服务在内部网络里，不对外暴露。

## 1. 服务器准备

```bash
# 安装 Docker 与 compose 插件（Ubuntu / Debian）
sudo apt update
sudo apt install -y docker.io docker-compose-v2 git
sudo systemctl enable --now docker
sudo usermod -aG docker "$USER"   # 之后重新登录，或每次命令前加 sudo

# 防火墙放行 80
sudo ufw allow 80/tcp
```

> 云服务器（腾讯云/阿里云）还需要在**控制台安全组**里放行 80 端口，这一步 ufw 管不到。

## 2. 拉取代码

```bash
cd ~
git clone https://github.com/zheyudu39-del/ThesisSharedPerform.git thesis-shared
cd thesis-shared
```

如果是私有仓库，用带 token 的地址，或先在服务器上配好 SSH key。

## 3. 配置环境变量

```bash
cp infra/docker/.env.prod.example .env.prod
```

逐项填写，**必须改的三项**：

```bash
# 数据库密码
POSTGRES_PASSWORD=<强密码>

# 认证密钥：至少 32 字节
openssl rand -base64 48
# 把输出填到 JWT_SECRET

# 对象存储密钥（随便两个随机串，自己记住即可）
S3_ACCESS_KEY=<随机串>
S3_SECRET_KEY=<随机串>
```

把 IP 换成你的服务器地址：

```bash
NEXT_PUBLIC_SITE_URL=http://152.136.255.253
S3_PUBLIC_ENDPOINT=152.136.255.253
NEXT_PUBLIC_WS_URL=ws://152.136.255.253/collab
WS_PUBLIC_URL=ws://152.136.255.253/collab
```

`S3_PUBLIC_ENDPOINT` 只写 host，**不要带 `http://` 也不要带端口**（端口由 nginx 的 80 提供）。
写错会表现为上传返回 `403 SignatureDoesNotMatch`。

> **为什么 `APP_ENV=development`**：生产模式下的会话 Cookie 带 `Secure` 标记，
> 浏览器在 `http://` 下会直接丢弃，症状是「登录接口 200 但立刻变成未登录」。
> 等换成 HTTPS 后再改成 `production`。

## 4. 构建并启动

```bash
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod up -d --build
```

首次构建约 5–10 分钟（要装 LibreOffice、编译 Go、构建 Next）。

看进度与日志：

```bash
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod ps
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod logs -f api
```

`migrate` 容器会在 api 启动前自动执行数据库迁移，跑完即退出（状态显示 `Exited (0)` 是正常的）。

## 5. 验证

```bash
# 后端存活 + 数据库连通
curl http://127.0.0.1/api/v1/health
# {"status":"ok","env":"development","database":"up","version":"prod"}

# 前端
curl -I http://152.136.255.253/
```

浏览器打开 `http://152.136.255.253`，注册第一个账号。

### 设为管理员

第一个注册的账号是普通用户，需要手动提权：

```bash
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod \
  exec postgres psql -U thesis -d thesis \
  -c "UPDATE users SET role='admin' WHERE handle='你的句柄';"
```

之后即可访问 `/admin`。

## 6. 日常运维

```bash
# 更新代码后重新部署
git pull
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod up -d --build

# 只重建某个服务
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod up -d --build web

# 查看日志
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod logs -f --tail=200

# 停止（保留数据）
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod down

# 备份数据库
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod \
  exec -T postgres pg_dump -U thesis thesis | gzip > backup-$(date +%F).sql.gz
```

数据存放在 Docker 命名卷 `pgdata` / `s3data` 中，`down` 不会删除；只有 `down -v` 才会。

## 7. 换成域名 + HTTPS

1. 域名 A 记录指向服务器 IP，安全组放行 443。
2. 在 `infra/docker/nginx.conf` 里加一个 443 server 块（证书可用 certbot 申请，
   或换成 Caddy 自动签发）。
3. `.env.prod` 改为：

```bash
APP_ENV=production          # 开启 Secure Cookie，必须走 HTTPS
NEXT_PUBLIC_SITE_URL=https://your-domain.com
S3_PUBLIC_ENDPOINT=your-domain.com
NEXT_PUBLIC_WS_URL=wss://your-domain.com/collab
WS_PUBLIC_URL=wss://your-domain.com/collab
```

4. 重新构建（`NEXT_PUBLIC_*` 是构建期变量，必须重建 web 镜像）：

```bash
docker compose -f infra/docker/docker-compose.prod.yml --env-file .env.prod up -d --build
```

## 8. 已知限制

| 项 | 说明 |
|---|---|
| 单机部署 | 所有服务在一台机器上，数据库与应用争抢资源；规模上来后先把 postgres 独立出去 |
| 无自动备份 | 需要自己加 cron 跑上面的 `pg_dump` |
| HTTP 明文 | 用 IP + HTTP 阶段登录凭据明文传输，仅适合内部试用 |
| 首次构建慢 | LibreOffice 体积大（约 500MB），构建缓存命中后后续部署会快很多 |
| 转码依赖 | 镜像内已装 poppler-utils 与 LibreOffice，Office/PDF 预览开箱可用 |
