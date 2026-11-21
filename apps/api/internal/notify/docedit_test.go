package notify

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// fakeDocEditQuerier 复刻 doc_edit 聚合的 SQL 语义：
// 只命中同用户、同目标、未读且在窗口内的通知。
// 它直接读写宿主 fakeQuerier 的通知切片，模拟两者共用同一张表。
type fakeDocEditQuerier struct {
	host *fakeQuerier
	now  time.Time
}

func (f *fakeDocEditQuerier) GetUnreadDocEditNotification(_ context.Context, arg db.GetUnreadDocEditNotificationParams) (db.Notification, error) {
	for _, record := range f.host.notifications {
		if record.UserID.Bytes != arg.UserID.Bytes || record.Type != db.NotifyTypeDocEdit {
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

func (f *fakeDocEditQuerier) UpdateDocEditNotification(_ context.Context, arg db.UpdateDocEditNotificationParams) (db.Notification, error) {
	for _, record := range f.host.notifications {
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

func newDocEditTestService() (*Service, *fakeQuerier, *fakeDocEditQuerier) {
	fake := newFakeQuerier()
	docEdits := &fakeDocEditQuerier{host: fake, now: fake.now}
	svc := newServiceWithQuerier(fake)
	svc.docEdits = docEdits
	// 让服务层的窗口计算与替身时钟同步，否则 5 分钟窗口按真实时间算会永远命中。
	svc.now = func() time.Time { return fake.now }
	return svc, fake, docEdits
}

// TestNotifyDocEditAggregatesWithinWindow 校验同一文档 5 分钟内的多次编辑合并为一条。
func TestNotifyDocEditAggregatesWithinWindow(t *testing.T) {
	svc, fake, docEdits := newDocEditTestService()
	owner := fake.addUser("作者", "author")
	first := fake.addUser("编辑甲", "editor_a")
	second := fake.addUser("编辑乙", "editor_b")
	docID := uuid.New()

	for _, actor := range []uuid.UUID{first, second} {
		if err := svc.Notify(context.Background(), CreateInput{
			UserID: owner, Type: "doc_edit", ActorID: actor, TargetType: "document", TargetID: docID,
		}); err != nil {
			t.Fatalf("Notify doc_edit 失败: %v", err)
		}
		fake.now = fake.now.Add(time.Minute)
		docEdits.now = fake.now
	}

	if len(fake.notifications) != 1 {
		t.Fatalf("窗口内两次编辑应合并为 1 条，实际 %d 条", len(fake.notifications))
	}
	payload := decodePayload(fake.notifications[0].Payload)
	if got := payloadInt(payload["count"]); got != 2 {
		t.Fatalf("聚合计数应为 2，实际 %d", got)
	}
}

// TestNotifyDocEditNewOutsideWindow 校验超过 5 分钟窗口后生成新通知。
func TestNotifyDocEditNewOutsideWindow(t *testing.T) {
	svc, fake, docEdits := newDocEditTestService()
	owner := fake.addUser("作者", "author")
	first := fake.addUser("编辑甲", "editor_a")
	second := fake.addUser("编辑乙", "editor_b")
	docID := uuid.New()

	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "doc_edit", ActorID: first, TargetType: "document", TargetID: docID,
	}); err != nil {
		t.Fatalf("第一次编辑失败: %v", err)
	}
	fake.now = fake.now.Add(docEditWindow + time.Minute)
	docEdits.now = fake.now
	if err := svc.Notify(context.Background(), CreateInput{
		UserID: owner, Type: "doc_edit", ActorID: second, TargetType: "document", TargetID: docID,
	}); err != nil {
		t.Fatalf("第二次编辑失败: %v", err)
	}

	if len(fake.notifications) != 2 {
		t.Fatalf("窗口外应新建通知，期望 2 条，实际 %d", len(fake.notifications))
	}
}
