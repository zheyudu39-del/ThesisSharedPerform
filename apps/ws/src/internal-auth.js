import { createHmac, timingSafeEqual } from 'node:crypto'

import { config } from './config.js'

/**
 * 服务间调用的鉴权。
 *
 * Go API 在快照回滚后需要通知协同服务「把这个文档从内存里卸掉」。
 * 这条内部通道不能用用户 JWT：用户令牌代表某个人的权限，
 * 而这里是一次服务对服务的运维动作，与任何用户无关。
 *
 * 方案：共享密钥 + HMAC。Go 侧用 JWT_SECRET 派生出内部密钥，
 * 对「动作 + 文档 ID + 时间戳」签名；本服务用同一套算法验签。
 *
 * 为什么不复用 JWT：JWT 是为了让「持有者」向「多个服务」证明身份，
 * 而这里只有两个进程、一个固定动作，HMAC 更简单也更不容易配错
 * （没有 algorithm 混淆、没有 kid 协商、没有 exp 时钟漂移以外的坑）。
 */

/** 内部密钥 = HMAC(JWT_SECRET, "internal-api")，与 Go 侧约定一致。 */
function deriveKey() {
  return createHmac('sha256', config.jwtSecret).update('internal-api').digest()
}

/** 签名窗口：超过这个时间差的请求视为重放攻击。 */
const MAX_SKEW_MS = 60_000

/**
 * 校验内部请求签名。
 *
 * @param {string} scope 参与签名的动作与目标，如 "reload:{docId}" 或 "snapshot:{docId}"。
 *   把动作写进签名内容，防止一个 reload 签名被挪用来触发快照（或反之）。
 * @param {string|undefined} timestamp 请求头 x-internal-timestamp（毫秒）
 * @param {string|undefined} signature 请求头 x-internal-signature（hex）
 * @returns {{ok: true} | {ok: false, reason: string}}
 */
export function verifyInternalSignature(scope, timestamp, signature) {
  if (!timestamp || !signature) {
    return { ok: false, reason: 'missing_headers' }
  }
  const ts = Number.parseInt(timestamp, 10)
  if (!Number.isFinite(ts)) {
    return { ok: false, reason: 'bad_timestamp' }
  }
  if (Math.abs(Date.now() - ts) > MAX_SKEW_MS) {
    return { ok: false, reason: 'expired' }
  }

  const expected = createHmac('sha256', deriveKey())
    .update(`${scope}:${ts}`)
    .digest('hex')

  // 长度不等时 timingSafeEqual 会抛错，先比长度。
  // 长度本身不是秘密（hex 摘要固定 64 字符），可以安全地提前返回。
  if (signature.length !== expected.length) {
    return { ok: false, reason: 'bad_signature' }
  }
  const a = Buffer.from(signature, 'hex')
  const b = Buffer.from(expected, 'hex')
  if (a.length !== b.length || !timingSafeEqual(a, b)) {
    return { ok: false, reason: 'bad_signature' }
  }
  return { ok: true }
}

/** 供测试与调试：生成签名（生产环境由 Go 侧生成）。 */
export function signInternalRequest(scope, timestamp = Date.now()) {
  const signature = createHmac('sha256', deriveKey())
    .update(`${scope}:${timestamp}`)
    .digest('hex')
  return { timestamp: String(timestamp), signature }
}
