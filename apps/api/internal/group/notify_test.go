package group

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeNotifier 记录群组服务发出的通知。
type fakeNotifier struct{ inputs []NotifyInput }

func (f *fakeNotifier) Notify(_ context.Context, input NotifyInput) error {
	f.inputs = append(f.inputs, input)
	return nil
}

// fakeManagerQuerier 返回固定的管理员集合。
type fakeManagerQuerier struct {
	ids []uuid.UUID
}

func (f *fakeManagerQuerier) ListGroupManagerIDs(_ context.Context, _ pgtype.UUID) ([]pgtype.UUID, error) {
	result := make([]pgtype.UUID, 0, len(f.ids))
	for _, id := range f.ids {
		result = append(result, uuidParam(id))
	}
	return result, nil
}

// TestNotifyJoinRequestReachesManagersAndOwner 校验入群申请通知发给群主与所有管理员，
// 且排除申请人自己。
func TestNotifyJoinRequestReachesManagersAndOwner(t *testing.T) {
	owner := uuid.New()
	admin := uuid.New()
	applicant := uuid.New()
	groupID := uuid.New()
	requestID := uuid.New()

	notifier := &fakeNotifier{}
	service := &Service{
		notifier: notifier,
		managers: &fakeManagerQuerier{ids: []uuid.UUID{owner, admin}},
	}
	service.notifyJoinRequest(context.Background(), groupID, owner, applicant, requestID, "ml-group")

	if len(notifier.inputs) != 2 {
		t.Fatalf("通知数 = %d，期望 2（群主 + 管理员）", len(notifier.inputs))
	}
	seen := map[uuid.UUID]bool{}
	for _, input := range notifier.inputs {
		if input.Type != "join_request" || input.TargetType != "group" || input.TargetID != groupID {
			t.Fatalf("通知字段不正确: %+v", input)
		}
		if input.ActorID != applicant {
			t.Fatalf("触发者应为申请人，实际 %v", input.ActorID)
		}
		if input.Payload["slug"] != "ml-group" {
			t.Fatalf("payload 应带 slug 供前端拼路由: %+v", input.Payload)
		}
		seen[input.UserID] = true
	}
	if !seen[owner] || !seen[admin] {
		t.Fatalf("收件人应包含群主与管理员: %v", seen)
	}
	if seen[applicant] {
		t.Fatal("申请人不应收到自己的入群申请通知")
	}
}

// TestNotifyJoinRequestSkipsWhenNotWired 确认未注入依赖时不 panic、不生成通知。
func TestNotifyJoinRequestSkipsWhenNotWired(t *testing.T) {
	service := &Service{}
	service.notifyJoinRequest(context.Background(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "x")
}
