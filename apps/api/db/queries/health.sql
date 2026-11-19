-- 占位查询文件：sqlc 要求 queries 目录至少可被解析。
-- 具体查询（关注流、热门流、权限判定等）随功能开发逐步加入，
-- SQL 直接取自 docs/data-model.md §5「关键查询模式」。

-- name: Ping :one
SELECT 1 AS ok;
