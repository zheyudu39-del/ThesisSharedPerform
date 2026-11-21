package moderation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeQuerier 覆盖服务层关键路径：举报去重、自举报、权限与屏蔽幂等。
// 状态放在 map 里，保证「同一份状态」贯穿所有断言。
type fakeQuerier struct {
	admins    map[uuid.UUID]bool
	users     map[string]uuid.UUID
	authors   map[uuid.UUID]uuid.UUID // targetID -> authorID
	reports   map[uuid.UUID]*ReportRow
	seen      map[string]bool // reporter|type|target 去重键
	blocks    map[string]bool // blocker|blocked
	blockedAt map[string]time.Time
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		admins:    map[uuid.UUID]bool{},
		users:     map[string]uuid.UUID{},
		authors:   map[uuid.UUID]uuid.UUID{},
		reports:   map[uuid.UUID]*ReportRow{},
		seen:      map[string]bool{},
		blocks:    map[string]bool{},
		blockedAt: map[string]time.Time{},
	}
}

func reportKey(reporter, target uuid.UUID, targetType string) string {
	return reporter.String() + "|" + targetType + "|" + target.String()
}

func blockKey(blocker, blocked uuid.UUID) string { return blocker.String() + "|" + blocked.String() }

func (f *fakeQuerier) LockReporter(_ context.Context, _ uuid.UUID) error { return nil }

func (f *fakeQuerier) ReportExists(_ context.Context, arg CreateReportParams) (bool, error) {
	return f.seen[reportKey(arg.ReporterID, arg.TargetID, arg.TargetType)], nil
}

func (f *fakeQuerier) CreateReport(_ context.Context, arg CreateReportParams) (uuid.UUID, error) {
	key := reportKey(arg.ReporterID, arg.TargetID, arg.TargetType)
	if f.seen[key] {
		return uuid.Nil, pgx.ErrNoRows
	}
	f.seen[key] = true
	id := uuid.New()
	f.reports[id] = &ReportRow{
		ID: id, TargetType: arg.TargetType, TargetID: arg.TargetID, Reason: arg.Reason,
		Detail: arg.Detail, Status: "pending",
		ReporterID: arg.ReporterID, ReporterName: "reporter", ReporterHandle: "reporter",
		CreatedAt: time.Now(),
	}
	return id, nil
}

func (f *fakeQuerier) GetReportByID(_ context.Context, id uuid.UUID) (ReportRow, error) {
	row, ok := f.reports[id]
	if !ok {
		return ReportRow{}, pgx.ErrNoRows
	}
	return *row, nil
}

func (f *fakeQuerier) ListReports(_ context.Context, arg ListReportsParams) ([]ReportRow, error) {
	rows := make([]ReportRow, 0, len(f.reports))
	for _, row := range f.reports {
		if arg.Status != nil && row.Status != *arg.Status {
			continue
		}
		rows = append(rows, *row)
	}
	if len(rows) > int(arg.Limit) {
		rows = rows[:arg.Limit]
	}
	return rows, nil
}

func (f *fakeQuerier) UpdateReportStatus(_ context.Context, arg UpdateReportStatusParams) (int64, error) {
	row, ok := f.reports[arg.ID]
	if !ok {
		return 0, nil
	}
	row.Status = arg.Status
	row.HandlerID = uuidParam(arg.HandledBy)
	row.HandledAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	return 1, nil
}

func (f *fakeQuerier) IsAdmin(_ context.Context, userID uuid.UUID) (bool, error) {
	return f.admins[userID], nil
}

func (f *fakeQuerier) GetUserIDByHandle(_ context.Context, handle string) (uuid.UUID, error) {
	id, ok := f.users[handle]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	return id, nil
}

func (f *fakeQuerier) GetTargetAuthor(_ context.Context, arg TargetAuthorParams) (uuid.UUID, error) {
	author, ok := f.authors[arg.TargetID]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	return author, nil
}

func (f *fakeQuerier) CreateBlock(_ context.Context, arg BlockParams) (int64, error) {
	key := blockKey(arg.BlockerID, arg.BlockedID)
	if f.blocks[key] {
		return 0, nil
	}
	f.blocks[key] = true
	f.blockedAt[key] = time.Now()
	return 1, nil
}

func (f *fakeQuerier) DeleteBlock(_ context.Context, arg BlockParams) (int64, error) {
	key := blockKey(arg.BlockerID, arg.BlockedID)
	if !f.blocks[key] {
		return 0, nil
	}
	delete(f.blocks, key)
	return 1, nil
}

func (f *fakeQuerier) ListBlocks(_ context.Context, arg ListBlocksParams) ([]BlockedUserRow, error) {
	rows := make([]BlockedUserRow, 0, len(f.blocks))
	for key, ok := range f.blocks {
		if !ok {
			continue
		}
		rows = append(rows, BlockedUserRow{
			UserID: uuid.MustParse(key[len(key)-36:]), Name: "blocked", Handle: "blocked",
			CreatedAt: f.blockedAt[key],
		})
	}
	return rows, nil
}

func (f *fakeQuerier) IsBlocked(_ context.Context, arg BlockParams) (bool, error) {
	return f.blocks[blockKey(arg.BlockerID, arg.BlockedID)], nil
}

func TestCreateReportValidatesAndDedupes(t *testing.T) {
	fake := newFakeQuerier()
	service := newServiceWithQuerier(fake)
	reporter := uuid.New()
	target := uuid.New()
	fake.authors[target] = uuid.New()

	ctx := context.Background()

	// 目标类型非法 → ErrValidation。
	if _, err := service.CreateReport(ctx, reporter, CreateReportInput{
		TargetType: "nope", TargetID: target, Reason: "spam",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法 targetType 期望 ErrValidation，得到 %v", err)
	}

	// 原因非法 → ErrValidation。
	if _, err := service.CreateReport(ctx, reporter, CreateReportInput{
		TargetType: "post", TargetID: target, Reason: "nope",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法 reason 期望 ErrValidation，得到 %v", err)
	}

	// 目标不存在 → ErrValidation（不能进入审核队列）。
	if _, err := service.CreateReport(ctx, reporter, CreateReportInput{
		TargetType: "post", TargetID: uuid.New(), Reason: "spam",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("目标不存在期望 ErrValidation，得到 %v", err)
	}

	// 首次成功。
	created, err := service.CreateReport(ctx, reporter, CreateReportInput{
		TargetType: "post", TargetID: target, Reason: "spam", Detail: "广告",
	})
	if err != nil {
		t.Fatalf("首次举报失败: %v", err)
	}
	if created.Status != "pending" || created.Detail != "广告" {
		t.Fatalf("举报字段不正确: %+v", created)
	}

	// 重复 → ErrAlreadyReported（409）。
	if _, err := service.CreateReport(ctx, reporter, CreateReportInput{
		TargetType: "post", TargetID: target, Reason: "spam",
	}); !errors.Is(err, ErrAlreadyReported) {
		t.Fatalf("重复举报期望 ErrAlreadyReported，得到 %v", err)
	}
}

func TestCreateReportRejectsSelf(t *testing.T) {
	fake := newFakeQuerier()
	service := newServiceWithQuerier(fake)
	reporter := uuid.New()
	target := uuid.New()
	fake.authors[target] = reporter

	if _, err := service.CreateReport(context.Background(), reporter, CreateReportInput{
		TargetType: "post", TargetID: target, Reason: "other",
	}); !errors.Is(err, ErrSelfReport) {
		t.Fatalf("自举报期望 ErrSelfReport，得到 %v", err)
	}
}

func TestListReportsRequiresAdmin(t *testing.T) {
	fake := newFakeQuerier()
	service := newServiceWithQuerier(fake)
	user := uuid.New()

	if _, err := service.ListReports(context.Background(), user, "", "", 20); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非管理员期望 ErrForbidden，得到 %v", err)
	}

	fake.admins[user] = true
	if _, err := service.ListReports(context.Background(), user, "bogus", "", 20); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法 status 期望 ErrValidation，得到 %v", err)
	}
	if _, err := service.ListReports(context.Background(), user, "pending", "", 20); err != nil {
		t.Fatalf("管理员查询失败: %v", err)
	}
}

func TestUpdateReportStatus(t *testing.T) {
	fake := newFakeQuerier()
	service := newServiceWithQuerier(fake)
	admin := uuid.New()
	reporter := uuid.New()
	target := uuid.New()
	fake.admins[admin] = true
	fake.authors[target] = uuid.New()

	created, err := service.CreateReport(context.Background(), reporter, CreateReportInput{
		TargetType: "user", TargetID: target, Reason: "harassment",
	})
	if err != nil {
		t.Fatalf("准备举报失败: %v", err)
	}

	// pending 不能由运营手动设回。
	if _, err := service.UpdateReportStatus(context.Background(), admin, created.ID, "pending"); !errors.Is(err, ErrValidation) {
		t.Fatalf("设回 pending 期望 ErrValidation，得到 %v", err)
	}

	updated, err := service.UpdateReportStatus(context.Background(), admin, created.ID, "resolved")
	if err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}
	if updated.Status != "resolved" || updated.HandledBy == nil || updated.HandledAt == nil {
		t.Fatalf("处理信息未写入: %+v", updated)
	}

	if _, err := service.UpdateReportStatus(context.Background(), admin, uuid.New(), "rejected"); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("不存在的举报期望 ErrReportNotFound，得到 %v", err)
	}
}

func TestBlockLifecycle(t *testing.T) {
	fake := newFakeQuerier()
	service := newServiceWithQuerier(fake)
	actor := uuid.New()
	target := uuid.New()
	fake.users["target"] = target

	ctx := context.Background()

	// 不能屏蔽自己。
	fake.users["me"] = actor
	if err := service.Block(ctx, actor, "me"); !errors.Is(err, ErrValidation) {
		t.Fatalf("屏蔽自己期望 ErrValidation，得到 %v", err)
	}

	// 句柄不存在。
	if err := service.Block(ctx, actor, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("不存在用户期望 ErrUserNotFound，得到 %v", err)
	}

	if err := service.Block(ctx, actor, "target"); err != nil {
		t.Fatalf("屏蔽失败: %v", err)
	}
	// 重复屏蔽。
	if err := service.Block(ctx, actor, "target"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("重复屏蔽期望 ErrBlocked，得到 %v", err)
	}

	blocked, err := service.Blocked(ctx, actor, "target")
	if err != nil || !blocked {
		t.Fatalf("屏蔽状态应为 true，得到 %v / %v", blocked, err)
	}

	result, err := service.ListBlocks(ctx, actor, "", 20)
	if err != nil {
		t.Fatalf("查询屏蔽列表失败: %v", err)
	}
	if len(result.Blocked) != 1 || result.Blocked[0].ID != target {
		t.Fatalf("屏蔽列表不正确: %+v", result.Blocked)
	}

	if err := service.Unblock(ctx, actor, "target"); err != nil {
		t.Fatalf("取消屏蔽失败: %v", err)
	}
	// 未屏蔽时取消。
	if err := service.Unblock(ctx, actor, "target"); !errors.Is(err, ErrNotBlocked) {
		t.Fatalf("取消未屏蔽用户期望 ErrNotBlocked，得到 %v", err)
	}
}

func TestCursorRejectsWrongStream(t *testing.T) {
	id := uuid.New()
	encoded, err := encodeCursor("reports", time.Now(), id)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if _, _, err := decodeCursor(encoded, "blocks"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("跨列表游标期望 ErrInvalidCursor，得到 %v", err)
	}
	if _, got, err := decodeCursor(encoded, "reports"); err != nil || got != id {
		t.Fatalf("正确游标解码失败: %v / %v", got, err)
	}
}
