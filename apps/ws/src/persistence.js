import * as Y from 'yjs'
import pg from 'pg'
import { Client as MinioClient } from 'minio'

import { config } from './config.js'
import { logger } from './logger.js'

/**
 * Y.Doc 持久化。
 *
 * 存储分层（见 docs/technical-architecture.md §4.4 / §5.1）：
 *   - 快照：doc-snapshot/{docId}/{version}.bin —— Y.encodeStateAsUpdate 全量状态；
 *   - 增量：doc-updates/{docId}/{seq}.bin   —— 两次快照之间的 Y update。
 *   - PG 只存指针与版本号：doc_snapshots / doc_updates / documents.latest_*。
 *
 * 恢复顺序：最新快照 → 按 seq 顺序回放其后仍未合并的增量。
 * 写快照时在同一事务里提升 documents.latest_snapshot，并把已被快照覆盖的
 * 增量记录删掉（对象存储中的对应文件随后异步清理）。
 *
 * 容错约定：对象存储不可用不能阻塞编辑。增量写入是 best-effort（失败只记日志，
 * 数据仍在内存与 Hocuspocus 的 debounce 快照里）；快照写入失败时抛错，
 * Hocuspocus 会保留文档在内存中，不会丢数据。
 */

const { Pool } = pg

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/** 与 Go 端 internal/storage 相同的 key 规范。 */
export const snapshotKey = (docId, version) => `doc-snapshot/${docId}/${version}.bin`
export const updateKey = (docId, seq) => `doc-updates/${docId}/${seq}.bin`

let pool = null
let minio = null
let bucketReady = null

function getPool() {
  if (!pool) {
    pool = new Pool({
      connectionString: config.postgresDsn,
      max: config.pgPoolMax,
      connectionTimeoutMillis: 5000,
      idleTimeoutMillis: 30_000,
    })
    pool.on('error', (err) => logger.error('PG 连接池错误', err))
  }
  return pool
}

/** 惰性创建 minio 客户端，并确保桶存在（SeaweedFS 上通常已由 Go 端创建）。 */
function getMinio() {
  if (!minio) {
    minio = new MinioClient({
      endPoint: config.s3.endpoint.split(':')[0],
      port: Number.parseInt(config.s3.endpoint.split(':')[1] ?? '9000', 10),
      useSSL: config.s3.useSSL,
      accessKey: config.s3.accessKey,
      secretKey: config.s3.secretKey,
      // SeaweedFS 不提供虚拟主机风格的桶域名，必须 path-style。
      pathStyle: true,
      region: 'us-east-1',
    })
  }
  return minio
}

async function ensureBucket() {
  if (!bucketReady) {
    bucketReady = (async () => {
      const client = getMinio()
      const exists = await client.bucketExists(config.s3.bucket).catch((err) => {
        logger.warn(`检查存储桶失败: ${err.message}`)
        return false
      })
      if (!exists) {
        await client.makeBucket(config.s3.bucket).catch((err) => {
          logger.warn(`创建存储桶 ${config.s3.bucket} 失败: ${err.message}`)
        })
      }
    })()
  }
  return bucketReady
}

async function putObject(key, buffer) {
  await ensureBucket()
  await getMinio().putObject(config.s3.bucket, key, buffer, buffer.length, {
    'Content-Type': 'application/octet-stream',
  })
}

async function getObject(key) {
  const stream = await getMinio().getObject(config.s3.bucket, key)
  const chunks = []
  for await (const chunk of stream) chunks.push(chunk)
  return Buffer.concat(chunks)
}

async function removeObject(key) {
  try {
    await getMinio().removeObject(config.s3.bucket, key)
  } catch (err) {
    logger.warn(`清理对象 ${key} 失败: ${err.message}`)
  }
}

function isNotFound(err) {
  return err?.code === 'NoSuchKey' || err?.code === 'NotFound' || err?.statusCode === 404
}

/**
 * 加载文档：最新快照 + 其后未合并的增量。
 * 没有任何历史时返回空 Y.Doc（Hocuspocus 会直接使用它）。
 */
export async function loadDocument(docId) {
  if (!UUID_RE.test(String(docId ?? ''))) {
    throw new Error(`文档标识无效: ${docId}`)
  }

  const doc = new Y.Doc()
  const db = getPool()

  const { rows: docRows } = await db.query(
    `SELECT latest_snapshot, latest_update_seq FROM documents WHERE id = $1 AND deleted_at IS NULL`,
    [docId],
  )
  const meta = docRows[0]
  if (!meta) {
    throw new Error(`文档不存在: ${docId}`)
  }

  let snapshotVersion = 0
  if (meta.latest_snapshot > 0) {
    const { rows } = await db.query(
      `SELECT version, storage_key FROM doc_snapshots WHERE doc_id = $1 AND version = $2`,
      [docId, meta.latest_snapshot],
    )
    const snapshot = rows[0]
    if (snapshot) {
      try {
        const buf = await getObject(snapshot.storage_key)
        Y.applyUpdate(doc, new Uint8Array(buf), 'load-snapshot')
        snapshotVersion = snapshot.version
      } catch (err) {
        // 快照丢失不应让文档打不开：退化为从增量回放（可能为空文档）。
        logger.error(
          `读取快照失败 doc=${docId} version=${snapshot.version}: ${err.message}；将尝试仅回放增量`,
        )
      }
    } else {
      logger.warn(`documents.latest_snapshot=${meta.latest_snapshot} 但缺少 doc_snapshots 记录 doc=${docId}`)
    }
  }

  // 增量：seq 单调递增；已被快照覆盖的增量在写快照时删除，这里按 seq 全量回放。
  const { rows: updates } = await db.query(
    `SELECT seq, storage_key FROM doc_updates WHERE doc_id = $1 ORDER BY seq ASC`,
    [docId],
  )

  let applied = 0
  for (const update of updates) {
    try {
      const buf = await getObject(update.storage_key)
      Y.applyUpdate(doc, new Uint8Array(buf), 'load-update')
      applied += 1
    } catch (err) {
      if (isNotFound(err)) {
        logger.warn(`增量对象缺失 doc=${docId} seq=${update.seq} key=${update.storage_key}`)
        continue
      }
      throw err
    }
  }

  logger.info(
    `已加载文档 doc=${docId} snapshot=v${snapshotVersion} updates=${applied}/${updates.length}`,
  )
  return doc
}

/**
 * 写全量快照。
 *
 * 版本号 = documents.latest_snapshot + 1，与快照记录、最新版本指针在同一事务
 * 内更新；行锁（FOR UPDATE）保证多实例并发写同一文档时版本号不重复。
 *
 * @returns {{version:number, key:string, sizeBytes:number}}
 */
export async function storeDocument(docId, ydoc) {
  if (!UUID_RE.test(String(docId ?? ''))) {
    throw new Error(`文档标识无效: ${docId}`)
  }
  if (!(ydoc instanceof Y.Doc)) {
    throw new Error('storeDocument 需要 Y.Doc 实例')
  }

  // 水位线必须在 encode 之前读取：seq ≤ watermark 的增量一定已经应用进文档，
  // 因此 encodeStateAsUpdate 的产物必然覆盖它们，快照提交后可以安全删除。
  // 反过来（先 encode 再读 seq）会把 encode 之后才落库的增量误删，造成丢数据。
  const { rows: watermarkRows } = await getPool().query(
    `SELECT latest_update_seq FROM documents WHERE id = $1 AND deleted_at IS NULL`,
    [docId],
  )
  if (!watermarkRows[0]) {
    throw new Error(`文档不存在或已删除: ${docId}`)
  }
  const watermark = Number(watermarkRows[0].latest_update_seq)

  const state = Y.encodeStateAsUpdate(ydoc)
  const buffer = Buffer.from(state)

  const client = await getPool().connect()
  try {
    await client.query('BEGIN')

    const { rows } = await client.query(
      `SELECT latest_snapshot
         FROM documents
        WHERE id = $1 AND deleted_at IS NULL
        FOR UPDATE`,
      [docId],
    )
    const meta = rows[0]
    if (!meta) {
      throw new Error(`文档不存在或已删除: ${docId}`)
    }

    const version = meta.latest_snapshot + 1
    const key = snapshotKey(docId, version)

    // 先落对象再落元数据：事务回滚最多留下一个孤儿对象，不会出现指向空对象的记录。
    await putObject(key, buffer)

    await client.query(
      `INSERT INTO doc_snapshots (doc_id, version, storage_key, size_bytes, label)
       VALUES ($1, $2, $3, $4, NULL)`,
      [docId, version, key, buffer.length],
    )
    await client.query(
      `UPDATE documents SET latest_snapshot = $2, updated_at = now() WHERE id = $1`,
      [docId, version],
    )

    // 只删除 encode 之前就已落库的增量（seq ≤ watermark）。快照之后到达的
    // 增量保留，下次加载时回放——重复应用同一 update 在 Yjs 中是幂等的。
    const { rows: stale } = await client.query(
      `DELETE FROM doc_updates WHERE doc_id = $1 AND seq <= $2 RETURNING storage_key`,
      [docId, watermark],
    )

    // 只保留最近 N 个快照（PRD D-04）。不清理的话每个 debounce 周期都会
    // 新增一份全量状态，中长期会把对象存储吃满。
    //
    // 命名里程碑（label 非空）永不删除：用户主动起的名字是他们的锚点，
    // 被自动清理掉比占用空间更糟。超出上限时优先淘汰无标签的旧快照。
    const pruned = await pruneSnapshots(client, docId, version)

    await client.query('COMMIT')

    for (const row of stale) {
      void removeObject(row.storage_key)
    }
    for (const key of pruned) {
      void removeObject(key)
    }

    logger.info(
      `快照已保存 doc=${docId} v${version} ${buffer.length}B（清理增量 ${stale.length} 条）`,
    )
    return { version, key, sizeBytes: buffer.length }
  } catch (err) {
    await client.query('ROLLBACK').catch(() => {})
    logger.error(`保存快照失败 doc=${docId}`, err)
    throw err
  } finally {
    client.release()
  }
}

/** 单个文档保留的快照上限（PRD D-04「保留最近 50 个自动快照」）。 */
export const MAX_SNAPSHOTS_PER_DOC = 50

/**
 * 决定哪些快照该被淘汰（纯函数，便于单测）。
 *
 * @param {Array<{version:number,label:string|null}>} snapshots 该文档的全部快照
 * @param {number} currentVersion 刚写入的版本，永远保留
 * @param {number} max 上限
 * @returns {number[]} 应删除的版本号
 *
 * 规则：
 *   - 带 label 的是用户亲手命名的里程碑，永不删除；
 *   - 无标签的按 version 从旧到新淘汰，直到无标签总数回到上限；
 *   - 当前版本永远保留。
 *
 * 有标签的快照不计入上限——它们是用户显式创建的锚点，
 * 被自动清理掉比多占几份空间更糟。
 */
export function selectPrunableSnapshots(snapshots, currentVersion, max = MAX_SNAPSHOTS_PER_DOC) {
  if (max <= 0) return []
  const unlabeled = snapshots
    .filter((s) => s.label == null && s.version !== currentVersion)
    .sort((a, b) => a.version - b.version)
  const keep = Math.max(0, max - 1) // 留一个名额给当前版本
  if (unlabeled.length <= keep) return []
  return unlabeled.slice(0, unlabeled.length - keep).map((s) => s.version)
}

/**
 * 裁剪超出上限的旧快照，返回被删除对象的 storage_key 列表。
 *
 * 规则：
 *   - 带 label 的是用户亲手命名的里程碑，永不删除；
 *   - 无标签的按 version 从旧到新淘汰，直到总数回到上限；
 *   - 当前刚写入的版本永远保留（哪怕它本身超出上限，也只能等下次再裁）。
 *
 * 返回的 key 由调用方在 COMMIT 之后删对象——先删对象再回滚事务会留下
 * 「元数据还在但对象没了」的坏记录。
 */
async function pruneSnapshots(client, docId, currentVersion) {
  // 读全部快照的版本与标签，交给纯函数决定淘汰哪些。
  //
  // 为什么不在 SQL 里用 OFFSET 直接删：淘汰规则要区分「有标签/无标签」
  // 并永远保留当前版本，写成 SQL 后难以验证；而单文档的快照数有上限
  // （几十条），全量读进内存再决策的代价可以忽略。
  const { rows } = await client.query(
    `SELECT version, storage_key, label FROM doc_snapshots WHERE doc_id = $1`,
    [docId],
  )
  const doomed = new Set(selectPrunableSnapshots(rows, currentVersion))
  if (doomed.size === 0) return []

  const keys = rows.filter((r) => doomed.has(r.version)).map((r) => r.storage_key)
  await client.query(
    `DELETE FROM doc_snapshots WHERE doc_id = $1 AND version = ANY($2::int[])`,
    [docId, [...doomed]],
  )
  return keys
}

/**
 * 删除文档的全部增量日志（回滚时用）。
 *
 * 回滚的语义是「丢弃当前内容、回到某个历史版本」，因此快照之后累积的
 * 增量必须一并清掉：loadDocument 会按 seq 回放它们，不清就会把回滚前的
 * 编辑重新拼回去。
 *
 * 先删元数据行再删对象：即使删对象失败，也只是留下孤儿对象，
 * 不会出现「元数据指向已删除对象」的加载错误。
 *
 * @returns {Promise<number>} 清理的增量条数
 */
export async function clearUpdates(docId) {
  if (!UUID_RE.test(String(docId ?? ''))) {
    throw new Error(`文档标识无效: ${docId}`)
  }
  const { rows } = await getPool().query(
    `DELETE FROM doc_updates WHERE doc_id = $1 RETURNING storage_key`,
    [docId],
  )
  for (const row of rows) {
    void removeObject(row.storage_key)
  }
  // 水位线归零：新编辑的 seq 从 1 重新开始，避免与已删除的序号混淆。
  await getPool().query(`UPDATE documents SET latest_update_seq = 0 WHERE id = $1`, [docId])
  return rows.length
}

/**
 * 把 Y.Doc 写成**指定版本**的快照。
 *
 * 与 storeDocument 的区别：storeDocument 自己分配版本号（协同服务自动落盘
 * 时用），而这里由调用方（REST 的「创建快照」）已经分配好版本并插入了
 * 元数据行，我们只负责把二进制写到对应的 storage_key。
 *
 * 不重复 BumpDocumentSnapshot：版本号是 REST 在事务里递增的，
 * 这里再 bump 会让两边的版本号错位。
 */
export async function storeDocumentAtVersion(docId, ydoc, version) {
  if (!UUID_RE.test(String(docId ?? ''))) {
    throw new Error(`文档标识无效: ${docId}`)
  }
  if (!(ydoc instanceof Y.Doc)) {
    throw new Error('storeDocumentAtVersion 需要 Y.Doc 实例')
  }
  if (!Number.isInteger(version) || version <= 0) {
    throw new Error(`版本号无效: ${version}`)
  }

  const state = Y.encodeStateAsUpdate(ydoc)
  const buffer = Buffer.from(state)
  const key = snapshotKey(docId, version)

  // 先落对象：REST 那边的元数据行已经存在（它先插入再调用我们），
  // 所以这里失败时必须让调用方知道，由它回滚或标记。
  await putObject(key, buffer)

  // 回填真实的字节数——元数据行插入时还不知道大小。
  await getPool().query(
    `UPDATE doc_snapshots SET storage_key = $3, size_bytes = $4
      WHERE doc_id = $1 AND version = $2`,
    [docId, version, key, buffer.length],
  )

  return { version, key, sizeBytes: buffer.length }
}

/**
 * 追加一条增量（best-effort）。
 *
 * seq 由 documents.latest_update_seq 原子自增获得，key 按规范拼出。
 * 上传失败时删除刚插入的元数据行，避免加载时指向不存在的对象。
 *
 * @returns {{seq:number, key:string}|null} 失败返回 null，调用方不应中断编辑
 */
export async function appendUpdate(docId, update) {
  if (!UUID_RE.test(String(docId ?? ''))) return null
  if (!(update instanceof Uint8Array) || update.length === 0) return null

  const buffer = Buffer.from(update)
  let row
  try {
    const { rows } = await getPool().query(
      `WITH next AS (
         UPDATE documents
            SET latest_update_seq = latest_update_seq + 1,
                updated_at = now()
          WHERE id = $1 AND deleted_at IS NULL
        RETURNING latest_update_seq
       )
       INSERT INTO doc_updates (doc_id, seq, storage_key, size_bytes)
       SELECT $1, next.latest_update_seq,
              'doc-updates/' || $1::text || '/' || next.latest_update_seq::text || '.bin',
              $2
         FROM next
       RETURNING seq, storage_key`,
      [docId, buffer.length],
    )
    row = rows[0]
  } catch (err) {
    logger.warn(`记录增量失败 doc=${docId}: ${err.message}`)
    return null
  }
  if (!row) return null

  try {
    await putObject(row.storage_key, buffer)
    return { seq: Number(row.seq), key: row.storage_key }
  } catch (err) {
    logger.warn(`上传增量失败 doc=${docId} seq=${row.seq}: ${err.message}（编辑不受影响）`)
    await getPool()
      .query(`DELETE FROM doc_updates WHERE doc_id = $1 AND seq = $2`, [docId, row.seq])
      .catch(() => {})
    return null
  }
}

/**
 * 增量日志：把高频 update 在内存里合并后再落盘，避免每次按键都发一次 S3 请求。
 * 定时器到期时用 Y.mergeUpdates 合并成一个 update 追加。
 */
class UpdateJournal {
  constructor(flushMs) {
    this.flushMs = flushMs
    this.entries = new Map() // docId -> { updates: Uint8Array[], timer: NodeJS.Timeout|null }
  }

  push(docId, update) {
    let entry = this.entries.get(docId)
    if (!entry) {
      entry = { updates: [], timer: null }
      this.entries.set(docId, entry)
    }
    entry.updates.push(update)
    if (!entry.timer) {
      entry.timer = setTimeout(() => {
        entry.timer = null
        void this.flush(docId)
      }, this.flushMs)
      entry.timer.unref?.()
    }
  }

  async flush(docId) {
    const entry = this.entries.get(docId)
    if (!entry || entry.updates.length === 0) return
    const updates = entry.updates
    entry.updates = []
    if (entry.timer) {
      clearTimeout(entry.timer)
      entry.timer = null
    }

    const merged = updates.length === 1 ? updates[0] : Y.mergeUpdates(updates)
    await appendUpdate(docId, merged)
  }

  async flushAll() {
    const ids = [...this.entries.keys()]
    await Promise.allSettled(ids.map((id) => this.flush(id)))
  }
}

export const updateJournal = new UpdateJournal(config.updateFlushMs)

/** 关闭连接池，供优雅退出与测试使用。 */
export async function closePersistence() {
  await updateJournal.flushAll()
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
