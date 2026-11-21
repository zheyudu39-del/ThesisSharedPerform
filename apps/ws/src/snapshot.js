import * as Y from 'yjs'

import { logger } from './logger.js'
import { loadDocument, storeDocumentAtVersion, updateJournal } from './persistence.js'

/**
 * 立即为文档写一份快照。
 *
 * 为什么需要这个端点：REST 侧的「创建快照」只写元数据行，二进制由协同服务
 * 负责落盘。但 REST 进程拿不到内存里的 Y.Doc —— 只有协同服务知道当前正文。
 * 早期实现直接写了一条 size_bytes=0、storage_key 指向不存在对象的记录，
 * 结果「回滚」回去的是空文档。
 *
 * 分工：版本号由 REST 侧分配（它在事务里 BumpDocumentSnapshot 并插入元数据行），
 * 这里只负责把二进制写到那个版本对应的 storage_key。
 *
 * 保证：
 *   1. 文档在内存中时，用它当前的状态写（最准确）；
 *   2. 文档不在内存时，先从快照 + 增量重建，再写（等价于「当前内容」）；
 *   3. 返回真实的 sizeBytes，供 REST 侧更新元数据。
 */
export async function snapshotNow(server, documentName, version) {
  if (!Number.isInteger(version) || version <= 0) {
    return { ok: false, reason: 'bad_version' }
  }

  const instance = server?.hocuspocus
  if (!instance) {
    return { ok: false, reason: 'server_not_ready' }
  }

  // 文档在内存里：直接用它的当前状态。
  const loaded = instance.documents.get(documentName)
  if (loaded) {
    // 先把待合并的增量刷盘，快照的 watermark 才能覆盖它们。
    await updateJournal.flush(documentName).catch((err) => {
      logger.warn(`快照前刷新增量失败 doc=${documentName}: ${err.message}`)
    })
    // Hocuspocus 的 Document 类继承自 Y.Doc，直接传它即可。
    const result = await storeDocumentAtVersion(documentName, loaded, version)
    logger.info(`按内存状态写快照 doc=${documentName} v${result.version} ${result.sizeBytes}B`)
    return { ok: true, version: result.version, sizeBytes: result.sizeBytes }
  }

  // 文档不在内存里：重建一份，保证快照反映的是「当前应有的内容」。
  // 这是常见情况——没人正在编辑时点「创建快照」。
  const rebuilt = await loadDocument(documentName)
  if (!(rebuilt instanceof Y.Doc)) {
    return { ok: false, reason: 'load_failed' }
  }
  try {
    const result = await storeDocumentAtVersion(documentName, rebuilt, version)
    logger.info(`按重建状态写快照 doc=${documentName} v${result.version} ${result.sizeBytes}B`)
    return { ok: true, version: result.version, sizeBytes: result.sizeBytes }
  } finally {
    rebuilt.destroy()
  }
}
