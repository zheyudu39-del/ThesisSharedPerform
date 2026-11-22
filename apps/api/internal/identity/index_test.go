package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// recordingIndexer 记录 Index 收到的文档，并可以强制返回错误。
type recordingIndexer struct {
	docs []SearchDocument
	err  error
}

func (r *recordingIndexer) Index(_ context.Context, doc SearchDocument) error {
	r.docs = append(r.docs, doc)
	return r.err
}

// TestIndexUserIncludesSearchableFields 验证用户索引覆盖了搜索要用到的三类文本。
//
// PRD S-01 要求用户可被搜索。名字进标题（权重高），句柄、机构与研究方向
// 进正文——用户搜「张三」「zhangsan」「清华大学」都应命中。
func TestIndexUserIncludesSearchableFields(t *testing.T) {
	idx := &recordingIndexer{}
	svc := newService(&fakeQuerier{}, nil)
	svc.SetIndexer(idx)

	user := User{
		ID:             uuid.Must(uuid.NewV7()),
		Name:           "张三",
		Handle:         "zhangsan",
		Institution:    "清华大学",
		ResearchFields: []string{"机器学习", "自然语言处理"},
	}
	svc.indexUser(context.Background(), user)

	if len(idx.docs) != 1 {
		t.Fatalf("应写入 1 条索引，实际 %d", len(idx.docs))
	}
	doc := idx.docs[0]
	if doc.EntityType != "user" {
		t.Fatalf("entityType = %q, want user", doc.EntityType)
	}
	if doc.EntityID != user.ID {
		t.Fatalf("entityID 不一致")
	}
	if doc.Title != "张三" {
		t.Fatalf("标题应为显示名，实际 %q", doc.Title)
	}
	for _, want := range []string{"zhangsan", "清华大学", "机器学习", "自然语言处理"} {
		if !strings.Contains(doc.Body, want) {
			t.Fatalf("正文缺少 %q：%q", want, doc.Body)
		}
	}
}

// TestIndexUserSkipsWhenNotConfigured 验证未注入 indexer 时不 panic。
// 多数单元测试与「只读部署」都不会注入索引。
func TestIndexUserSkipsWhenNotConfigured(t *testing.T) {
	svc := newService(&fakeQuerier{}, nil)
	svc.indexUser(context.Background(), User{ID: uuid.Must(uuid.NewV7()), Name: "x"})
}

// TestIndexUserIgnoresErrors 验证索引失败不影响主流程。
// 注册已经成功，索引没写上不该让整个请求失败。
func TestIndexUserIgnoresErrors(t *testing.T) {
	idx := &recordingIndexer{err: errors.New("search down")}
	svc := newService(&fakeQuerier{}, nil)
	svc.SetIndexer(idx)

	// 不 panic、不返回错误即为通过。
	svc.indexUser(context.Background(), User{
		ID: uuid.Must(uuid.NewV7()), Name: "李四", Handle: "lisi",
	})
	if len(idx.docs) != 1 {
		t.Fatal("索引调用应当发生")
	}
}

// TestIndexUserSkipsZeroID 验证没有 ID 的用户不会被写入索引。
func TestIndexUserSkipsZeroID(t *testing.T) {
	idx := &recordingIndexer{}
	svc := newService(&fakeQuerier{}, nil)
	svc.SetIndexer(idx)
	svc.indexUser(context.Background(), User{Name: "无 ID"})
	if len(idx.docs) != 0 {
		t.Fatalf("零值 ID 不应写入索引，实际 %d 条", len(idx.docs))
	}
}
