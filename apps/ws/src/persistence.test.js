import assert from 'node:assert/strict'
import { test } from 'node:test'
import { MAX_SNAPSHOTS_PER_DOC, selectPrunableSnapshots } from './persistence.js'

// 构造 n 个快照；labeled 里的版本带 label。
function makeSnapshots(n, labeled = []) {
  return Array.from({ length: n }, (_, i) => {
    const version = i + 1
    return { version, label: labeled.includes(version) ? `milestone-${version}` : null }
  })
}

test('未超上限时不淘汰任何快照', () => {
  const snapshots = makeSnapshots(MAX_SNAPSHOTS_PER_DOC - 1)
  const pruned = selectPrunableSnapshots(snapshots, MAX_SNAPSHOTS_PER_DOC - 1)
  assert.deepEqual(pruned, [])
})

test('恰好达到上限时不淘汰', () => {
  const snapshots = makeSnapshots(MAX_SNAPSHOTS_PER_DOC)
  assert.deepEqual(selectPrunableSnapshots(snapshots, MAX_SNAPSHOTS_PER_DOC), [])
})

test('超出上限时从最旧的无标签快照开始淘汰', () => {
  const snapshots = makeSnapshots(MAX_SNAPSHOTS_PER_DOC + 5)
  const pruned = selectPrunableSnapshots(snapshots, MAX_SNAPSHOTS_PER_DOC + 5)
  assert.equal(pruned.length, 5)
  // 最旧的 5 个版本被淘汰，且按升序返回
  assert.deepEqual(pruned, [1, 2, 3, 4, 5])
})

test('带标签的里程碑永不淘汰', () => {
  // 已命名的不占用「自动快照」额度（PRD 原文是「最近 50 个自动快照」），
  // 因此保留 50 个无标签 + 2 个里程碑 = 52 条，但淘汰 3 个最旧的无标签项。
  const snapshots = makeSnapshots(MAX_SNAPSHOTS_PER_DOC + 5, [1, 2])
  const pruned = selectPrunableSnapshots(snapshots, MAX_SNAPSHOTS_PER_DOC + 5)
  assert.ok(!pruned.includes(1), '里程碑 1 不应被淘汰')
  assert.ok(!pruned.includes(2), '里程碑 2 不应被淘汰')

  // 淘汰后剩下的无标签快照应恰好为上限
  const remaining = snapshots.filter(
    (s) => s.label == null && !pruned.includes(s.version),
  )
  // 当前版本（55）本身是最后一个无标签项，包含在这 50 个里
  assert.equal(remaining.length, MAX_SNAPSHOTS_PER_DOC)
  // 被淘汰的应当是最旧的几个无标签版本
  assert.deepEqual(pruned, [3, 4, 5])
})

test('当前版本永远保留', () => {
  // 当前版本是最旧的（例如回滚后重新写入），也不能被删
  const snapshots = makeSnapshots(MAX_SNAPSHOTS_PER_DOC + 3)
  const pruned = selectPrunableSnapshots(snapshots, 1)
  assert.ok(!pruned.includes(1), '当前版本不应出现在淘汰列表里')
  assert.equal(pruned.length, 3)
})

test('全部带标签时不会淘汰任何东西', () => {
  const all = Array.from({ length: MAX_SNAPSHOTS_PER_DOC + 10 }, (_, i) => i + 1)
  const snapshots = makeSnapshots(MAX_SNAPSHOTS_PER_DOC + 10, all)
  assert.deepEqual(selectPrunableSnapshots(snapshots, MAX_SNAPSHOTS_PER_DOC + 10), [])
})

test('上限为 1 时只保留当前版本', () => {
  const snapshots = makeSnapshots(4)
  const pruned = selectPrunableSnapshots(snapshots, 4, 1)
  assert.deepEqual(pruned, [1, 2, 3])
})
