-- 文档（协同编辑）相关查询。
--
-- 约定：
--   * 文档一律属于某个群组；查看/编辑权限由服务层用 GetDocumentAccess
--     一次性取回「创建者、群主、群内角色、文档级覆盖角色」，再交给
--     permissions.go 的纯函数判定，避免权限逻辑散落在 SQL 里。
--   * 删除是软删除（deleted_at），所有读查询都排除已删除行。
--   * latest_snapshot / word_count / latest_update_seq 由协同服务维护；
--     REST 侧只在创建快照时递增 latest_snapshot，其余字段只读。

-- name: CreateDocument :one
INSERT INTO documents (group_id, title, status, default_role, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetDocumentByID :one
-- 文档视图：附带群组 slug 与创建者摘要，供 DocumentDTO 直接投影。
SELECT d.*, g.slug AS group_slug, u.name AS creator_name, u.handle AS creator_handle
FROM documents d
JOIN groups g ON g.id = d.group_id
JOIN users u ON u.id = d.created_by
WHERE d.id = $1 AND d.deleted_at IS NULL;

-- name: UpdateDocument :one
-- 局部更新：参数为 NULL 表示「本次不修改」。
-- 只允许改标题与状态：default_role 决定群成员的默认文档权限，
-- 经由 PATCH 修改会让 editor 悄悄把群成员降为 viewer，故不开放。
UPDATE documents SET
    title  = COALESCE(sqlc.narg('title')::varchar(256), title),
    status = COALESCE(sqlc.narg('status')::doc_status, status)
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteDocument :execrows
UPDATE documents SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListDocumentsByGroup :many
-- 群内文档列表，按 (updated_at, id) 倒序翻页：最近编辑的排前面，
-- id 用于打破同一时间的并列，保证翻页稳定。
SELECT d.*, g.slug AS group_slug, u.name AS creator_name, u.handle AS creator_handle
FROM documents d
JOIN groups g ON g.id = d.group_id
JOIN users u ON u.id = d.created_by
WHERE d.group_id = sqlc.arg('group_id') AND d.deleted_at IS NULL
  AND (sqlc.narg('before_updated_at')::timestamptz IS NULL
       OR (d.updated_at, d.id) < (sqlc.narg('before_updated_at')::timestamptz, sqlc.arg('before_id')::uuid))
ORDER BY d.updated_at DESC, d.id DESC
LIMIT sqlc.arg('limit');

-- name: GetDocumentAccess :one
-- 一次性取回权限判定所需的全部事实：
--   * 文档是否属于已删除的群组（g.deleted_at 过滤后无行即视为不存在）；
--   * 创建者与群主（两者都隐含 owner）；
--   * 当前用户是否为群成员及群内角色；
--   * 当前用户是否有文档级覆盖角色（doc_members）。
-- 行不存在说明文档不存在/已删除/群已删除。
SELECT d.id AS doc_id, d.group_id, d.default_role, d.created_by,
       g.owner_id AS group_owner_id, g.slug AS group_slug,
       gm.role AS group_role, dm.role AS doc_role
FROM documents d
JOIN groups g ON g.id = d.group_id AND g.deleted_at IS NULL
LEFT JOIN group_members gm ON gm.group_id = d.group_id AND gm.user_id = sqlc.arg('user_id')
LEFT JOIN doc_members dm ON dm.doc_id = d.id AND dm.user_id = sqlc.arg('user_id')
WHERE d.id = sqlc.arg('id') AND d.deleted_at IS NULL;

-- name: GetGroupForDocumentCreate :one
-- 建文档前的群组解析：返回群组 ID 与当前用户的群内角色（非成员为 NULL）。
SELECT g.id, g.slug, g.name, gm.role AS my_role
FROM groups g
LEFT JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = $2
WHERE g.slug = $1 AND g.deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 文档成员（doc_members）
-- ---------------------------------------------------------------------------

-- name: ListDocMembers :many
-- 文档级显式成员。群成员的基础权限来自 documents.default_role，不在此列表里；
-- 这里只列出被单独授予/调整过角色的用户。
SELECT dm.doc_id, dm.user_id, dm.role, dm.granted_by, dm.created_at,
       u.name, u.handle, u.verify_type
FROM doc_members dm
JOIN users u ON u.id = dm.user_id
WHERE dm.doc_id = $1
ORDER BY dm.created_at ASC, dm.user_id ASC;

-- name: GetDocMember :one
SELECT * FROM doc_members WHERE doc_id = $1 AND user_id = $2;

-- name: GetGroupMemberRole :one
-- 校验目标用户是否为群成员：doc_members 只在群成员范围内生效。
SELECT role FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: GetDocMemberView :one
SELECT dm.doc_id, dm.user_id, dm.role, dm.granted_by, dm.created_at,
       u.name, u.handle, u.verify_type
FROM doc_members dm
JOIN users u ON u.id = dm.user_id
WHERE dm.doc_id = $1 AND dm.user_id = $2;

-- name: UpsertDocMember :one
-- 幂等授予/覆盖角色：同一用户重复设置只更新角色与授权人。
INSERT INTO doc_members (doc_id, user_id, role, granted_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (doc_id, user_id) DO UPDATE
SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by
RETURNING *;

-- name: DeleteDocMember :execrows
DELETE FROM doc_members WHERE doc_id = $1 AND user_id = $2;

-- ---------------------------------------------------------------------------
-- 快照（doc_snapshots）
-- ---------------------------------------------------------------------------

-- name: BumpDocumentSnapshot :one
-- 原子地把 latest_snapshot +1 并返回新版本号。
-- UPDATE 会锁住文档行，并发创建快照时版本号不会重复。
UPDATE documents SET latest_snapshot = latest_snapshot + 1
WHERE id = $1 AND deleted_at IS NULL
RETURNING latest_snapshot;

-- name: InsertSnapshot :one
INSERT INTO doc_snapshots (doc_id, version, storage_key, size_bytes, label, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListSnapshots :many
-- 按版本号倒序翻页。版本号单调递增且唯一，比时间戳更适合做快照游标。
SELECT s.doc_id, s.version, s.storage_key, s.size_bytes, s.label, s.created_by, s.created_at,
       u.name AS creator_name, u.handle AS creator_handle
FROM doc_snapshots s
LEFT JOIN users u ON u.id = s.created_by
WHERE s.doc_id = sqlc.arg('doc_id')
  AND (sqlc.narg('before_version')::integer IS NULL OR s.version < sqlc.narg('before_version')::integer)
ORDER BY s.version DESC
LIMIT sqlc.arg('limit');

-- name: GetSnapshot :one
-- 读取单个版本的快照记录，供「下载快照」与「回滚到该版本」使用。
-- 回滚不会复制二进制：新记录直接引用旧记录的 storage_key（见 doc.RestoreSnapshot）。
-- 因此清理历史快照前必须确认没有其他记录仍在引用同一个 storage_key。
SELECT * FROM doc_snapshots WHERE doc_id = $1 AND version = $2;

-- ---------------------------------------------------------------------------
-- 批注（annotations）
-- ---------------------------------------------------------------------------
-- 批注是多态表：文档批注固定用 target_type = 'document'，target_id = 文档 ID。

-- name: CreateAnnotation :one
INSERT INTO annotations (target_type, target_id, author_id, anchor, quoted_text, content)
VALUES ('document', $1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAnnotation :one
SELECT a.id, a.target_type, a.target_id, a.author_id, a.anchor, a.quoted_text,
       a.content, a.resolved_at, a.resolved_by, a.created_at, a.deleted_at,
       u.name AS author_name, u.handle AS author_handle
FROM annotations a
JOIN users u ON u.id = a.author_id
WHERE a.id = $1 AND a.target_type = 'document' AND a.target_id = $2 AND a.deleted_at IS NULL;

-- name: ListAnnotations :many
SELECT a.id, a.target_type, a.target_id, a.author_id, a.anchor, a.quoted_text,
       a.content, a.resolved_at, a.resolved_by, a.created_at, a.deleted_at,
       u.name AS author_name, u.handle AS author_handle
FROM annotations a
JOIN users u ON u.id = a.author_id
WHERE a.target_type = 'document' AND a.target_id = $1 AND a.deleted_at IS NULL
ORDER BY a.created_at ASC, a.id ASC;

-- name: UpdateAnnotationContent :one
UPDATE annotations SET content = $3
WHERE id = $1 AND target_type = 'document' AND target_id = $2 AND deleted_at IS NULL
RETURNING id;

-- name: SetAnnotationResolved :one
-- resolved_at 非空表示标记已解决，为空表示取消解决。
UPDATE annotations SET resolved_at = $3, resolved_by = $4
WHERE id = $1 AND target_type = 'document' AND target_id = $2 AND deleted_at IS NULL
RETURNING id;
