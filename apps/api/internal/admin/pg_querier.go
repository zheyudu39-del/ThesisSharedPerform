package admin

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// pgQuerier 把 sqlc 生成的 db.Queries 适配成 admin.Querier。
//
// 为什么不直接让服务依赖 *db.Queries：后台的每个查询都要在测试里注入替身
// （尤其是「最后一名管理员」这类边界），接口比具体类型好替换。
type pgQuerier struct{ q *db.Queries }

func (p *pgQuerier) AdminOverviewTotals(ctx context.Context) (db.AdminOverviewTotalsRow, error) {
	return p.q.AdminOverviewTotals(ctx)
}

func (p *pgQuerier) AdminDailyActivity(ctx context.Context, days int32) ([]db.AdminDailyActivityRow, error) {
	return p.q.AdminDailyActivity(ctx, days)
}

func (p *pgQuerier) AdminTopTags(ctx context.Context, limit int32) ([]db.AdminTopTagsRow, error) {
	return p.q.AdminTopTags(ctx, limit)
}

func (p *pgQuerier) AdminListUsers(ctx context.Context, arg db.AdminListUsersParams) ([]db.AdminListUsersRow, error) {
	return p.q.AdminListUsers(ctx, arg)
}

func (p *pgQuerier) AdminCountUsers(ctx context.Context) (int64, error) {
	return p.q.AdminCountUsers(ctx)
}

func (p *pgQuerier) AdminSetUserRole(ctx context.Context, arg db.AdminSetUserRoleParams) (db.AdminSetUserRoleRow, error) {
	return p.q.AdminSetUserRole(ctx, arg)
}

func (p *pgQuerier) AdminSetUserStatus(ctx context.Context, arg db.AdminSetUserStatusParams) (db.AdminSetUserStatusRow, error) {
	return p.q.AdminSetUserStatus(ctx, arg)
}

func (p *pgQuerier) AdminCountAdmins(ctx context.Context) (int64, error) {
	return p.q.AdminCountAdmins(ctx)
}

func (p *pgQuerier) AdminListPosts(ctx context.Context, arg db.AdminListPostsParams) ([]db.AdminListPostsRow, error) {
	return p.q.AdminListPosts(ctx, arg)
}

func (p *pgQuerier) AdminSoftDeletePost(ctx context.Context, id pgtype.UUID) (db.AdminSoftDeletePostRow, error) {
	return p.q.AdminSoftDeletePost(ctx, id)
}

func (p *pgQuerier) AdminRestorePost(ctx context.Context, id pgtype.UUID) (db.AdminRestorePostRow, error) {
	return p.q.AdminRestorePost(ctx, id)
}

func (p *pgQuerier) AdminListPapers(ctx context.Context, arg db.AdminListPapersParams) ([]db.AdminListPapersRow, error) {
	return p.q.AdminListPapers(ctx, arg)
}

func (p *pgQuerier) AdminListComments(ctx context.Context, arg db.AdminListCommentsParams) ([]db.AdminListCommentsRow, error) {
	return p.q.AdminListComments(ctx, arg)
}

func (p *pgQuerier) AdminSoftDeleteComment(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return p.q.AdminSoftDeleteComment(ctx, id)
}

func (p *pgQuerier) AdminRestoreComment(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return p.q.AdminRestoreComment(ctx, id)
}

func (p *pgQuerier) AdminListGroups(ctx context.Context, arg db.AdminListGroupsParams) ([]db.AdminListGroupsRow, error) {
	return p.q.AdminListGroups(ctx, arg)
}

func (p *pgQuerier) AdminSoftDeleteGroup(ctx context.Context, id pgtype.UUID) (db.AdminSoftDeleteGroupRow, error) {
	return p.q.AdminSoftDeleteGroup(ctx, id)
}

func (p *pgQuerier) AdminRestoreGroup(ctx context.Context, id pgtype.UUID) (db.AdminRestoreGroupRow, error) {
	return p.q.AdminRestoreGroup(ctx, id)
}

func (p *pgQuerier) AdminListFiles(ctx context.Context, arg db.AdminListFilesParams) ([]db.AdminListFilesRow, error) {
	return p.q.AdminListFiles(ctx, arg)
}

func (p *pgQuerier) AdminFileStats(ctx context.Context) ([]db.AdminFileStatsRow, error) {
	return p.q.AdminFileStats(ctx)
}

// IsAdmin 判定调用者是否为运营。
//
// 查不到行（用户不存在或已注销）返回 pgx.ErrNoRows，由服务层翻译成 403——
// 后台不区分「不存在」与「无权限」，避免成为账号枚举通道。
func (p *pgQuerier) IsAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	return p.q.AdminIsAdmin(ctx, pgtype.UUID{Bytes: userID, Valid: true})
}
