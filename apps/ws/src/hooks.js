import { AuthError, checkDocumentAccess, extractToken, verifyJWT } from './auth.js'
import { loadDocument, storeDocument, updateJournal } from './persistence.js'
import { isStoreSuppressed } from './reload.js'
import { logger } from './logger.js'

/**
 * Hocuspocus 钩子。
 *
 * 全部钩子以单个 extension 对象注册，原因有二：
 *   1. onAuthenticate 与 onChange 需要共享同一份 context 与在线登记表；
 *   2. 同一钩子只跑一次，避免把函数同时挂在顶层配置和 extensions 里造成重复执行。
 *
 * 鉴权结果通过 context 传给后续钩子：
 *   context.userId / role / canWrite / groupId / docId
 *
 * 失败语义：onAuthenticate 抛出带 reason 的错误时，Hocuspocus 会向客户端
 * 回 PermissionDenied 并断开该文档连接——这正是「连接被拒」。
 */

/** 在线连接登记表：docId -> Map<socketId, {userId, role, readOnly, since}> */
const presence = new Map()

function trackJoin(docId, socketId, info) {
  let doc = presence.get(docId)
  if (!doc) {
    doc = new Map()
    presence.set(docId, doc)
  }
  doc.set(socketId, { ...info, since: Date.now() })
  return doc.size
}

function trackLeave(docId, socketId) {
  const doc = presence.get(docId)
  if (!doc) return 0
  doc.delete(socketId)
  if (doc.size === 0) presence.delete(docId)
  return doc.size
}

/** 当前在线人数（文档级）。 */
export function onlineCount(docId) {
  return presence.get(docId)?.size ?? 0
}

/** 当前在线用户快照，用于调试与可选的 HTTP 状态接口。 */
export function onlineUsers(docId) {
  return [...(presence.get(docId)?.values() ?? [])].map(({ userId, role, readOnly, since }) => ({
    userId,
    role,
    readOnly,
    since,
  }))
}

/**
 * 建连鉴权：JWT + 文档级权限。
 * viewer 允许连接，但以只读方式接入（Hocuspocus 原生支持 readOnly 连接，
 * 会在协议层丢弃该连接发来的 sync/update 消息）。
 */
async function onAuthenticate({ token, documentName, requestParameters, requestHeaders, connectionConfig, socketId }) {
  const rawToken = extractToken({ token, requestParameters, requestHeaders })
  const { userId } = await verifyJWT(rawToken)
  const access = await checkDocumentAccess(userId, documentName)

  connectionConfig.readOnly = !access.canWrite

  const context = {
    userId,
    docId: documentName,
    role: access.role,
    canWrite: access.canWrite,
    groupId: access.groupId,
  }

  const online = trackJoin(documentName, socketId, {
    userId,
    role: access.role,
    readOnly: connectionConfig.readOnly,
  })

  logger.info(
    `鉴权通过 user=${userId} doc=${documentName} role=${access.role} readOnly=${connectionConfig.readOnly} 在线=${online}`,
  )
  return context
}

/** 从快照 + 增量重建 Y.Doc。返回的 Y.Doc 会被 Hocuspocus 合并进内部文档。 */
async function onLoadDocument({ documentName }) {
  try {
    return await loadDocument(documentName)
  } catch (err) {
    // 加载失败必须抛错：Hocuspocus 会关闭该文档的连接。否则用户会看到
    // 一份「空文档」并开始编辑，反而覆盖掉真实内容。
    logger.error(`加载文档失败 doc=${documentName}`, err)
    throw new AuthError('文档加载失败，请稍后重试', 'load-failed')
  }
}

/** debounce 后落全量快照。抛错时 Hocuspocus 会把文档留在内存并重试，不会丢数据。 */
async function onStoreDocument({ documentName, document, lastContext }) {
  // 回滚期间不落快照：内存里的 Y.Doc 仍是回滚前的内容，
  // 写下去会把刚回滚的记录覆盖回去。见 reload.js 的说明。
  if (isStoreSuppressed(documentName)) {
    // 不清除标记：回滚窗口内可能触发多次存储，由 reload.js 在卸载完成后统一清除。
    logger.info(`跳过快照（回滚中）doc=${documentName}`)
    return
  }

  // 先把待合并的增量刷盘，再写快照：快照读取的 watermark 才能覆盖它们。
  await updateJournal.flush(documentName).catch((err) => {
    logger.warn(`快照前刷新增量失败 doc=${documentName}: ${err.message}`)
  })

  const result = await storeDocument(documentName, document)
  logger.debug(
    `已持久化 doc=${documentName} v${result.version} ${result.sizeBytes}B by=${lastContext?.userId ?? 'unknown'}`,
  )
}

/** 断开连接：清理在线登记；最后一个连接离开时立刻把增量刷盘。 */
async function onDisconnect({ documentName, context, socketId, clientsCount }) {
  const remaining = trackLeave(documentName, socketId)
  logger.info(
    `连接断开 user=${context?.userId ?? 'unknown'} doc=${documentName} 剩余=${remaining} clientsCount=${clientsCount}`,
  )
  if (remaining === 0) {
    await updateJournal.flush(documentName).catch((err) => {
      logger.warn(`断开时刷新增量失败 doc=${documentName}: ${err.message}`)
    })
  }
}

/**
 * 增量日志：记录每条 Y update。
 * 只有已鉴权连接产生的编辑才会进入日志；服务端内部事务（source: 'local'）
 * 同样记录，便于恢复，但 readOnly 连接在协议层就不会产生 update。
 */
async function onChange({ documentName, update, context }) {
  if (!context?.userId) return
  updateJournal.push(documentName, update)
}

/** 供 index.js 注册的 extension 对象。 */
export const hocuspocusHooks = {
  extensionName: 'thesis-collab',
  onAuthenticate,
  onLoadDocument,
  onStoreDocument,
  onChange,
  onDisconnect,
}

/** 导出单个钩子便于单元测试直接调用。 */
export const hooks = { onAuthenticate, onLoadDocument, onStoreDocument, onChange, onDisconnect }

export { AuthError }
