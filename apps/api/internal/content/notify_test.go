package content

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// fakeHandleResolver 用固定映射替代数据库解析，便于验证提及通知的收件人逻辑。
type fakeHandleResolver struct {
	byHandle map[string]uuid.UUID
	calls    [][]string
}

func (f *fakeHandleResolver) ResolveHandles(_ context.Context, handles []string) (map[string]uuid.UUID, error) {
	f.calls = append(f.calls, handles)
	result := make(map[string]uuid.UUID, len(handles))
	for _, handle := range handles {
		if id, ok := f.byHandle[handle]; ok {
			result[handle] = id
		}
	}
	return result, nil
}

func TestNotifyMentionsSkipsSelfAndResolvesBatch(t *testing.T) {
	notifier := &fakeNotifier{}
	author := uuid.New()
	alice := uuid.New()
	bob := uuid.New()
	resolver := &fakeHandleResolver{byHandle: map[string]uuid.UUID{
		"alice": alice,
		"bob":   bob,
		"me":    author,
	}}
	service := &Service{notifier: notifier, handles: resolver}
	postID := uuid.New()

	service.notifyMentions(context.Background(), author, "post", postID, "@alice @bob @me @nobody", nil)

	// 只应发两条：alice 与 bob；自己与未解析的 handle 都被跳过。
	if len(notifier.inputs) != 2 {
		t.Fatalf("提及通知数 = %d，期望 2", len(notifier.inputs))
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("handle 应批量解析一次，实际调用 %d 次", len(resolver.calls))
	}
	seen := map[uuid.UUID]bool{}
	for _, input := range notifier.inputs {
		if input.Type != "mention" || input.TargetType != "post" || input.TargetID != postID {
			t.Fatalf("提及通知字段不正确: %+v", input)
		}
		seen[input.UserID] = true
	}
	if !seen[alice] || !seen[bob] {
		t.Fatalf("收件人应包含 alice 与 bob: %v", seen)
	}
	if seen[author] {
		t.Fatal("作者不应收到自己的提及通知")
	}
}

func TestNotifyMentionsNoopWithoutResolver(t *testing.T) {
	notifier := &fakeNotifier{}
	service := &Service{notifier: notifier}
	service.notifyMentions(context.Background(), uuid.New(), "post", uuid.New(), "@alice", nil)
	if len(notifier.inputs) != 0 {
		t.Fatal("未注入解析器时不应生成通知")
	}
}

func TestNotifyRepostSkipsSelf(t *testing.T) {
	notifier := &fakeNotifier{}
	service := &Service{notifier: notifier}
	author := uuid.New()
	original := uuid.New()
	repostID := uuid.New()

	// 自己转发自己的帖子不通知。
	service.notifyRepost(context.Background(), author, author, repostID, original)
	if len(notifier.inputs) != 0 {
		t.Fatal("自转发不应生成通知")
	}

	actor := uuid.New()
	service.notifyRepost(context.Background(), author, actor, repostID, original)
	if len(notifier.inputs) != 1 {
		t.Fatalf("转发通知数 = %d，期望 1", len(notifier.inputs))
	}
	got := notifier.inputs[0]
	if got.UserID != author || got.ActorID != actor || got.Type != "repost" || got.TargetID != original {
		t.Fatalf("转发通知字段不正确: %+v", got)
	}
}
