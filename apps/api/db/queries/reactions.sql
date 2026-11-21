-- name: AddReaction :execrows
INSERT INTO reactions (user_id, target_type, target_id, kind)
VALUES ($1, 'post', $2, $3)
ON CONFLICT DO NOTHING;

-- name: RemoveReaction :execrows
DELETE FROM reactions
WHERE user_id = $1 AND target_type = 'post' AND target_id = $2 AND kind = $3;

-- name: AddLikeCount :one
UPDATE posts SET like_count = like_count + 1, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING like_count;

-- name: RemoveLikeCount :one
UPDATE posts SET like_count = greatest(like_count - 1, 0), updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING like_count;

-- name: GetLikeCount :one
SELECT like_count FROM posts WHERE id = $1 AND deleted_at IS NULL;

-- name: HasReaction :one
SELECT EXISTS(SELECT 1 FROM reactions
WHERE user_id = $1 AND target_type = 'post' AND target_id = $2 AND kind = $3);

-- name: ListBookmarkedPosts :many
-- 收藏列表：按「收藏时间」而不是帖子发布时间倒序。
--
-- 排序键来自 reactions.created_at，因此游标是 (reactions.created_at, target_id)。
-- 投影列与 GetPostByID 完全一致，多一列 bookmarked_at 供游标使用；
-- 服务层用 content.PostFromRow 复用同一份 PostDTO 投影。
--
-- 可见性谓词与信息流相同：收藏之后作者可能把帖子改为 followers / group / private，
-- 收藏列表不能因此把它重新暴露给已经无权查看的人。
SELECT p.*, u.name AS author_name, u.handle AS author_handle, u.verify_type AS author_verify_type,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = sqlc.arg('user_id') AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'like') AS liked,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = sqlc.arg('user_id') AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'bookmark') AS bookmarked,
       COALESCE((SELECT array_agg(t.name ORDER BY t.name) FROM post_tags pt JOIN tags t ON t.id = pt.tag_id WHERE pt.post_id = p.id), '{}') AS tags,
       op.id AS paper_id_value, op.doi AS paper_doi, op.arxiv_id AS paper_arxiv_id, op.openreview_id AS paper_openreview_id,
       op.title AS paper_title, op.authors AS paper_authors, op.abstract AS paper_abstract, op.venue AS paper_venue,
       op.published_at AS paper_published_at, op.citation_count AS paper_citation_count, op.meta_source AS paper_meta_source, op.created_at AS paper_created_at,
       rp.id AS repost_id, ru.name AS repost_author_name, rp.content_text AS repost_content_text,
       rpp.id AS repost_paper_id, rpp.doi AS repost_paper_doi, rpp.arxiv_id AS repost_paper_arxiv_id, rpp.openreview_id AS repost_paper_openreview_id,
       rpp.title AS repost_paper_title, rpp.authors AS repost_paper_authors, rpp.abstract AS repost_paper_abstract, rpp.venue AS repost_paper_venue,
       rpp.published_at AS repost_paper_published_at, rpp.citation_count AS repost_paper_citation_count, rpp.meta_source AS repost_paper_meta_source, rpp.created_at AS repost_paper_created_at,
       b.created_at AS bookmarked_at
FROM reactions b
JOIN posts p ON p.id = b.target_id AND p.deleted_at IS NULL
JOIN users u ON u.id = p.author_id
LEFT JOIN papers op ON op.id = p.paper_id
LEFT JOIN posts rp ON rp.id = p.repost_of AND rp.deleted_at IS NULL
    AND (
      rp.author_id = sqlc.arg('user_id')
      OR rp.visibility = 'public'
      OR (rp.visibility = 'followers'
          AND EXISTS(SELECT 1 FROM follows rf WHERE rf.follower_id = sqlc.arg('user_id') AND rf.followee_id = rp.author_id))
      OR (rp.visibility = 'group' AND rp.group_id IS NOT NULL
          AND EXISTS(SELECT 1 FROM group_members rgm WHERE rgm.group_id = rp.group_id AND rgm.user_id = sqlc.arg('user_id')))
    )
LEFT JOIN users ru ON ru.id = rp.author_id
LEFT JOIN papers rpp ON rpp.id = rp.paper_id
WHERE b.user_id = sqlc.arg('user_id') AND b.target_type = 'post' AND b.kind = 'bookmark'
  AND (
    p.author_id = sqlc.arg('user_id')
    OR p.visibility = 'public'
    OR (p.visibility = 'followers'
        AND EXISTS(SELECT 1 FROM follows f WHERE f.follower_id = sqlc.arg('user_id') AND f.followee_id = p.author_id))
    OR (p.visibility = 'group' AND p.group_id IS NOT NULL
        AND EXISTS(SELECT 1 FROM group_members gm WHERE gm.group_id = p.group_id AND gm.user_id = sqlc.arg('user_id')))
  )
  AND (sqlc.narg('cursor_time')::timestamptz IS NULL
       OR (b.created_at, b.target_id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY b.created_at DESC, b.target_id DESC
LIMIT sqlc.arg('limit')::int;
