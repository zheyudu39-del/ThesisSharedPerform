package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// profileQuerier 是只实现资料相关方法的替身；未实现的方法会 panic，
// 保证测试不会意外走到注册登录路径。
type profileQuerier struct {
	users     map[string]db.GetPublicUserByHandleRow
	follows   map[[2]uuid.UUID]bool
	postCount int64
}

func newProfileQuerier() *profileQuerier {
	return &profileQuerier{users: map[string]db.GetPublicUserByHandleRow{}, follows: map[[2]uuid.UUID]bool{}}
}

func (p *profileQuerier) add(name, handle string) db.GetPublicUserByHandleRow {
	row := db.GetPublicUserByHandleRow{
		ID:             pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name:           name,
		Handle:         handle,
		ResearchFields: []string{},
		CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	p.users[handle] = row
	return row
}

func (p *profileQuerier) GetPublicUserByHandle(_ context.Context, handle string) (db.GetPublicUserByHandleRow, error) {
	row, ok := p.users[handle]
	if !ok {
		return db.GetPublicUserByHandleRow{}, pgx.ErrNoRows
	}
	return row, nil
}

func (p *profileQuerier) UpdateUserProfile(_ context.Context, params db.UpdateUserProfileParams) (db.User, error) {
	return db.User{ID: params.ID, Name: "更新后的名字"}, nil
}

func (p *profileQuerier) FollowUser(_ context.Context, params db.FollowUserParams) (int64, error) {
	key := [2]uuid.UUID{params.FollowerID.Bytes, params.FolloweeID.Bytes}
	if p.follows[key] {
		return 0, nil // 幂等：已存在时不新增
	}
	p.follows[key] = true
	return 1, nil
}

func (p *profileQuerier) UnfollowUser(_ context.Context, params db.UnfollowUserParams) (int64, error) {
	key := [2]uuid.UUID{params.FollowerID.Bytes, params.FolloweeID.Bytes}
	if !p.follows[key] {
		return 0, nil
	}
	delete(p.follows, key)
	return 1, nil
}

func (p *profileQuerier) CountFollowers(_ context.Context, followeeID pgtype.UUID) (int64, error) {
	var count int64
	for key := range p.follows {
		if key[1] == followeeID.Bytes {
			count++
		}
	}
	return count, nil
}

func (p *profileQuerier) CountFollowing(_ context.Context, followerID pgtype.UUID) (int64, error) {
	var count int64
	for key := range p.follows {
		if key[0] == followerID.Bytes {
			count++
		}
	}
	return count, nil
}

func (p *profileQuerier) IsFollowing(_ context.Context, params db.IsFollowingParams) (bool, error) {
	return p.follows[[2]uuid.UUID{params.FollowerID.Bytes, params.FolloweeID.Bytes}], nil
}

func (p *profileQuerier) CountPostsByAuthor(_ context.Context, _ pgtype.UUID) (int64, error) {
	return p.postCount, nil
}

// newProfileOnlyService 只装配资料相关依赖，避免为注册登录实现无关替身。
func newProfileOnlyService(querier ProfileQuerier) *Service {
	return &Service{profile: querier, tokens: NewTokenManager("test-secret", time.Hour)}
}

func TestFollowIsIdempotent(t *testing.T) {
	querier := newProfileQuerier()
	target := querier.add("目标用户", "target")
	service := newProfileOnlyService(querier)
	follower := uuid.New()

	for i := 0; i < 3; i++ {
		following, count, err := service.Follow(context.Background(), follower, "target")
		if err != nil {
			t.Fatal(err)
		}
		if !following || count != 1 {
			t.Fatalf("第 %d 次关注: following=%v count=%d，期望 true/1", i+1, following, count)
		}
	}
	// 计数必须等于真实关注者数，而不是写入次数。
	if got := len(querier.follows); got != 1 {
		t.Fatalf("关注关系条数 = %d，期望 1", got)
	}

	for i := 0; i < 3; i++ {
		following, count, err := service.Unfollow(context.Background(), follower, "target")
		if err != nil {
			t.Fatal(err)
		}
		if following || count != 0 {
			t.Fatalf("第 %d 次取关: following=%v count=%d，期望 false/0", i+1, following, count)
		}
	}
	if _, _, err := service.Follow(context.Background(), follower, "target"); err != nil {
		t.Fatal(err)
	}
	_ = target
}

func TestFollowRejectsSelfAndMissing(t *testing.T) {
	querier := newProfileQuerier()
	self := querier.add("我自己", "myself")
	service := newProfileOnlyService(querier)

	if _, _, err := service.Follow(context.Background(), self.ID.Bytes, "myself"); !errors.Is(err, ErrSelfFollow) {
		t.Fatalf("关注自己 error = %v", err)
	}
	if _, _, err := service.Follow(context.Background(), uuid.New(), "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("关注不存在用户 error = %v", err)
	}
	if _, _, err := service.GetPublic(context.Background(), "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("查询不存在用户 error = %v", err)
	}
}

func TestGetPublicStats(t *testing.T) {
	querier := newProfileQuerier()
	target := querier.add("目标用户", "target")
	querier.postCount = 7
	service := newProfileOnlyService(querier)

	viewer := uuid.New()
	if _, _, err := service.Follow(context.Background(), viewer, "target"); err != nil {
		t.Fatal(err)
	}
	user, stats, err := service.GetPublic(context.Background(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != target.ID.Bytes || user.Handle != "target" {
		t.Fatalf("公开资料 = %+v", user)
	}
	if stats.PostCount != 7 || stats.FollowerCount != 1 || stats.FollowingCount != 0 {
		t.Fatalf("统计 = %+v", stats)
	}
}

func TestGetPublicViewFollowing(t *testing.T) {
	querier := newProfileQuerier()
	target := querier.add("目标用户", "target")
	service := newProfileOnlyService(querier)
	viewer := uuid.New()

	// 匿名：不查询关注状态，following 恒为 false。
	_, _, following, err := service.GetPublicView(context.Background(), uuid.Nil, "target")
	if err != nil {
		t.Fatal(err)
	}
	if following {
		t.Fatal("匿名访问 following 应为 false")
	}

	// 已关注：viewer 关注 target 后应为 true。
	if _, _, err := service.Follow(context.Background(), viewer, "target"); err != nil {
		t.Fatal(err)
	}
	_, _, following, err = service.GetPublicView(context.Background(), viewer, "target")
	if err != nil {
		t.Fatal(err)
	}
	if !following {
		t.Fatal("已关注用户 following 应为 true")
	}

	// 查看自己：不应把自己算作「已关注自己」。
	_, _, following, err = service.GetPublicView(context.Background(), target.ID.Bytes, "target")
	if err != nil {
		t.Fatal(err)
	}
	if following {
		t.Fatal("查看自己 following 应为 false")
	}

	// 未关注者：false。
	_, _, following, err = service.GetPublicView(context.Background(), uuid.New(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if following {
		t.Fatal("未关注者 following 应为 false")
	}
}

func TestUpdateProfileValidation(t *testing.T) {
	longBio := strings.Repeat("字", 281)
	longField := strings.Repeat("领", 65)
	tooMany := make([]string, 11)
	for i := range tooMany {
		tooMany[i] = "方向"
	}
	empty := ""
	tooLongName := strings.Repeat("名", 65)

	tests := []struct {
		name    string
		input   UpdateProfileInput
		wantErr bool
	}{
		{name: "全部省略", input: UpdateProfileInput{}},
		{name: "空名字", input: UpdateProfileInput{Name: &empty}, wantErr: true},
		{name: "名字过长", input: UpdateProfileInput{Name: &tooLongName}, wantErr: true},
		{name: "简介过长", input: UpdateProfileInput{Bio: &longBio}, wantErr: true},
		{name: "单方向过长", input: UpdateProfileInput{ResearchFields: &[]string{longField}}, wantErr: true},
		{name: "方向过多", input: UpdateProfileInput{ResearchFields: &tooMany}, wantErr: true},
		{name: "清空简介", input: UpdateProfileInput{Bio: &empty}},
		{name: "清空方向", input: UpdateProfileInput{ResearchFields: &[]string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildUpdateProfileParams(uuid.New(), tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ValidationError) {
				t.Fatalf("error = %v，期望包装 ValidationError", err)
			}
		})
	}
}

// 局部更新必须把「未提供」映射为 nil 参数（COALESCE 保持原值），
// 把「显式清空」映射为空切片（写入 {}）。
func TestUpdateProfilePartialFields(t *testing.T) {
	empty := ""
	fields := []string{}
	params, err := buildUpdateProfileParams(uuid.New(), UpdateProfileInput{Bio: &empty, ResearchFields: &fields})
	if err != nil {
		t.Fatal(err)
	}
	if params.Name != nil || params.Institution != nil {
		t.Fatalf("未提供的字段应保持 nil: %+v", params)
	}
	if params.Bio == nil || *params.Bio != "" {
		t.Fatalf("简介应为显式空字符串: %v", params.Bio)
	}
	if params.ResearchFields == nil || len(params.ResearchFields) != 0 {
		t.Fatalf("研究方向应为空切片而非 nil: %#v", params.ResearchFields)
	}
}

func TestUpdateProfileDeduplicatesFields(t *testing.T) {
	fields := []string{" 机器学习 ", "机器学习", "系统"}
	params, err := buildUpdateProfileParams(uuid.New(), UpdateProfileInput{ResearchFields: &fields})
	if err != nil {
		t.Fatal(err)
	}
	if len(params.ResearchFields) != 2 || params.ResearchFields[0] != "机器学习" {
		t.Fatalf("去重后 = %#v", params.ResearchFields)
	}
}
