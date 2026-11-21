import { logger } from './logger.js'
import { clearUpdates } from './persistence.js'

/**
 * 文档强制重载（快照回滚的配套能力）。
 *
 * 背景：正文是 Yjs CRDT，活在协同服务内存里。Go 侧回滚快照只能改数据库与
 * 对象存储，改不动已经加载进内存的 Y.Doc —— 用户会继续看到旧内容，
 * 直到所有连接断开、文档被卸载为止。
 *
 * 这里的做法：
 *   1. closeConnections(documentName)：以 ResetConnection 关闭全部连接。
 *      这是「可恢复的断开」，Hocuspocus 客户端会按自己的重试策略重连，
 *      不需要用户刷新页面。
 *   2. 等待连接计数归零（unloadDocument 要求 getConnectionsCount() === 0）。
 *   3. unloadDocument：触发 afterUnloadDocument，把内存里的 Y.Doc 释放。
 *      卸载前会先跑 onStoreDocument，保证未落盘的编辑不丢。
 *   4. 客户端重连时走 onLoadDocument，从对象存储的最新快照重建文档——
 *      而 Go 侧回滚时写入的新快照记录正指向旧版本的 storage_key。
 *
 * 关键约束：closeConnections 之后必须等连接真正断开才能卸载。
 * 这里用轮询而非固定 sleep：固定 sleep 在慢机器上会过早卸载失败，
 * 在快机器上又白等。
 */

/** 等待连接归零的最长时间；超过则放弃（回滚的数据库记录仍然有效，只是要等自然卸载）。 */
const DRAIN_TIMEOUT_MS = 5000
const DRAIN_POLL_MS = 50

/** 广播给客户端的回滚信号。客户端收到后丢弃本地 Y.Doc 并重新同步。 */
export const ROLLBACK_SIGNAL = 'thesis:rollback'

/** 发信号后给客户端自行断开留出的时间。 */
const ROLLBACK_SIGNAL_GRACE_MS = 300

/**
 * 正在回滚的文档集合。
 *
 * onStoreDocument 会检查它：回滚期间不落快照，否则内存中的旧内容会把
 * 刚回滚的记录覆盖回去。用 Set 而不是单个变量，允许不同文档同时回滚。
 */
const suppressStore = new Set()

function suppressNextStore(documentName) {
  suppressStore.add(documentName)
}

/** 供 hooks.onStoreDocument 调用：该文档是否正处于回滚中。 */
export function isStoreSuppressed(documentName) {
  return suppressStore.has(documentName)
}

/**
 * 回滚流程结束后清除标记。
 *
 * 注意：不在 onStoreDocument 里清除。unloadDocument 可能触发多次存储
 * （debounce 中的那次 + 卸载前的那次），只拦一次会让后一次把旧内容写回去。
 * 由 forceReloadDocument 在卸载完成后统一清除。
 */
export function clearStoreSuppressed(documentName) {
  suppressStore.delete(documentName)
}

/**
 * 强制重载一个文档。
 *
 * @param {import('@hocuspocus/server').Server} server
 * @param {string} documentName 文档 UUID
 * @returns {Promise<{reloaded: boolean, reason?: string, connections?: number}>}
 */
export async function forceReloadDocument(server, documentName) {
  const instance = server?.hocuspocus
  if (!instance) {
    return { reloaded: false, reason: 'server_not_ready' }
  }

  const document = instance.documents.get(documentName)
  if (!document) {
    // 文档当前没有被加载：下一个连接会自然从最新快照重建，无需任何操作。
    // 这是最常见的情况，也是最理想的情况。
    return { reloaded: true, reason: 'not_loaded' }
  }

  const before = document.getConnectionsCount()
  logger.info(`强制重载 doc=${documentName} 连接=${before}`)

  // 0) 立刻设置「不落快照」标记，且一直保持到卸载完成。
  //
  // 顺序很关键：debounce 队列里可能已经有待执行的 onStoreDocument，
  // 它会带着内存中的旧内容把快照写回去。标记必须在任何断开动作之前
  // 就位，并在整个回滚窗口内保持有效。
  suppressNextStore(documentName)

  // 1) 先清掉增量日志，**在断开之前**。
  //
  // 顺序是这条链路里最容易踩错的地方：客户端收到信号后会立刻重连，
  // 重连走 loadDocument —— 它会读快照再按 seq 回放全部增量。
  // 如果清增量发生在断开之后，重连已经完成，旧增量被重新应用，
  // 回滚就白做了。所以必须先清增量，再让任何人重连。
  const cleared = await clearUpdates(documentName)

  // 2) 广播「已回滚」信号。
  //
  // 为什么必须先发信号再断开：Yjs 是 CRDT，客户端本地 Y.Doc 里保留着
  // 自己的编辑历史。如果只是断开重连，Yjs 会把服务端快照与本地状态
  // **合并**，本地编辑依然胜出——用户看到的还是回滚前的内容。
  // 只有客户端主动丢弃本地状态、从空 Y.Doc 重新同步，回滚才真正生效。
  document.broadcastStateless(ROLLBACK_SIGNAL)

  // 给客户端一点时间收到信号并自行断开（避免它把「服务端主动断开」
  // 误判为网络故障而带着旧状态重连）。
  await new Promise((resolve) => setTimeout(resolve, ROLLBACK_SIGNAL_GRACE_MS))

  // 3) 以可恢复的错误码断开剩余连接，客户端会自动重连。
  instance.closeConnections(documentName)

  // 4) 等连接归零。unloadDocument 内部也会检查，但提前等待能让我们
  //    在超时时给出准确原因，而不是静默地「什么也没发生」。
  const deadline = Date.now() + DRAIN_TIMEOUT_MS
  while (document.getConnectionsCount() > 0 && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, DRAIN_POLL_MS))
  }

  const remaining = document.getConnectionsCount()
  if (remaining > 0) {
    logger.warn(`强制重载失败 doc=${documentName} 仍有 ${remaining} 个连接未断开`)
    return { reloaded: false, reason: 'connections_draining', connections: remaining }
  }

  // 5) 丢弃内存里的 Y.Doc，**不写回快照**。
  //
  // 这一步是整条链路的关键：unloadDocument 会触发 onStoreDocument，
  // 把内存中（仍是回滚前内容）的 Y.Doc 存成新快照，把刚回滚的记录覆盖掉。
  // 因此先摘掉文档再卸载，让卸载路径拿不到可存的文档。
  //
  // 代价：卸载前尚未落盘的编辑会丢失。这是回滚的语义所决定的——
  // 用户明确要求丢弃当前内容、回到某个历史版本。
  try {
    await instance.unloadDocument(document)
  } finally {
    // 卸载完成（成功或失败）后才允许再次落快照，
    // 保证回滚窗口内的所有 onStoreDocument 都被拦住。
    clearStoreSuppressed(documentName)
  }

  const stillLoaded = instance.documents.has(documentName)
  logger.info(
    `强制重载完成 doc=${documentName} 已卸载=${!stillLoaded} 清理增量=${cleared} 条`,
  )
  return {
    reloaded: !stillLoaded,
    reason: stillLoaded ? 'unload_skipped' : 'reloaded',
    connections: before,
    clearedUpdates: cleared,
  }
}
