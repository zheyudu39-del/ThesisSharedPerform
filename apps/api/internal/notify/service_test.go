package notify

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// fakeQuerier 是通知服务的内存替身。
// 它刻意复刻 SQL 里的关键语义（静音过滤、游标比较、like_agg 窗口），
// 这样服务层的行为可以在没有数据库的单元测试里被完整验证。
type fakeQuerier struct {
	notifications []*db.Notification
	prefs         map[uuid.UUID]*db.NotificationPref
	users         map[uuid.UUID]struct{ name, handle string }
	now           time.Time
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		prefs: make(map[uuid.UUID]*db.NotificationPref),
		users: make(map[uuid.UUID]struct{ name, handle string }),
		now:   time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}

func (f *fakeQuerier) addUser(name, handle string) uuid.UUID {
	id := uuid.New()
	f.users[id] = struct{ name, handle string }{name, handle}
	return id
}

// muted 返回该用户静音的类型集合，未设置偏好时为空。
func (f *fakeQuerier) muted(userID uuid.UUID) map[db.NotifyType]struct{} {
	result := make(map[db.NotifyType]struct{})
	pref, ok := f.prefs[userID]
	if !ok {
		return result
	}
	for _, t := range pref.MutedTypes {
		result[t] = struct{}{}
	}
	return result
}

func (f *fakeQuerier) CreateNotification(_ context.Context, arg db.CreateNotificationParams) (db.Notification, error) {
	record := &db.Notification{
		ID:         uuidParam(uuid.New()),
		UserID:     arg.UserID,
		Type:       arg.Type,
		ActorID:    arg.ActorID,
		TargetType: arg.TargetType,
		TargetID:   arg.TargetID,
		Payload:    arg.Payload,
		CreatedAt:  pgtype.Timestamptz{Time: f.now, Valid: true},
	}
	f.notifications = append(f.notifications, record)
	return *record, nil
}

func (f *fakeQuerier) ListNotifications(_ context.Context, arg db.ListNotificationsParams) ([]db.ListNotificationsRow, error) {
	muted := f.muted(arg.UserID.Bytes)
	candidates := make([]*db.Notification, 0, len(f.notifications))
	for _, record := range f.notifications {
		if record.UserID.Bytes != arg.UserID.Bytes {
			continue
		}
		if _, isMuted := muted[record.Type]; isMuted {
			continue
		}
		if arg.Column2.Valid && !lessThan(record, arg.Column2.Time, arg.Column3.Bytes) {
			continue
		}
		candidates = append(candidates, record)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Time.Equal(candidates[j].CreatedAt.Time) {
			return uuid.UUID(candidates[i].ID.Bytes).String() > uuid.UUID(candidates[j].ID.Bytes).String()
		}
		return candidates[i].CreatedAt.Time.After(candidates[j].CreatedAt.Time)
	})
	if len(candidates) > int(arg.Limit) {
		candidates = candidates[:arg.Limit]
	}
	rows := make([]db.ListNotificationsRow, 0, len(candidates))
	for _, record := range candidates {
		row := db.ListNotificationsRow{
			ID: record.ID, UserID: record.UserID, Type: record.Type, ActorID: record.ActorID,
			TargetType: record.TargetType, TargetID: record.TargetID, Payload: record.Payload,
			ReadAt: record.ReadAt, CreatedAt: record.CreatedAt,
		}
		if record.ActorID.Valid {
			if user, ok := f.users[record.ActorID.Bytes]; ok {
				name, handle := user.name, user.handle
				row.ActorName, row.ActorHandle = &name, &handle
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// lessThan 复刻 SQL 的 (created_at, id) < (cursor) 字典序比较。
func lessThan(record *db.Notification, cursorTime time.Time, cursorID uuid.UUID) bool {
	if !record.CreatedAt.Time.Equal(cursorTime) {
		return record.CreatedAt.Time.Before(cursorTime)
	}
	return uuid.UUID(record.ID.Bytes).String() < cursorID.String()
}

func (f *fakeQuerier) CountUnread(_ context.Context, userID pgtype.UUID) (int64, error) {
	muted := f.muted(userID.Bytes)
	var count int64
	for _, record := range f.notifications {
		if record.UserID.Bytes != userID.Bytes || record.ReadAt.Valid {
			continue
		}
		if _, isMuted := muted[record.Type]; isMuted {
			continue
		}
		count++
	}
	return count, nil
}

func (f *fakeQuerier) MarkRead(_ context.Context, arg db.MarkReadParams) (int64, error) {
	for _, record := range f.notifications {
		if record.ID.Bytes != arg.ID.Bytes || record.UserID.Bytes != arg.UserID.Bytes {
			continue
		}
		if !record.ReadAt.Valid {
			record.ReadAt = pgtype.Timestamptz{Time: f.now, Valid: true}
		}
		return 1, nil
	}
	return 0, nil
}

func (f *fakeQuerier) MarkAllRead(_ context.Context, userID pgtype.UUID) (int64, error) {
	var count int64
	for _, record := range f.notifications {
		if record.UserID.Bytes != userID.Bytes || record.ReadAt.Valid {
			continue
		}
		record.ReadAt = pgtype.Timestamptz{Time: f.now, Valid: true}
		count++
	}
	return count, nil
}

func (f *fakeQuerier) GetNotificationPrefs(_ context.Context, userID pgtype.UUID) (db.NotificationPref, error) {
	pref, ok := f.prefs[userID.Bytes]
	if !ok {
		return db.NotificationPref{}, pgx.ErrNoRows
	}
	return *pref, nil
}

func (f *fakeQuerier) UpsertNotificationPrefs(_ context.Context, arg db.UpsertNotificationPrefsParams) (db.NotificationPref, error) {
	pref := &db.NotificationPref{UserID: arg.UserID, MutedTypes: arg.MutedTypes, EmailDigest: arg.EmailDigest}
	f.prefs[arg.UserID.Bytes] = pref
	return *pref, nil
}

func (f *fakeQuerier) GetUnreadLikeAggNotification(_ context.Context, arg db.GetUnreadLikeAggNotificationParams) (db.Notification, error) {
	for _, record := range f.notifications {
		if record.UserID.Bytes != arg.UserID.Bytes || record.Type != db.NotifyTypeLikeAgg {
			continue
		}
		if record.ReadAt.Valid || record.TargetID.Bytes != arg.TargetID.Bytes {
			continue
		}
		if record.TargetType == nil || arg.TargetType == nil || *record.TargetType != *arg.TargetType {
			continue
		}
		if record.CreatedAt.Time.Before(arg.CreatedAt.Time) {
			continue
		}
		return *record, nil
	}
	return db.Notification{}, pgx.ErrNoRows
}

func (f *fakeQuerier) UpdateLikeAggNotification(_ context.Context, arg db.UpdateLikeAggNotificationParams) (db.Notification, error) {
	for _, record := range f.notifications {
		if record.ID.Bytes != arg.ID.Bytes || record.UserID.Bytes != arg.UserID.Bytes {
			continue
		}
		record.Payload = arg.Payload
		record.ActorID = arg.ActorID
		record.CreatedAt = pgtype.Timestamptz{Time: f.now, Valid: true}
		return *record, nil
	}
	return db.Notification{}, pgx.ErrNoRows
}

func newTestService(fake *fakeQuerier) *Service {
	svc := newServiceWithQuerier(fake)
	svc.now = func() time.Time { return fake.now }
	return svc
}

// ---------------------------------------------------------------------------
// 自己给自己不发通知
// ---------------------------------------------------------------------------

func TestNotifySkipsSelfAction(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	userID := fake.addUser("张三", "zhangsan")

	err := svc.Notify(context.Background(), CreateInput{
		UserID: userID, Type: "comment", ActorID: userID, TargetType: "post", TargetID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("自己的动作应静默返回 nil，得到 %v", err)
	}
	if len(fake.notifications) != 0 {
		t.Fatalf("自己的动作不应落库，实际写入 %d 条", len(fake.notifications))
	}
}

func TestNotifyCreatesForOtherActor(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("李四", "lisi")
	actor := fake.addUser("王五", "wangwu")
	target := uuid.New()

	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "comment", ActorID: actor, TargetType: "post", TargetID: target,
		Payload: map[string]any{"excerpt": "写得好"},
	}); err != nil {
		t.Fatalf("Notify 失败: %v", err)
	}
	if len(fake.notifications) != 1 {
		t.Fatalf("应写入 1 条通知，实际 %d", len(fake.notifications))
	}
	record := fake.notifications[0]
	if record.UserID.Bytes != owner || record.ActorID.Bytes != actor {
		t.Fatalf("通知收发双方不正确: %+v", record)
	}
	var payload map[string]any
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v", err)
	}
	if payload["excerpt"] != "写得好" {
		t.Fatalf("payload 未保留业务字段: %v", payload)
	}
}

func TestNotifyRejectsInvalidType(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("李四", "lisi")
	actor := fake.addUser("王五", "wangwu")

	err := svc.Notify(context.Background(), CreateInput{UserID: owner, Type: "bogus", ActorID: actor})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("非法类型应返回 ErrValidation，得到 %v", err)
	}
	if len(fake.notifications) != 0 {
		t.Fatal("非法类型不应落库")
	}
}

// ---------------------------------------------------------------------------
// 游标
// ---------------------------------------------------------------------------

func TestCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)
	id := uuid.New()

	encoded, err := encodeCursor("notifications", createdAt, id)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	decodedTime, decodedID, err := decodeCursor(encoded, "notifications")
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if !decodedTime.Equal(createdAt) {
		t.Fatalf("时间不匹配: %v != %v", decodedTime, createdAt)
	}
	if decodedID != id {
		t.Fatalf("ID 不匹配: %v != %v", decodedID, id)
	}
}

func TestCursorRejectsWrongStreamAndGarbage(t *testing.T) {
	encoded, err := encodeCursor("notifications", time.Now().UTC(), uuid.New())
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if _, _, err := decodeCursor(encoded, "other"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("跨列表游标应被拒绝，得到 %v", err)
	}
	if _, _, err := decodeCursor("not-base64!!", "notifications"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("损坏游标应被拒绝，得到 %v", err)
	}
	// 空串表示第一页。
	if createdAt, id, err := decodeCursor("", "notifications"); err != nil || !createdAt.IsZero() || id != uuid.Nil {
		t.Fatalf("空游标应返回零值且无错误，得到 %v %v %v", createdAt, id, err)
	}
}

// ---------------------------------------------------------------------------
// 静音过滤
// ---------------------------------------------------------------------------

func TestListExcludesMutedTypes(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("赵六", "zhaoliu")
	actor := fake.addUser("钱七", "qianqi")
	target := uuid.New()

	// 先写入三类通知，时间依次递增，保证顺序可预测。
	for i, kind := range []string{"comment", "like_agg", "system"} {
		fake.now = fake.now.Add(time.Minute)
		if err := svc.Notify(context.Background(), CreateInput{
			UserID: owner, Type: kind, ActorID: actor, TargetType: "post", TargetID: target,
		}); err != nil {
			t.Fatalf("写入 %s 通知失败: %v", kind, err)
		}
		_ = i
	}

	// 静音 like_agg。
	if err := svc.UpdatePrefs(context.Background(), owner, []string{"like_agg"}, ""); err != nil {
		t.Fatalf("更新偏好失败: %v", err)
	}

	result, err := svc.List(context.Background(), owner, "", 20)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(result.Notifications) != 2 {
		t.Fatalf("静音后应只剩 2 条，实际 %d", len(result.Notifications))
	}
	for _, item := range result.Notifications {
		if item.Type == "like_agg" {
			t.Fatal("静音类型不应出现在列表中")
		}
	}

	// 未读数口径必须一致：剩下 2 条都未读。
	count, err := svc.UnreadCount(context.Background(), owner)
	if err != nil {
		t.Fatalf("UnreadCount 失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("未读数应与列表口径一致，期望 2，实际 %d", count)
	}
}

func TestListPagination(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("孙八", "sunba")
	actor := fake.addUser("周九", "zhoujiu")
	target := uuid.New()

	for i := 0; i < 5; i++ {
		fake.now = fake.now.Add(time.Minute)
		if err := svc.Notify(context.Background(), CreateInput{
			UserID: owner, Type: "comment", ActorID: actor, TargetType: "post", TargetID: target,
		}); err != nil {
			t.Fatalf("写入通知失败: %v", err)
		}
	}

	first, err := svc.List(context.Background(), owner, "", 2)
	if err != nil {
		t.Fatalf("第一页失败: %v", err)
	}
	if len(first.Notifications) != 2 || first.NextCursor == nil {
		t.Fatalf("第一页应有 2 条且带游标，实际 %d / %v", len(first.Notifications), first.NextCursor)
	}
	second, err := svc.List(context.Background(), owner, *first.NextCursor, 2)
	if err != nil {
		t.Fatalf("第二页失败: %v", err)
	}
	if len(second.Notifications) != 2 {
		t.Fatalf("第二页应有 2 条，实际 %d", len(second.Notifications))
	}
	for _, a := range first.Notifications {
		for _, b := range second.Notifications {
			if a.ID == b.ID {
				t.Fatalf("翻页出现重复通知 %v", a.ID)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 点赞聚合
// ---------------------------------------------------------------------------

func TestNotifyLikeAggMergesWithinWindow(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("吴十", "wushi")
	first := fake.addUser("郑一", "zhengyi")
	second := fake.addUser("王二", "wanger")
	target := uuid.New()

	for _, actor := range []uuid.UUID{first, second} {
		if err := svc.Notify(context.Background(), CreateInput{
			UserID: owner, Type: "like_agg", ActorID: actor, TargetType: "post", TargetID: target,
		}); err != nil {
			t.Fatalf("Notify like_agg 失败: %v", err)
		}
		fake.now = fake.now.Add(time.Minute)
	}

	if len(fake.notifications) != 1 {
		t.Fatalf("窗口内两次点赞应合并为 1 条，实际 %d 条", len(fake.notifications))
	}
	payload := decodePayload(fake.notifications[0].Payload)
	if got := payloadInt(payload["count"]); got != 2 {
		t.Fatalf("聚合计数应为 2，实际 %d", got)
	}
	actors := payloadActors(payload["actors"])
	if len(actors) != 2 || actors[0] != second.String() || actors[1] != first.String() {
		t.Fatalf("actors 应最近者在前: %v", actors)
	}
}

func TestNotifyLikeAggCreatesNewOutsideWindow(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("吴十", "wushi")
	first := fake.addUser("郑一", "zhengyi")
	second := fake.addUser("王二", "wanger")
	target := uuid.New()

	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "like_agg", ActorID: first, TargetType: "post", TargetID: target,
	}); err != nil {
		t.Fatalf("第一次点赞失败: %v", err)
	}
	// 超过 10 分钟窗口。
	fake.now = fake.now.Add(likeAggWindow + time.Minute)
	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "like_agg", ActorID: second, TargetType: "post", TargetID: target,
	}); err != nil {
		t.Fatalf("第二次点赞失败: %v", err)
	}

	if len(fake.notifications) != 2 {
		t.Fatalf("窗口外应新建通知，期望 2 条，实际 %d", len(fake.notifications))
	}
}

func TestNotifyLikeAggDoesNotMergeReadNotification(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("吴十", "wushi")
	first := fake.addUser("郑一", "zhengyi")
	second := fake.addUser("王二", "wanger")
	target := uuid.New()

	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "like_agg", ActorID: first, TargetType: "post", TargetID: target,
	}); err != nil {
		t.Fatalf("第一次点赞失败: %v", err)
	}
	if err := svc.MarkRead(context.Background(), owner, fake.notifications[0].ID.Bytes); err != nil {
		t.Fatalf("标记已读失败: %v", err)
	}
	fake.now = fake.now.Add(time.Minute)
	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "like_agg", ActorID: second, TargetType: "post", TargetID: target,
	}); err != nil {
		t.Fatalf("第二次点赞失败: %v", err)
	}

	// 已读通知不应被重新激活，应生成新的一条。
	if len(fake.notifications) != 2 {
		t.Fatalf("已读通知不应参与聚合，期望 2 条，实际 %d", len(fake.notifications))
	}
}

// ---------------------------------------------------------------------------
// 已读与偏好
// ---------------------------------------------------------------------------

func TestMarkReadRejectsOtherUsersNotification(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("陈三", "chensan")
	other := fake.addUser("林四", "linsi")
	actor := fake.addUser("何五", "hew")

	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "comment", ActorID: actor, TargetType: "post", TargetID: uuid.New(),
	}); err != nil {
		t.Fatalf("写入通知失败: %v", err)
	}
	err := svc.MarkRead(context.Background(), other, fake.notifications[0].ID.Bytes)
	if !errors.Is(err, ErrNotificationNotFound) {
		t.Fatalf("越权标记应返回 ErrNotificationNotFound，得到 %v", err)
	}
	if fake.notifications[0].ReadAt.Valid {
		t.Fatal("越权调用不应改变通知状态")
	}
}

func TestGetPrefsDefaults(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	userID := fake.addUser("许六", "xuliu")

	prefs, err := svc.GetPrefs(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPrefs 失败: %v", err)
	}
	if prefs.EmailDigest != "daily" || len(prefs.MutedTypes) != 0 {
		t.Fatalf("默认偏好不正确: %+v", prefs)
	}
}

func TestUpdatePrefsValidates(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	userID := fake.addUser("许六", "xuliu")

	if err := svc.UpdatePrefs(context.Background(), userID, []string{"bogus"}, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法静音类型应返回 ErrValidation，得到 %v", err)
	}
	if err := svc.UpdatePrefs(context.Background(), userID, nil, "hourly"); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法邮件频率应返回 ErrValidation，得到 %v", err)
	}

	// 合法更新并去重。
	if err := svc.UpdatePrefs(context.Background(), userID, []string{"comment", "comment", "mention"}, "weekly"); err != nil {
		t.Fatalf("更新偏好失败: %v", err)
	}
	prefs, err := svc.GetPrefs(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPrefs 失败: %v", err)
	}
	if len(prefs.MutedTypes) != 2 || prefs.EmailDigest != "weekly" {
		t.Fatalf("偏好未正确保存: %+v", prefs)
	}

	// 传空切片清空静音，空串保持邮件频率不变。
	if err := svc.UpdatePrefs(context.Background(), userID, []string{}, ""); err != nil {
		t.Fatalf("清空静音失败: %v", err)
	}
	prefs, err = svc.GetPrefs(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPrefs 失败: %v", err)
	}
	if len(prefs.MutedTypes) != 0 || prefs.EmailDigest != "weekly" {
		t.Fatalf("清空静音后偏好不正确: %+v", prefs)
	}
}

func TestMarkAllReadClearsUnread(t *testing.T) {
	fake := newFakeQuerier()
	svc := newTestService(fake)
	owner := fake.addUser("邓七", "dengqi")
	actor := fake.addUser("冯八", "fengba")
	target := uuid.New()

	for i := 0; i < 3; i++ {
		fake.now = fake.now.Add(time.Minute)
		if err := svc.Notify(context.Background(), CreateInput{
			UserID: owner, Type: "comment", ActorID: actor, TargetType: "post", TargetID: target,
		}); err != nil {
			t.Fatalf("写入通知失败: %v", err)
		}
	}
	if err := svc.MarkAllRead(context.Background(), owner); err != nil {
		t.Fatalf("MarkAllRead 失败: %v", err)
	}
	count, err := svc.UnreadCount(context.Background(), owner)
	if err != nil {
		t.Fatalf("UnreadCount 失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("全部已读后未读数应为 0，实际 %d", count)
	}
}
