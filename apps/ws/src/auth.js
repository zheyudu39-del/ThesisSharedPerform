import pg from 'pg'
import { jwtVerify } from 'jose'

import { config } from './config.js'
import { logger } from './logger.js'

/**
 * 鉴权与文档权限。
 *
 * 两条独立链路：
 *   1. verifyJWT —— 校验 Go API 签发的 HS256 令牌（sub = 用户 UUID）；
 *   2. checkDocumentAccess —— 直连 PG 查文档、群成员关系与文档级 ACL。
 *
 * 直连 PG 而不是回调 Go API，是为了让每次 WebSocket 建连只依赖数据库，
 * 不把协同服务的可用性绑在 API 进程上；两边共用同一个库与同一套枚举。
 */

const { Pool } = pg

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/** 携带 reason 的错误：Hocuspocus 会把 reason 回传给客户端。 */
export class AuthError extends Error {
  constructor(message, reason = 'permission-denied') {
    super(message)
    this.name = 'AuthError'
    this.reason = reason
  }
}

let pool = null

/** 惰性创建连接池：模块被 import 但未启动服务时不占用数据库连接。 */
function getPool() {
  if (!pool) {
    pool = new Pool({
      connectionString: config.postgresDsn,
      max: config.pgPoolMax,
      // 建连失败不要无限重试，让鉴权快速失败而不是把连接挂死。
      connectionTimeoutMillis: 5000,
      idleTimeoutMillis: 30_000,
    })
    pool.on('error', (err) => {
      logger.error('PG 连接池错误', err)
    })
  }
  return pool
}

const jwtKey = new TextEncoder().encode(config.jwtSecret)

/**
 * 校验 JWT，返回 { userId, payload }。
 * 仅接受 HS256 —— 与 Go 端 identity.TokenManager 保持一致，
 * 显式限定算法可防止 alg=none / 算法混淆攻击。
 */
export async function verifyJWT(token) {
  if (typeof token !== 'string' || token.trim() === '') {
    throw new AuthError('缺少访问令牌', 'unauthorized')
  }

  let payload
  try {
    ;({ payload } = await jwtVerify(token, jwtKey, {
      algorithms: ['HS256'],
      // 允许 30 秒时钟偏差，避免协同服务与 API 主机时间轻微漂移时误拒。
      clockTolerance: 30,
    }))
  } catch (err) {
    logger.debug(`JWT 校验失败: ${err?.code ?? err?.message ?? err}`)
    throw new AuthError('访问令牌无效或已过期', 'unauthorized')
  }

  const userId = typeof payload.sub === 'string' ? payload.sub : ''
  if (!UUID_RE.test(userId)) {
    throw new AuthError('访问令牌缺少有效的用户标识', 'unauthorized')
  }

  return { userId, payload }
}

/**
 * 查询用户对某文档的访问权限。
 *
 * 角色判定顺序（前者优先）：
 *   1. doc_members 中的显式角色；
 *   2. 文档创建者视为 owner；
 *   3. 群 owner/admin 视为 owner（群管理者可管理群内文档）；
 *   4. 普通群成员取 documents.default_role；
 *   5. 其余一律拒绝（文档不存在与无权访问对外都表现为 404 语义）。
 *
 * @returns {{ role: 'owner'|'editor'|'viewer', canWrite: boolean,
 *             groupId: string, title: string, defaultRole: string,
 *             latestSnapshot: number }}
 */
export async function checkDocumentAccess(userId, docId) {
  if (!UUID_RE.test(String(userId ?? ''))) {
    throw new AuthError('用户标识无效', 'unauthorized')
  }
  if (!UUID_RE.test(String(docId ?? ''))) {
    throw new AuthError('文档标识无效', 'not-found')
  }

  let rows
  try {
    ;({ rows } = await getPool().query(
      `SELECT d.id,
              d.group_id,
              d.title,
              d.status,
              d.default_role,
              d.latest_snapshot,
              d.created_by,
              gm.role AS group_role,
              dm.role AS doc_role
         FROM documents d
         LEFT JOIN group_members gm
                ON gm.group_id = d.group_id AND gm.user_id = $2
         LEFT JOIN doc_members dm
                ON dm.doc_id = d.id AND dm.user_id = $2
        WHERE d.id = $1
          AND d.deleted_at IS NULL`,
      [docId, userId],
    ))
  } catch (err) {
    logger.error(`查询文档权限失败 doc=${docId}`, err)
    throw new AuthError('权限校验暂时不可用', 'internal-error')
  }

  const row = rows[0]
  if (!row) {
    throw new AuthError('文档不存在', 'not-found')
  }

  let role = null
  if (row.doc_role) {
    role = row.doc_role
  } else if (row.created_by === userId) {
    role = 'owner'
  } else if (row.group_role === 'owner' || row.group_role === 'admin') {
    role = 'owner'
  } else if (row.group_role === 'member') {
    role = row.default_role
  }

  if (!role) {
    throw new AuthError('无权访问该文档', 'forbidden')
  }

  return {
    role,
    canWrite: role !== 'viewer',
    groupId: row.group_id,
    title: row.title,
    defaultRole: row.default_role,
    latestSnapshot: row.latest_snapshot,
  }
}

/**
 * 从鉴权载荷中提取 token。支持三种来源（按优先级）：
 *   1. Hocuspocus Auth 消息里的 token（provider 的 token 选项）；
 *   2. WebSocket URL 查询参数 ?token=xxx（前端当前使用的方式）；
 *   3. 握手 Cookie 中的 token / access_token（同域部署时的备选）。
 */
export function extractToken({ token, requestParameters, requestHeaders } = {}) {
  if (typeof token === 'string' && token.trim() !== '') return token.trim()

  const fromQuery = requestParameters?.get?.('token')
  if (typeof fromQuery === 'string' && fromQuery.trim() !== '') return fromQuery.trim()

  const cookie = requestHeaders?.get?.('cookie')
  if (cookie) {
    for (const part of cookie.split(';')) {
      const idx = part.indexOf('=')
      if (idx === -1) continue
      const name = part.slice(0, idx).trim()
      if (name !== 'token' && name !== 'access_token') continue
      const value = part.slice(idx + 1).trim()
      if (value !== '') return decodeURIComponent(value)
    }
  }

  return ''
}

/** 关闭连接池，供优雅退出与测试使用。 */
export async function closeAuth() {
  if (pool) {
    const closing = pool
    pool = null
    await closing.end()
  }
}

/** 健康检查用：数据库是否可查询。 */
export async function pingDatabase() {
  const { rows } = await getPool().query('SELECT 1 AS ok')
  return rows[0]?.ok === 1
}
