import http from 'node:http'
import { fileURLToPath } from 'node:url'
import { realpathSync } from 'node:fs'

import { Server } from '@hocuspocus/server'

import { config, validateConfig } from './config.js'
import { logger } from './logger.js'
import { hocuspocusHooks } from './hooks.js'
import { closeAuth, pingDatabase } from './auth.js'
import { closePersistence } from './persistence.js'

/**
 * 协同服务入口。
 *
 * 架构位置：Go API (:8080) 负责文档 CRUD / 权限 / 快照元数据；本进程 (:1234)
 * 只负责 Yjs 实时同步，浏览器带 JWT 直连本进程的 WebSocket。
 *
 * 注意：@hocuspocus/server 4.x 的入口是 `Server` 类，`new Server({...})`
 * 才有 listen()；旧文档里的 `new Hocuspocus()` 没有监听能力。
 */

/**
 * 创建（但不启动）协同服务器。拆出来是为了测试可以拿到实例，
 * 也为了让 `import('./src/index.js')` 不产生副作用。
 */
export function createServer() {
  const server = new Server({
    port: config.port,
    address: config.address,

    // 由本文件自己处理 SIGINT/SIGTERM：Hocuspocus 内置的处理器会直接
    // process.exit(0)，来不及把待写快照与增量刷盘。
    stopOnSignals: false,

    // 关闭启动横幅，日志统一走 logger。
    quiet: true,

    // 落快照节奏：最后一次编辑 2s 后写，持续编辑最多等 10s。
    debounce: config.debounce,
    maxDebounce: config.maxDebounce,

    // 文档无人连接时立刻卸载：卸载前 Hocuspocus 会先执行 onStoreDocument，
    // 所以不会丢数据；及时卸载能释放内存中的 Y.Doc。
    unloadImmediately: true,

    // 所有钩子集中在一个 extension 里，避免同一钩子被执行两次。
    extensions: [hocuspocusHooks],
  })

  return server
}

/** 独立的健康检查 HTTP 端口（与 WebSocket 共用 1234 会与 Hocuspocus 的请求处理冲突）。 */
function startHealthServer(port) {
  const health = http.createServer(async (req, res) => {
    if (req.method !== 'GET' || !['/healthz', '/readyz'].includes(req.url)) {
      res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' })
      res.end('not found')
      return
    }

    let db = false
    try {
      db = await pingDatabase()
    } catch (err) {
      logger.warn(`健康检查：数据库不可用 ${err.message}`)
    }

    const ok = db
    res.writeHead(ok ? 200 : 503, { 'Content-Type': 'application/json; charset=utf-8' })
    res.end(
      JSON.stringify({
        status: ok ? 'ok' : 'degraded',
        database: db,
        documents: 0,
        uptimeSeconds: Math.round(process.uptime()),
      }),
    )
  })

  health.on('error', (err) => {
    logger.error(`健康检查端口 ${port} 启动失败`, err)
  })
  health.listen(port, config.address, () => {
    logger.info(`健康检查监听 http://${config.address}:${port}/healthz`)
  })
  return health
}

/**
 * 启动服务。返回 { server, health, shutdown } 便于测试与优雅退出。
 */
export async function start() {
  validateConfig(config)

  const server = createServer()
  await server.listen(config.port)

  logger.info(`协同服务已启动 ws://${config.address}:${config.port}（文档名 = 文档 UUID）`)

  let health = null
  if (config.healthPort > 0) {
    health = startHealthServer(config.healthPort)
  }

  let shuttingDown = false
  const shutdown = async (signal) => {
    if (shuttingDown) return
    shuttingDown = true
    logger.info(`收到 ${signal}，正在关闭协同服务…`)
    try {
      // destroy() 会关闭连接并 flushPendingStores，把 debounce 中的快照写完。
      await server.destroy()
      await closePersistence()
      await closeAuth()
      if (health) await new Promise((resolve) => health.close(resolve))
      logger.info('协同服务已关闭')
    } catch (err) {
      logger.error('关闭过程中出错', err)
    }
  }

  return { server, health, shutdown }
}

/** 判断当前文件是否被 node 直接执行（而不是被 import）。 */
function isMainModule() {
  const entry = process.argv[1]
  if (!entry) return false
  try {
    return realpathSync(fileURLToPath(import.meta.url)) === realpathSync(entry)
  } catch {
    return false
  }
}

if (isMainModule()) {
  const { shutdown } = await start()

  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.on(signal, () => {
      void shutdown(signal).then(() => process.exit(0))
    })
  }

  process.on('unhandledRejection', (reason) => {
    logger.error('未处理的 Promise 拒绝', reason instanceof Error ? reason : new Error(String(reason)))
  })
}
