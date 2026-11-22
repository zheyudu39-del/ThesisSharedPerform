package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// fakeQuerier 只实现测试用到的查询，其余方法返回零值。
type fakeQuerier struct {
	isAdmin    map[uuid.UUID]bool
	adminCount int64
	roleErr    error

	setRoleCalls   int
	setStatusCalls int
	deleteCalls    int
	restoreCalls   int
}

func (f *fakeQuerier) IsAdmin(_ context.Context, id uuid.UUID) (bool, error) {
	if f.roleErr != nil {
		return false, f.roleErr
	}
	return f.isAdmin[id], nil
}

func (f *fakeQuerier) AdminCountAdmins(context.Context) (int64, error) {
	return f.adminCount, nil
}

func (f *fakeQuerier) AdminSetUserRole(_ context.Context, arg db.AdminSetUserRoleParams) (db.AdminSetUserRoleRow, error) {
	f.setRoleCalls++
	return db.AdminSetUserRoleRow{ID: arg.ID, Handle: "target", Role: arg.Role, Status: "active"}, nil
}

func (f *fakeQuerier) AdminSetUserStatus(_ context.Context, arg db.AdminSetUserStatusParams) (db.AdminSetUserStatusRow, error) {
	f.setStatusCalls++
	return db.AdminSetUserStatusRow{ID: arg.ID, Handle: "target", Role: db.UserRoleUser, Status: arg.Status}, nil
}

func (f *fakeQuerier) AdminSoftDeletePost(_ context.Context, id pgtype.UUID) (db.AdminSoftDeletePostRow, error) {
	f.deleteCalls++
	return db.AdminSoftDeletePostRow{ID: id}, nil
}

func (f *fakeQuerier) AdminRestorePost(_ context.Context, id pgtype.UUID) (db.AdminRestorePostRow, error) {
	f.restoreCalls++
	return db.AdminRestorePostRow{ID: id}, nil
}

func (f *fakeQuerier) AdminListUsers(context.Context, db.AdminListUsersParams) ([]db.AdminListUsersRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminCountUsers(context.Context) (int64, error) { return 0, nil }
func (f *fakeQuerier) AdminOverviewTotals(context.Context) (db.AdminOverviewTotalsRow, error) {
	return db.AdminOverviewTotalsRow{}, nil
}
func (f *fakeQuerier) AdminDailyActivity(context.Context, int32) ([]db.AdminDailyActivityRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminTopTags(context.Context, int32) ([]db.AdminTopTagsRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminListPosts(context.Context, db.AdminListPostsParams) ([]db.AdminListPostsRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminListPapers(context.Context, db.AdminListPapersParams) ([]db.AdminListPapersRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminListComments(context.Context, db.AdminListCommentsParams) ([]db.AdminListCommentsRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminSoftDeleteComment(context.Context, pgtype.UUID) (pgtype.UUID, error) {
	f.deleteCalls++
	return pgtype.UUID{}, nil
}
func (f *fakeQuerier) AdminRestoreComment(context.Context, pgtype.UUID) (pgtype.UUID, error) {
	f.restoreCalls++
	return pgtype.UUID{}, nil
}
func (f *fakeQuerier) AdminListGroups(context.Context, db.AdminListGroupsParams) ([]db.AdminListGroupsRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminSoftDeleteGroup(context.Context, pgtype.UUID) (db.AdminSoftDeleteGroupRow, error) {
	return db.AdminSoftDeleteGroupRow{}, nil
}
func (f *fakeQuerier) AdminRestoreGroup(context.Context, pgtype.UUID) (db.AdminRestoreGroupRow, error) {
	return db.AdminRestoreGroupRow{}, nil
}
func (f *fakeQuerier) AdminListFiles(context.Context, db.AdminListFilesParams) ([]db.AdminListFilesRow, error) {
	return nil, nil
}
func (f *fakeQuerier) AdminFileStats(context.Context) ([]db.AdminFileStatsRow, error) {
	return nil, nil
}

func newTestService(f *fakeQuerier) *Service {
	s := NewServiceWithQuerier(f)
	s.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return s
}

var (
	adminID  = uuid.Must(uuid.NewV7())
	targetID = uuid.Must(uuid.NewV7())
)

// TestNonAdminIsRejected 每个后台方法都必须先过 requireAdmin。
func TestNonAdminIsRejected(t *testing.T) {
	f := &fakeQuerier{isAdmin: map[uuid.UUID]bool{adminID: false}}
	s := newTestService(f)
	ctx := context.Background()

	cases := map[string]error{}
	_, cases["overview"] = s.GetOverview(ctx, adminID, 7)
	_, cases["users"] = s.ListUsers(ctx, adminID, UserFilter{})
	_, cases["posts"] = s.ListPosts(ctx, adminID, ContentFilter{})
	_, cases["papers"] = s.ListPapers(ctx, adminID, ContentFilter{})
	_, cases["groups"] = s.ListGroups(ctx, adminID, ContentFilter{})
	_, cases["files"] = s.ListFiles(ctx, adminID, FileFilter{})
	cases["deletePost"] = s.DeletePost(ctx, adminID, targetID)
	cases["restorePost"] = s.RestorePost(ctx, adminID, targetID)
	cases["deleteGroup"] = s.DeleteGroup(ctx, adminID, targetID)
	cases["comments"] = nil
	_, cases["comments"] = s.ListComments(ctx, adminID, ContentFilter{})
	cases["deleteComment"] = s.DeleteComment(ctx, adminID, targetID)
	cases["restoreComment"] = s.RestoreComment(ctx, adminID, targetID)

	for name, err := range cases {
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s: 非运营应返回 ErrForbidden，实际 %v", name, err)
		}
	}
	if f.deleteCalls != 0 || f.restoreCalls != 0 || f.setRoleCalls != 0 {
		t.Fatal("鉴权失败时不应触达写查询")
	}
}

// TestAdminRoleLookupFailureIsForbidden 查不到用户（已注销）也按无权限处理，
// 不能因为 ErrNoRows 就放行。
func TestAdminRoleLookupFailureIsForbidden(t *testing.T) {
	f := &fakeQuerier{roleErr: pgx.ErrNoRows}
	s := newTestService(f)
	if _, err := s.GetOverview(context.Background(), adminID, 7); !errors.Is(err, ErrForbidden) {
		t.Fatalf("应返回 ErrForbidden，实际 %v", err)
	}
}

// TestCannotDemoteSelf 管理员不能撤销自己的权限，否则可能把自己锁在门外。
func TestCannotDemoteSelf(t *testing.T) {
	f := &fakeQuerier{isAdmin: map[uuid.UUID]bool{adminID: true}, adminCount: 5}
	s := newTestService(f)
	_, err := s.SetUserRole(context.Background(), adminID, adminID, "user")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("应拒绝自我降级，实际 %v", err)
	}
	if f.setRoleCalls != 0 {
		t.Fatal("被拒绝的请求不应写入")
	}
}

// TestCannotDemoteLastAdmin 降级最后一名管理员会让后台失去入口。
func TestCannotDemoteLastAdmin(t *testing.T) {
	f := &fakeQuerier{
		isAdmin:    map[uuid.UUID]bool{adminID: true, targetID: true},
		adminCount: 1,
	}
	s := newTestService(f)
	_, err := s.SetUserRole(context.Background(), adminID, targetID, "user")
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("应返回 ErrLastAdmin，实际 %v", err)
	}
	if f.setRoleCalls != 0 {
		t.Fatal("被拒绝的请求不应写入")
	}
}

// TestCannotSuspendLastAdmin 封禁同样受保护。
func TestCannotSuspendLastAdmin(t *testing.T) {
	f := &fakeQuerier{
		isAdmin:    map[uuid.UUID]bool{adminID: true, targetID: true},
		adminCount: 1,
	}
	s := newTestService(f)
	_, err := s.SetUserStatus(context.Background(), adminID, targetID, UserStatusSuspended)
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("应返回 ErrLastAdmin，实际 %v", err)
	}
}

// TestCannotSuspendSelf 管理员不能封禁自己。
func TestCannotSuspendSelf(t *testing.T) {
	f := &fakeQuerier{isAdmin: map[uuid.UUID]bool{adminID: true}, adminCount: 3}
	s := newTestService(f)
	_, err := s.SetUserStatus(context.Background(), adminID, adminID, UserStatusSuspended)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("应拒绝自我封禁，实际 %v", err)
	}
}

// TestDemoteOtherAdminWhenSeveralExist 有多名管理员时降级应当放行。
func TestDemoteOtherAdminWhenSeveralExist(t *testing.T) {
	f := &fakeQuerier{
		isAdmin:    map[uuid.UUID]bool{adminID: true, targetID: true},
		adminCount: 2,
	}
	s := newTestService(f)
	if _, err := s.SetUserRole(context.Background(), adminID, targetID, "user"); err != nil {
		t.Fatalf("应允许降级，实际 %v", err)
	}
	if f.setRoleCalls != 1 {
		t.Fatalf("应写入一次，实际 %d", f.setRoleCalls)
	}
}

// TestInvalidRoleRejected 角色白名单。
func TestInvalidRoleRejected(t *testing.T) {
	f := &fakeQuerier{isAdmin: map[uuid.UUID]bool{adminID: true}, adminCount: 2}
	s := newTestService(f)
	if _, err := s.SetUserRole(context.Background(), adminID, targetID, "root"); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法角色应被拒绝，实际 %v", err)
	}
	if _, err := s.SetUserStatus(context.Background(), adminID, targetID, "deleted"); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法状态应被拒绝，实际 %v", err)
	}
}

// TestSearchPatternEscapesWildcards 搜索词里的 % 与 _ 必须转义，
// 否则「100%」会被当成前缀通配符。
func TestSearchPatternEscapesWildcards(t *testing.T) {
	got, err := searchPattern("100%_x")
	if err != nil {
		t.Fatalf("searchPattern: %v", err)
	}
	if got == nil {
		t.Fatal("应返回模式")
	}
	want := `%100\%\_x%`
	if *got != want {
		t.Fatalf("转义结果 = %q，期望 %q", *got, want)
	}
}

// TestSearchPatternTooLong 搜索词长度上限。
func TestSearchPatternTooLong(t *testing.T) {
	long := ""
	for i := 0; i < 65; i++ {
		long += "a"
	}
	if _, err := searchPattern(long); !errors.Is(err, ErrValidation) {
		t.Fatalf("超长搜索词应被拒绝，实际 %v", err)
	}
}

// TestCursorRoundTrip 游标往返一致，且不能跨 stream 复用。
func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	id := uuid.Must(uuid.NewV7())
	encoded := encodeCursor("users", at, id)
	if encoded == nil {
		t.Fatal("应生成游标")
	}

	gotAt, gotID, err := decodeCursor(*encoded, "users")
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if uuid.UUID(gotID.Bytes) != id || !gotAt.Time.Equal(at) {
		t.Fatal("游标往返不一致")
	}

	if _, _, err := decodeCursor(*encoded, "posts"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("跨 stream 复用游标应被拒绝")
	}
}

// TestEmptyCursorIsFirstPage 空游标表示第一页，不是错误。
func TestEmptyCursorIsFirstPage(t *testing.T) {
	at, id, err := decodeCursor("", "users")
	if err != nil {
		t.Fatalf("空游标不应报错：%v", err)
	}
	if at.Valid || id.Valid {
		t.Fatal("空游标应返回零值")
	}
}

// TestNormalizeLimit 分页上限收敛。
func TestNormalizeLimit(t *testing.T) {
	cases := map[int32]int32{0: defaultLimit, -5: defaultLimit, 10: 10, 1000: maxLimit}
	for in, want := range cases {
		if got := normalizeLimit(in); got != want {
			t.Fatalf("normalizeLimit(%d) = %d，期望 %d", in, got, want)
		}
	}
}
