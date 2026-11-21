import 'dotenv/config'

/**
 * 协同服务的运行时配置。
 *
 * 变量名与 Go API 的 internal/config 保持一致（JWT_SECRET、S3_*、POSTGRES_DSN），
 * 这样两个进程可以共用同一份 .env，不需要为协同服务维护第二套命名。
 * 服务专属的开关统一用 WS_ 前缀，避免与 API 的配置混淆。
 */

/** 与 apps/api/internal/config/config.go 中的 devJWTSecret 完全一致。 */
const DEV_JWT_SECRET = 'dev-only-change-me'

function readString(name, fallback) {
  const raw = process.env[name]
  if (raw === undefined || raw.trim() === '') return fallback
  return raw.trim()
}

function readInt(name, fallback) {
  const raw = readString(name, '')
  if (raw === '') return fallback
  const value = Number.parseInt(raw, 10)
  if (!Number.isFinite(value)) {
    throw new Error(`环境变量 ${name} 必须是整数，当前值：${raw}`)
  }
  return value
}

function readBool(name, fallback) {
  const raw = readString(name, '')
  if (raw === '') return fallback
  return raw === 'true' || raw === '1'
}

export const config = {
  env: readString('APP_ENV', 'development'),
  logLevel: readString('LOG_LEVEL', 'info'),

  // ---- WebSocket 监听 ----
  port: readInt('WS_PORT', 1234),
  // 默认只监听本机，生产环境用 WS_ADDRESS=0.0.0.0 显式放开。
  address: readString('WS_ADDRESS', '0.0.0.0'),
  // 健康检查端口，独立于 WebSocket 端口；设为 0 可关闭。
  healthPort: readInt('WS_HEALTH_PORT', 1235),

  // ---- PostgreSQL ----
  postgresDsn: readString(
    'POSTGRES_DSN',
    'postgres://thesis:thesis_dev@localhost:5432/thesis?sslmode=disable',
  ),
  pgPoolMax: readInt('WS_PG_POOL_MAX', 10),

  // ---- JWT ----
  jwtSecret: readString('JWT_SECRET', DEV_JWT_SECRET),

  // ---- 对象存储（SeaweedFS / MinIO）----
  s3: {
    endpoint: readString('S3_ENDPOINT', '127.0.0.1:8333'),
    accessKey: readString('S3_ACCESS_KEY', 'minioadmin'),
    secretKey: readString('S3_SECRET_KEY', 'minioadmin'),
    bucket: readString('S3_BUCKET', 'thesis-files'),
    useSSL: readBool('S3_USE_SSL', false),
  },

  // ---- 持久化节奏 ----
  // Hocuspocus 内置 debounce：最后一次编辑后多久落快照。
  debounce: readInt('WS_DEBOUNCE', 2000),
  // 持续编辑时的最长等待，避免永远不落盘。
  maxDebounce: readInt('WS_MAX_DEBOUNCE', 10000),
  // 增量 update 合并写入对象存储的间隔。
  updateFlushMs: readInt('WS_UPDATE_FLUSH_MS', 1000),
}

/** 生产环境禁止使用开发默认密钥，与 Go 端的启动校验保持一致。 */
export function validateConfig(cfg = config) {
  const isProduction = ['production', 'prod'].includes(cfg.env.toLowerCase())
  if (isProduction) {
    if (cfg.jwtSecret === DEV_JWT_SECRET) {
      throw new Error('生产环境必须显式设置 JWT_SECRET，禁止使用默认值')
    }
    if (Buffer.byteLength(cfg.jwtSecret, 'utf8') < 32) {
      throw new Error('生产环境 JWT_SECRET 长度至少 32 字节')
    }
  }
}
