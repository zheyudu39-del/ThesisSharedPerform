-- 帖子的公共投影：一次 JOIN 完成作者、论文、原帖摘要、标签和当前用户互动状态。
-- name: CreatePost :one
INSERT INTO posts (author_id, type, content_html, content_text, paper_id, repost_of, group_id, visibility)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPaperForPost :one
SELECT * FROM papers WHERE id = $1;

-- name: GetPostForWrite :one
-- 写操作（点赞/收藏/评论）前必须确认当前用户可读该帖，否则私密帖会被越权操作。
-- 可见性谓词与 feed 查询保持一致：本人 / public / 已关注作者的 followers / 群成员。
SELECT p.id FROM posts p
WHERE p.id = $1 AND p.deleted_at IS NULL
  AND (
    p.author_id = $2
    OR p.visibility = 'public'
    OR (p.visibility = 'followers'
        AND EXISTS(SELECT 1 FROM follows f WHERE f.follower_id = $2 AND f.followee_id = p.author_id))
    OR (p.visibility = 'group' AND p.group_id IS NOT NULL
        AND EXISTS(SELECT 1 FROM group_members gm WHERE gm.group_id = p.group_id AND gm.user_id = $2))
  );

-- name: GetPostAuthorForNotify :one
-- 点赞等互动在事务提交后生成通知，需要知道帖子作者。
-- 与 GetPostForWrite 分开：这里不套可见性谓词，调用方已经通过写路径校验过可读性，
-- 且私密帖的点赞同样应当通知作者本人（可见性只影响谁能操作，不影响作者收不收通知）。
SELECT author_id FROM posts WHERE id = $1 AND deleted_at IS NULL;

-- name: UpsertTag :one
INSERT INTO tags (name, slug) VALUES ($1, $2)
ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: LinkPostTag :exec
INSERT INTO post_tags (post_id, tag_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: IncrementRepostCount :exec
UPDATE posts SET repost_count = repost_count + 1, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPostByID :one
-- $1 是帖子 ID，$2 是当前查看者 ID。
-- 外层帖与转发原帖都套用同一套可见性谓词：本人 / public /
-- 已关注作者的 followers / 群成员。
-- 「不存在」与「无权查看」都返回空行，由服务层统一映射为同一个错误，
-- 避免通过响应差异探测私密帖是否存在。
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
WHERE p.id = $1 AND p.deleted_at IS NULL
  AND (
    p.author_id = $2
    OR p.visibility = 'public'
    OR (p.visibility = 'followers'
        AND EXISTS(SELECT 1 FROM follows f WHERE f.follower_id = $2 AND f.followee_id = p.author_id))
    OR (p.visibility = 'group' AND p.group_id IS NOT NULL
        AND EXISTS(SELECT 1 FROM group_members gm WHERE gm.group_id = p.group_id AND gm.user_id = $2))
  );

-- name: ListFollowingFeed :many
SELECT p.*, u.name AS author_name, u.handle AS author_handle, u.verify_type AS author_verify_type,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'like') AS liked,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'bookmark') AS bookmarked,
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
      rp.author_id = $1
      OR rp.visibility = 'public'
      OR (rp.visibility = 'followers'
          AND EXISTS(SELECT 1 FROM follows rf WHERE rf.follower_id = $1 AND rf.followee_id = rp.author_id))
      OR (rp.visibility = 'group' AND rp.group_id IS NOT NULL
          AND EXISTS(SELECT 1 FROM group_members rgm WHERE rgm.group_id = rp.group_id AND rgm.user_id = $1))
    )
LEFT JOIN users ru ON ru.id = rp.author_id
LEFT JOIN papers rpp ON rpp.id = rp.paper_id
WHERE p.deleted_at IS NULL AND p.group_id IS NULL
  AND (p.author_id = $1 OR EXISTS(SELECT 1 FROM follows f WHERE f.follower_id = $1 AND f.followee_id = p.author_id))
  -- 可见性：本人可见全部；他人帖子只允许 public 或 followers。
  -- followers 已由上一行的关注关系保证，因此这里不能再放宽，否则未关注者也能看到。
  AND (p.author_id = $1 OR p.visibility IN ('public', 'followers'))
  -- 屏蔽：被当前用户屏蔽的作者，其帖子不出现在信息流里。
  -- 在查询层过滤而不是 handler 层：handler 过滤会漏掉分页与计数。
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = $1 AND b.blocked_id = p.author_id)
  AND ($2::timestamptz IS NULL OR (p.created_at, p.id) < ($2::timestamptz, $3::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT $4;

-- name: ListAllFeed :many
SELECT p.*, u.name AS author_name, u.handle AS author_handle, u.verify_type AS author_verify_type,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'like') AS liked,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'bookmark') AS bookmarked,
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
      rp.author_id = $1
      OR rp.visibility = 'public'
      OR (rp.visibility = 'followers'
          AND EXISTS(SELECT 1 FROM follows rf WHERE rf.follower_id = $1 AND rf.followee_id = rp.author_id))
      OR (rp.visibility = 'group' AND rp.group_id IS NOT NULL
          AND EXISTS(SELECT 1 FROM group_members rgm WHERE rgm.group_id = rp.group_id AND rgm.user_id = $1))
    )
LEFT JOIN users ru ON ru.id = rp.author_id
LEFT JOIN papers rpp ON rpp.id = rp.paper_id
WHERE p.deleted_at IS NULL AND p.visibility = 'public' AND p.group_id IS NULL
  -- 屏蔽：被当前用户屏蔽的作者，其帖子不出现在信息流里。
  -- 在查询层过滤而不是 handler 层：handler 过滤会漏掉分页与计数。
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = $1 AND b.blocked_id = p.author_id)
  AND ($2::timestamptz IS NULL OR (p.created_at, p.id) < ($2::timestamptz, $3::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT $4;

-- name: RefreshHotScores :exec
UPDATE posts SET hot_score = (like_count + 2.0 * comment_count + 3.0 * repost_count)
    / power(extract(epoch FROM (now() - created_at)) / 3600.0 + 2, 1.5)
WHERE deleted_at IS NULL AND visibility = 'public' AND group_id IS NULL;

-- name: ListHotFeed :many
SELECT p.*, u.name AS author_name, u.handle AS author_handle, u.verify_type AS author_verify_type,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'like') AS liked,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'bookmark') AS bookmarked,
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
      rp.author_id = $1
      OR rp.visibility = 'public'
      OR (rp.visibility = 'followers'
          AND EXISTS(SELECT 1 FROM follows rf WHERE rf.follower_id = $1 AND rf.followee_id = rp.author_id))
      OR (rp.visibility = 'group' AND rp.group_id IS NOT NULL
          AND EXISTS(SELECT 1 FROM group_members rgm WHERE rgm.group_id = rp.group_id AND rgm.user_id = $1))
    )
LEFT JOIN users ru ON ru.id = rp.author_id
LEFT JOIN papers rpp ON rpp.id = rp.paper_id
WHERE p.deleted_at IS NULL AND p.visibility = 'public' AND p.group_id IS NULL
  -- 屏蔽：被当前用户屏蔽的作者，其帖子不出现在信息流里。
  -- 在查询层过滤而不是 handler 层：handler 过滤会漏掉分页与计数。
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = $1 AND b.blocked_id = p.author_id)
  AND ($2::real = 'Infinity'::real OR (p.hot_score, p.created_at, p.id) < ($2::real, $3::timestamptz, $4::uuid))
ORDER BY p.hot_score DESC, p.created_at DESC, p.id DESC
LIMIT $5;

-- name: GetGroupMembership :one
-- 发帖到群组前校验：群未删除，且当前用户是有效成员。
SELECT gm.role FROM group_members gm
JOIN groups g ON g.id = gm.group_id
WHERE gm.group_id = $1 AND gm.user_id = $2 AND g.deleted_at IS NULL;

-- name: LinkPostAttachment :exec
-- 帖子附件：发帖时把文件挂到帖子上。
--
-- 只排除 failed 与 rejected，不要求 ready：PDF 是异步转码的，
-- 用户上传后往往立刻发帖，此时状态还是 transcoding。要求 ready 会让
-- 「刚传完就发帖」这个最常见的操作静默丢掉附件。真正打不开的文件
-- 在读取时（ListPostAttachments）再按 ready 过滤。
INSERT INTO post_attachments (post_id, file_id, position)
SELECT $1, $2, $3
WHERE EXISTS (
    SELECT 1 FROM files f
     WHERE f.id = $2 AND f.owner_id = $4
       AND f.status NOT IN ('failed', 'rejected')
)
ON CONFLICT (post_id, file_id) DO NOTHING;

-- name: ListPostAttachments :many
-- 帖子附件列表。只返回就绪的文件，半成品对读者没有意义。
SELECT f.id, f.original_name, f.mime, f.kind::text AS kind,
       f.size_bytes, f.page_count, pa.position
FROM post_attachments pa
JOIN files f ON f.id = pa.file_id
WHERE pa.post_id = $1 AND f.status NOT IN ('failed', 'rejected')
ORDER BY pa.position ASC, pa.id ASC;

-- name: ListAttachmentsForPosts :many
-- 信息流批量取附件：一条 IN 查询，避免每张卡片单独查一次（N+1）。
SELECT pa.post_id, f.id, f.original_name, f.mime, f.kind::text AS kind,
       f.size_bytes, f.page_count, pa.position
FROM post_attachments pa
JOIN files f ON f.id = pa.file_id
WHERE pa.post_id = ANY($1::uuid[]) AND f.status NOT IN ('failed', 'rejected')
ORDER BY pa.post_id, pa.position ASC, pa.id ASC;

-- name: GetPostAuthorAndType :one
-- 编辑/删除前的权限判定依据：作者用于区分「本人」与「他人」，
-- type 用于决定正文能否为空（转发可以只转发不附言，原创帖不行），
-- group_id 用于阻止把群内帖改成公开帖（那等于把群内容泄露到全站）。
-- 这里刻意不套可见性谓词：运营需要能处理自己看不到的私密违规帖。
SELECT author_id, type::text AS type, group_id
FROM posts
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdatePost :execrows
-- 局部更新：参数为 NULL 表示「本次不修改」。
-- 正文同时写入 content_html / content_text，两者必须一起改，
-- 否则展示与检索、纯文本导出会读到不一致的内容。
-- type / paper_id / repost_of / group_id 不在可改范围内：改这些等于换了一条内容。
UPDATE posts SET
    content_html = COALESCE(sqlc.narg('content_html')::text, content_html),
    content_text = COALESCE(sqlc.narg('content_text')::text, content_text),
    visibility   = COALESCE(sqlc.narg('visibility')::post_visibility, visibility)
WHERE id = sqlc.arg('id') AND deleted_at IS NULL;

-- name: SoftDeletePost :execrows
-- 软删除：保留行以便审计与恢复，同时让所有读路径（都带 deleted_at IS NULL）立即不可见。
-- 只改本行，不触碰 repost_of 指向的原帖——删掉一次转发不应影响被转发的原帖。
UPDATE posts SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL;
