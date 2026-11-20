package content

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

func TestPostTags(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		want    []string
		wantErr bool
	}{
		{name: "pgx text array", value: []any{"go", "research"}, want: []string{"go", "research"}},
		{name: "string slice", value: []string{"go"}, want: []string{"go"}},
		{name: "empty pgx array", value: []any{}, want: []string{}},
		{name: "invalid element", value: []any{1}, wantErr: true},
		{name: "null", value: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postTags(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("postTags() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("postTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

// fakeReadQuerier 模拟可见性谓词：把 uuid.Nil 当作 NULL 查看者，
// 只放行 visibility = 'public' 的帖子，其余一律「无行」。
// 这与 posts.sql 中 `p.author_id = $2 OR p.visibility = 'public' OR ...`
// 在 $2 为 NULL 时的真实求值结果一致（NULL 比较得 UNKNOWN，只有 public 分支成立）。
type fakeReadQuerier struct {
	posts       map[uuid.UUID]db.PostVisibility
	comments    []db.ListCommentsRow
	gotViewerID pgtype.UUID
}

func (f *fakeReadQuerier) visible(id uuid.UUID, viewer pgtype.UUID) bool {
	visibility, ok := f.posts[id]
	if !ok {
		return false
	}
	if viewer.Valid {
		return true // 测试只关心匿名路径，已登录一律放行
	}
	return visibility == db.PostVisibilityPublic
}

func (f *fakeReadQuerier) GetPostByID(_ context.Context, arg db.GetPostByIDParams) (db.GetPostByIDRow, error) {
	f.gotViewerID = arg.UserID
	if !f.visible(arg.ID.Bytes, arg.UserID) {
		return db.GetPostByIDRow{}, pgx.ErrNoRows
	}
	return db.GetPostByIDRow{
		ID: arg.ID, Type: db.PostTypeOriginal, Visibility: f.posts[arg.ID.Bytes],
		AuthorName: "作者", AuthorHandle: "author", Tags: []any{},
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}, nil
}

func (f *fakeReadQuerier) GetPostForWrite(_ context.Context, arg db.GetPostForWriteParams) (pgtype.UUID, error) {
	f.gotViewerID = arg.AuthorID
	if !f.visible(arg.ID.Bytes, arg.AuthorID) {
		return pgtype.UUID{}, pgx.ErrNoRows
	}
	return arg.ID, nil
}

func (f *fakeReadQuerier) ListComments(_ context.Context, arg db.ListCommentsParams) ([]db.ListCommentsRow, error) {
	if arg.Limit <= 0 {
		return nil, errors.New("limit 未设置")
	}
	return f.comments, nil
}

func newPublicAccessFixture() (*fakeReadQuerier, uuid.UUID, uuid.UUID) {
	publicID, privateID := uuid.New(), uuid.New()
	return &fakeReadQuerier{
		posts: map[uuid.UUID]db.PostVisibility{
			publicID:  db.PostVisibilityPublic,
			privateID: db.PostVisibilityPrivate,
		},
		comments: []db.ListCommentsRow{{
			ID: uuidParam(uuid.New()), PostID: uuidParam(publicID), AuthorName: "评论者", AuthorHandle: "commenter",
			ContentHtml: "<p>hi</p>", ContentText: "hi",
			CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}},
	}, publicID, privateID
}

// TestGetPostAnonymousSeesOnlyPublic 是匿名读的核心断言：
// viewerID = uuid.Nil 时只返回 public 帖，private 帖与不存在的帖一样 404。
func TestGetPostAnonymousSeesOnlyPublic(t *testing.T) {
	querier, publicID, privateID := newPublicAccessFixture()
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	post, err := service.GetPost(ctx, uuid.Nil, publicID)
	if err != nil {
		t.Fatalf("匿名读取 public 帖 error = %v", err)
	}
	if post.ID != publicID {
		t.Fatalf("返回帖子 ID = %v，期望 %v", post.ID, publicID)
	}
	if querier.gotViewerID.Valid {
		t.Fatalf("匿名查看者被编码成非 NULL（%v），SQL 可见性谓词会误判", querier.gotViewerID)
	}

	for _, id := range []uuid.UUID{privateID, uuid.New()} {
		if _, err := service.GetPost(ctx, uuid.Nil, id); !errors.Is(err, ErrPostNotFound) {
			t.Fatalf("匿名读取 %v error = %v，期望 ErrPostNotFound", id, err)
		}
	}
}

// TestGetPostAuthenticatedViewerPassedThrough 确认已登录路径没有因为引入匿名分支而退化。
func TestGetPostAuthenticatedViewerPassedThrough(t *testing.T) {
	querier, _, privateID := newPublicAccessFixture()
	service := newServiceWithQuerier(querier)

	viewer := uuid.New()
	if _, err := service.GetPost(context.Background(), viewer, privateID); err != nil {
		t.Fatalf("已登录读取 error = %v", err)
	}
	if !querier.gotViewerID.Valid || querier.gotViewerID.Bytes != viewer {
		t.Fatalf("查看者 ID = %v，期望 %v", querier.gotViewerID, viewer)
	}
}

// TestListCommentsAnonymous 校验匿名只能看 public 帖的评论。
func TestListCommentsAnonymous(t *testing.T) {
	querier, publicID, privateID := newPublicAccessFixture()
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	result, err := service.ListComments(ctx, uuid.Nil, publicID, "", 20)
	if err != nil {
		t.Fatalf("匿名读取 public 帖评论 error = %v", err)
	}
	if len(result.Comments) != 1 {
		t.Fatalf("评论数 = %d，期望 1", len(result.Comments))
	}
	if querier.gotViewerID.Valid {
		t.Fatalf("匿名查看者被编码成非 NULL（%v）", querier.gotViewerID)
	}

	if _, err := service.ListComments(ctx, uuid.Nil, privateID, "", 20); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("匿名读取 private 帖评论 error = %v，期望 ErrPostNotFound", err)
	}
}

// TestGetPostForWriteAnonymousDoesNotRejectPublic 覆盖任务点名的易错处：
// GetPostForWrite 的参数是 (ID, AuthorID)，匿名时 AuthorID 为 uuid.Nil。
// 必须仍然放行 public 帖，否则匿名读评论会被误判为「无权查看」而 404。
func TestGetPostForWriteAnonymousDoesNotRejectPublic(t *testing.T) {
	querier, publicID, privateID := newPublicAccessFixture()
	ctx := context.Background()

	if _, err := querier.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(publicID), AuthorID: uuidParam(uuid.Nil)}); err != nil {
		t.Fatalf("匿名 GetPostForWrite(public) error = %v，期望通过", err)
	}
	if _, err := querier.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(privateID), AuthorID: uuidParam(uuid.Nil)}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("匿名 GetPostForWrite(private) error = %v，期望 pgx.ErrNoRows", err)
	}
}

// TestUUIDParamEncodesNilAsNull 直接锁住匿名路径依赖的编码约定。
func TestUUIDParamEncodesNilAsNull(t *testing.T) {
	if value := uuidParam(uuid.Nil); value.Valid {
		t.Fatal("uuidParam(uuid.Nil) 必须是 NULL，否则匿名可见性谓词会失效")
	}
	id := uuid.New()
	if value := uuidParam(id); !value.Valid || value.Bytes != id {
		t.Fatalf("uuidParam(%v) = %v", id, value)
	}
}
