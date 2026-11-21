import { config } from './config.js'

/**
 * 极简日志：不引入 pino 之类的依赖，输出一行一条，便于容器日志采集。
 * 级别由 LOG_LEVEL 控制（debug/info/warn/error）。
 */

const LEVELS = { debug: 10, info: 20, warn: 30, error: 40 }

const threshold = LEVELS[config.logLevel] ?? LEVELS.info

function write(level, message, extra) {
  if ((LEVELS[level] ?? LEVELS.info) < threshold) return
  const line = `${new Date().toISOString()} [${level.toUpperCase()}] ${message}`
  const sink = level === 'error' ? console.error : level === 'warn' ? console.warn : console.log
  if (extra === undefined) {
    sink(line)
  } else if (extra instanceof Error) {
    sink(line, extra.stack ?? extra.message)
  } else {
    sink(line, extra)
  }
}

export const logger = {
  debug: (message, extra) => write('debug', message, extra),
  info: (message, extra) => write('info', message, extra),
  warn: (message, extra) => write('warn', message, extra),
  error: (message, extra) => write('error', message, extra),
}
