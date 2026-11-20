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
  AND ($2::real = 'Infinity'::real OR (p.hot_score, p.created_at, p.id) < ($2::real, $3::timestamptz, $4::uuid))
ORDER BY p.hot_score DESC, p.created_at DESC, p.id DESC
LIMIT $5;

-- name: GetGroupMembership :one
-- 发帖到群组前校验：群未删除，且当前用户是有效成员。
SELECT gm.role FROM group_members gm
JOIN groups g ON g.id = gm.group_id
WHERE gm.group_id = $1 AND gm.user_id = $2 AND g.deleted_at IS NULL;
