-- 群组相关查询。
--
-- 约定：
--   * member_count 是冗余计数，但每次成员增删都在同一事务内用
--     RecomputeGroupMemberCount 从 group_members 实际行数重算，
--     因此它与真实成员数永远一致（不会因为并发或失败路径漂移）。
--   * 群组视图统一附带 my_role（当前查看者在群内的角色，非成员为 NULL），
--     供 DTO 直接输出，避免服务层再查一次成员表。

-- name: CreateGroup :one
INSERT INTO groups (name, slug, description, owner_id, join_policy, member_count)
VALUES ($1, $2, $3, $4, $5, 1)
RETURNING *;

-- name: GroupSlugExists :one
SELECT EXISTS(SELECT 1 FROM groups WHERE slug = $1);

-- name: GetGroupBySlug :one
-- $1 是 slug，$2 是当前查看者 ID（匿名传 NULL，此时 my_role 恒为 NULL）。
SELECT g.*, u.name AS owner_name, gm.role AS my_role
FROM groups g
JOIN users u ON u.id = g.owner_id
LEFT JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = $2
WHERE g.slug = $1 AND g.deleted_at IS NULL;

-- name: GetGroupByID :one
SELECT g.*, u.name AS owner_name, gm.role AS my_role
FROM groups g
JOIN users u ON u.id = g.owner_id
LEFT JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = $2
WHERE g.id = $1 AND g.deleted_at IS NULL;

-- name: UpdateGroup :one
-- 局部更新：参数为 NULL 表示「本次不修改」。
UPDATE groups SET
    name        = COALESCE(sqlc.narg('name')::varchar(64), name),
    description = COALESCE(sqlc.narg('description')::varchar(500), description),
    join_policy = COALESCE(sqlc.narg('join_policy')::group_join_policy, join_policy)
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: ListPublicGroups :many
-- 仅列出 joinPolicy = public 的群，供「发现群组」使用。
-- 游标按 (created_at, id) 倒序翻页。
SELECT g.*, u.name AS owner_name, gm.role AS my_role
FROM groups g
JOIN users u ON u.id = g.owner_id
LEFT JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = $1
WHERE g.deleted_at IS NULL AND g.join_policy = 'public'
  AND ($2::timestamptz IS NULL OR (g.created_at, g.id) < ($2::timestamptz, $3::uuid))
ORDER BY g.created_at DESC, g.id DESC
LIMIT $4;

-- name: AddGroupMember :execrows
-- 幂等：重复加入不会报错，也不会产生第二行；返回受影响行数供调用方判断是否新加入。
INSERT INTO group_members (group_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: GetGroupMember :one
SELECT * FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: GetGroupMemberView :one
SELECT gm.group_id, gm.user_id, gm.role, gm.joined_at,
       u.name, u.handle, u.verify_type
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1 AND gm.user_id = $2;

-- name: ListGroupMembers :many
SELECT gm.group_id, gm.user_id, gm.role, gm.joined_at,
       u.name, u.handle, u.verify_type
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1
  AND ($2::timestamptz IS NULL OR (gm.joined_at, gm.user_id) > ($2::timestamptz, $3::uuid))
ORDER BY gm.joined_at ASC, gm.user_id ASC
LIMIT $4;

-- name: UpdateGroupMemberRole :one
UPDATE group_members SET role = $3
WHERE group_id = $1 AND user_id = $2
RETURNING *;

-- name: RemoveGroupMember :execrows
DELETE FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: RecomputeGroupMemberCount :one
-- 从实际行数重算冗余计数。UPDATE 会锁住 groups 行，
-- 使同一群组的成员变更在事务间串行，计数不会因并发而漂移。
UPDATE groups SET member_count = (SELECT COUNT(*) FROM group_members WHERE group_id = $1)::integer
WHERE id = $1 AND deleted_at IS NULL
RETURNING member_count;

-- name: CreateGroupInvite :one
INSERT INTO group_invites (group_id, code, created_by, expires_at, max_uses)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetGroupInviteByCode :one
SELECT * FROM group_invites WHERE code = $1;

-- name: ConsumeGroupInvite :one
-- 原子地消耗一次使用额度：只有「未过期且未用尽」时才 +1 并返回行。
-- 并发下多个请求同时用同一邀请码，也只有一个能越过 max_uses 限制；
-- 过期判断放在同一条 UPDATE 里，避免「先查后改」之间邀请码刚好过期。
UPDATE group_invites SET used_count = used_count + 1
WHERE id = $1
  AND (max_uses IS NULL OR used_count < max_uses)
  AND (expires_at IS NULL OR expires_at > now())
RETURNING *;

-- name: ListGroupFeed :many
-- 群内动态：投影与 GetPostByID 完全一致（列名、顺序都相同），
-- 这样服务层可以直接转换复用 content 模块的 toPost。
-- $1 群组 ID，$2 查看者（成员校验已在服务层完成），
-- $3/$4 为 (created_at, id) 游标，$5 为 limit。
SELECT p.*, u.name AS author_name, u.handle AS author_handle, u.verify_type AS author_verify_type,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $2 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'like') AS liked,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $2 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'bookmark') AS bookmarked,
       COALESCE((SELECT array_agg(t.name ORDER BY t.name) FROM post_tags pt JOIN tags t ON t.id = pt.tag_id WHERE pt.post_id = p.id), '{}') AS tags,
       op.id AS paper_id_value, op.doi AS paper_doi, op.arxiv_id AS paper_arxiv_id, op.openreview_id AS paper_openreview_id,
       op.title AS paper_title, op.authors AS paper_authors, op.abstract AS paper_abstract, op.venue AS paper_venue,
       op.published_at AS paper_published_at, op.citation_count AS paper_citation_count, op.meta_source AS paper_meta_source, op.created_at AS paper_created_at,
       rp.id AS repost_id, ru.name AS repost_author_name, rp.content_text AS repost_content_text,
       rpp.id AS repost_paper_id, rpp.doi AS repost_paper_doi, rpp.arxiv_id AS repost_paper_arxiv_id, rpp.openreview_id AS repost_paper_openreview_id,
       rpp.title AS repost_paper_title, rpp.authors AS repost_paper_authors, rpp.abstract AS repost_paper_abstract, rpp.venue AS repost_paper_venue,
       rpp.published_at AS repost_paper_published_at, rpp.citation_count AS repost_paper_citation_count, rpp.meta_source AS repost_paper_meta_source, rpp.created_at AS repost_paper_created_at
FROM posts p
JOIN users u ON u.id = p.author_id
LEFT JOIN papers op ON op.id = p.paper_id
LEFT JOIN posts rp ON rp.id = p.repost_of AND rp.deleted_at IS NULL
    AND (
      rp.author_id = $2
      OR rp.visibility = 'public'
      OR (rp.visibility = 'followers'
          AND EXISTS(SELECT 1 FROM follows rf WHERE rf.follower_id = $2 AND rf.followee_id = rp.author_id))
      OR (rp.visibility = 'group' AND rp.group_id IS NOT NULL
          AND EXISTS(SELECT 1 FROM group_members rgm WHERE rgm.group_id = rp.group_id AND rgm.user_id = $2))
    )
LEFT JOIN users ru ON ru.id = rp.author_id
LEFT JOIN papers rpp ON rpp.id = rp.paper_id
WHERE p.deleted_at IS NULL AND p.group_id = $1
  AND ($3::timestamptz IS NULL OR (p.created_at, p.id) < ($3::timestamptz, $4::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT $5;

-- name: GetJoinRequestByUser :one
SELECT * FROM join_requests WHERE group_id = $1 AND user_id = $2;

-- name: CreateJoinRequest :one
-- 同一用户对同一群只有一行申请（UNIQUE(group_id, user_id)）。
-- 被拒后可以重新申请：冲突时把旧行重置为 pending 并更新时间与留言。
INSERT INTO join_requests (group_id, user_id, message)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO UPDATE
SET message     = EXCLUDED.message,
    status      = 'pending',
    reviewed_by = NULL,
    created_at  = now()
RETURNING *;

-- name: GetJoinRequestByID :one
SELECT jr.id, jr.group_id, jr.user_id, jr.message, jr.status, jr.created_at,
       u.name AS user_name, u.handle AS user_handle
FROM join_requests jr
JOIN users u ON u.id = jr.user_id
WHERE jr.id = $1 AND jr.group_id = $2;

-- name: ListJoinRequests :many
-- 只返回待处理的申请，按提交时间正序（先到先审）。
SELECT jr.id, jr.group_id, jr.user_id, jr.message, jr.status, jr.created_at,
       u.name AS user_name, u.handle AS user_handle
FROM join_requests jr
JOIN users u ON u.id = jr.user_id
WHERE jr.group_id = $1 AND jr.status = 'pending'
ORDER BY jr.created_at ASC, jr.id ASC
LIMIT 100;

-- name: UpdateJoinRequestStatus :one
-- 只有 pending 的申请能被审核，重复审核返回 0 行。
UPDATE join_requests SET status = $3, reviewed_by = $4
WHERE id = $1 AND group_id = $2 AND status = 'pending'
RETURNING *;
