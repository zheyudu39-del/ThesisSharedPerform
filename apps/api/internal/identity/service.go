package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

var handlePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// User 是账号接口对外暴露的用户资料，不包含密码摘要。
type User struct {
	ID             uuid.UUID `json:"id"`
	Email          string    `json:"email"`
	Name           string    `json:"name"`
	Handle         string    `json:"handle"`
	Bio            string    `json:"bio"`
	Institution    string    `json:"institution"`
	ResearchFields []string  `json:"researchFields"`
	VerifyType     string    `json:"verifyType"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"createdAt"`
}

type RegisterInput struct {
	Email    string
	Password string
	Name     string
	Handle   string
}

type LoginInput struct {
	Email    string
	Password string
}

// Querier 限定账号服务使用的生成查询，便于单元测试替身。
type Querier interface {
	CreateUser(context.Context, db.CreateUserParams) (db.User, error)
	GetUserByEmail(context.Context, *string) (db.User, error)
	GetUserByHandle(context.Context, string) (db.User, error)
	GetUserByID(context.Context, pgtype.UUID) (db.User, error)
}

// ProfileQuerier 是个人主页、关注关系与资料编辑所需的查询集合。
// 与 Querier 分开，使只测注册登录的替身不必实现这些方法。
type ProfileQuerier interface {
	GetPublicUserByHandle(context.Context, string) (db.GetPublicUserByHandleRow, error)
	UpdateUserProfile(context.Context, db.UpdateUserProfileParams) (db.User, error)
	FollowUser(context.Context, db.FollowUserParams) (int64, error)
	UnfollowUser(context.Context, db.UnfollowUserParams) (int64, error)
	CountFollowers(context.Context, pgtype.UUID) (int64, error)
	CountFollowing(context.Context, pgtype.UUID) (int64, error)
	IsFollowing(context.Context, db.IsFollowingParams) (bool, error)
	CountPostsByAuthor(context.Context, pgtype.UUID) (int64, error)
}

// Service 处理注册、登录和用户资料查询。
// Indexer 是检索索引的抽象，由 server 组装时注入（与 group / content 一致）。
//
// 为什么不在这里直接 import search：identity 是最底层的包，
// 加一个反向依赖会让 search 与 identity 的导入关系变得难以推理。
type Indexer interface {
	Index(ctx context.Context, doc SearchDocument) error
}

// SearchDocument 与 search.IndexInput 字段一一对应，单独声明以免反向依赖。
type SearchDocument struct {
	EntityType string
	EntityID   uuid.UUID
	Title      string
	Body       string
}

type Service struct {
	querier Querier
	profile ProfileQuerier
	indexer Indexer
	pool    *pgxpool.Pool
	tokens  *TokenManager
}

// SetIndexer 注入检索索引实现；不注入时索引被静默跳过。
func (s *Service) SetIndexer(indexer Indexer) { s.indexer = indexer }

// indexUser 把用户写进检索索引（PRD S-01 要求「论文、帖子、用户、群组」可搜）。
//
// 标题存显示名、正文句柄、机构与研究方向：用户搜「张三」按名字命中，
// 搜「zhangsan」按句柄命中，搜「清华大学」也能找到该机构的人。
//
// 失败只记日志：注册或改资料已经成功，索引没写上不该让整个请求失败
// ——用户下次改资料或登录时还会再写一次。
func (s *Service) indexUser(ctx context.Context, user User) {
	if s.indexer == nil || user.ID == uuid.Nil {
		return
	}
	body := strings.TrimSpace(strings.Join([]string{user.Handle, user.Institution, strings.Join(user.ResearchFields, " ")}, " "))
	if err := s.indexer.Index(ctx, SearchDocument{
		EntityType: "user",
		EntityID:   user.ID,
		Title:      user.Name,
		Body:       body,
	}); err != nil {
		slog.Error("索引用户失败", "userId", user.ID, "err", err)
	}
}

func NewService(pool *pgxpool.Pool, tokens *TokenManager) *Service {
	q := db.New(pool)
	return &Service{querier: q, profile: q, pool: pool, tokens: tokens}
}

func newService(querier Querier, tokens *TokenManager) *Service {
	service := &Service{querier: querier, tokens: tokens}
	if profile, ok := querier.(ProfileQuerier); ok {
		service.profile = profile
	}
	return service
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (User, string, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	input.Handle = strings.TrimSpace(input.Handle)
	if err := validateRegister(input); err != nil {
		return User{}, "", err
	}
	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return User{}, "", err
	}

	querier := s.querier
	var tx pgx.Tx
	if s.pool != nil {
		tx, err = s.pool.Begin(ctx)
		if err != nil {
			return User{}, "", fmt.Errorf("开启注册事务失败: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		querier = db.New(tx)
	}
	if _, err := querier.GetUserByEmail(ctx, &input.Email); err == nil {
		return User{}, "", EmailTaken
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", fmt.Errorf("查询邮箱失败: %w", err)
	}
	if _, err := querier.GetUserByHandle(ctx, input.Handle); err == nil {
		return User{}, "", HandleTaken
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", fmt.Errorf("查询句柄失败: %w", err)
	}

	created, err := querier.CreateUser(ctx, db.CreateUserParams{
		Email: &input.Email, PasswordHash: &passwordHash, Name: input.Name, Handle: input.Handle,
	})
	if err != nil {
		return User{}, "", classifyUniqueViolation(err)
	}
	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			return User{}, "", fmt.Errorf("提交注册事务失败: %w", err)
		}
	}
	user := userFromDB(created)
	s.indexUser(ctx, user)
	token, err := s.tokens.Issue(user.ID)
	if err != nil {
		return User{}, "", fmt.Errorf("签发访问令牌失败: %w", err)
	}
	return user, token, nil
}

// dummyPasswordHash 用于「用户不存在」分支，保证与正常校验耗时一致，
// 避免通过响应时间差异枚举已注册账号。它在 init 时一次性生成。
var dummyPasswordHash string

func init() {
	h, err := HashPassword("dummy-password-for-timing-equalization")
	if err != nil {
		// 仅在 crypto/rand 不可用时发生；此时无法安全地做恒定耗时处理。
		panic("identity: 无法生成占位密码摘要: " + err.Error())
	}
	dummyPasswordHash = h
}

func (s *Service) Login(ctx context.Context, input LoginInput) (User, string, error) {
	email := strings.TrimSpace(input.Email)
	record, err := s.querier.GetUserByEmail(ctx, &email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 用户不存在时仍执行一次等价的 Argon2 计算，抹平时间差异。
			_, _ = VerifyPassword(dummyPasswordHash, input.Password)
			return User{}, "", InvalidCredentials
		}
		return User{}, "", fmt.Errorf("查询登录用户失败: %w", err)
	}
	if record.PasswordHash == nil || record.DeletedAt.Valid || record.Status != "active" {
		// 账号不可用同样做一次等价计算，防止据此判断账号状态。
		_, _ = VerifyPassword(dummyPasswordHash, input.Password)
		return User{}, "", InvalidCredentials
	}
	valid, err := VerifyPassword(*record.PasswordHash, input.Password)
	if err != nil || !valid {
		return User{}, "", InvalidCredentials
	}
	user := userFromDB(record)
	token, err := s.tokens.Issue(user.ID)
	if err != nil {
		return User{}, "", fmt.Errorf("签发访问令牌失败: %w", err)
	}
	return user, token, nil
}

func (s *Service) GetByID(ctx context.Context, userID uuid.UUID) (User, error) {
	record, err := s.querier.GetUserByID(ctx, pgtype.UUID{Bytes: userID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, InvalidToken
		}
		return User{}, fmt.Errorf("查询用户失败: %w", err)
	}
	// 令牌签发后账号可能被封禁或注销；此处拒绝，使失效立即生效，
	// 而不必等 JWT 自然过期。
	if record.DeletedAt.Valid || record.Status != "active" {
		return User{}, InvalidToken
	}
	return userFromDB(record), nil
}

func validateRegister(input RegisterInput) error {
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || !strings.Contains(input.Email, "@") {
		return fmt.Errorf("%w: 邮箱格式不正确", ValidationError)
	}
	if len(input.Password) < 8 {
		return fmt.Errorf("%w: 密码至少 8 位", ValidationError)
	}
	if len(input.Password) > 128 {
		return fmt.Errorf("%w: 密码不能超过 128 位", ValidationError)
	}
	if n := utf8.RuneCountInString(input.Name); n < 1 || n > 64 {
		return fmt.Errorf("%w: 显示名长度须为 1 到 64 个字符", ValidationError)
	}
	if n := utf8.RuneCountInString(input.Handle); n < 3 || n > 32 || !handlePattern.MatchString(input.Handle) {
		return fmt.Errorf("%w: 句柄长度须为 3 到 32 个字符且只能包含字母、数字、下划线和连字符", ValidationError)
	}
	return nil
}

func classifyUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "users_handle_key":
			return HandleTaken
		case "users_email_key":
			return EmailTaken
		}
	}
	return fmt.Errorf("创建用户失败: %w", err)
}

// PublicUser 是个人主页对外暴露的资料，只含公开字段。
// 刻意不包含 email、role、status 等私密字段，即使调用方拿到 JSON 也无法还原账号信息。
type PublicUser struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Handle         string    `json:"handle"`
	Bio            string    `json:"bio"`
	Institution    string    `json:"institution"`
	ResearchFields []string  `json:"researchFields"`
	VerifyType     string    `json:"verifyType"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Stats 是个人主页的统计数字，全部来自实时 COUNT。
type Stats struct {
	PostCount      int64 `json:"postCount"`
	FollowerCount  int64 `json:"followerCount"`
	FollowingCount int64 `json:"followingCount"`
}

// UpdateProfileInput 是局部更新入参：字段为 nil 表示请求体未提供该字段，保持不变。
// ResearchFields 用指针区分「未提供」与「清空为 []」。
type UpdateProfileInput struct {
	Name           *string
	Bio            *string
	Institution    *string
	ResearchFields *[]string
}

// Follow 关注 handle 对应的用户，返回关注后的状态与该用户的粉丝数。
// 重复关注幂等：写入用 ON CONFLICT DO NOTHING，计数始终由表内 COUNT(*) 得出。
func (s *Service) Follow(ctx context.Context, followerID uuid.UUID, handle string) (bool, int64, error) {
	return s.setFollow(ctx, followerID, handle, true)
}

// Unfollow 取消关注，语义与 Follow 对称且幂等。
func (s *Service) Unfollow(ctx context.Context, followerID uuid.UUID, handle string) (bool, int64, error) {
	return s.setFollow(ctx, followerID, handle, false)
}

func (s *Service) setFollow(ctx context.Context, followerID uuid.UUID, handle string, follow bool) (bool, int64, error) {
	if s.profile == nil {
		return false, 0, fmt.Errorf("账号服务未配置资料查询")
	}
	target, err := s.profile.GetPublicUserByHandle(ctx, strings.TrimSpace(handle))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, ErrUserNotFound
	}
	if err != nil {
		return false, 0, fmt.Errorf("查询被关注用户失败: %w", err)
	}
	if target.ID.Bytes == followerID {
		return false, 0, ErrSelfFollow
	}

	var count int64
	// 写入与计数放在同一事务：计数读到的必然包含本次写入，
	// 不会因为并发请求而在写入后读到旧值。
	run := func(q ProfileQuerier) error {
		if follow {
			if _, err := q.FollowUser(ctx, db.FollowUserParams{FollowerID: uuidParam(followerID), FolloweeID: target.ID}); err != nil {
				return err
			}
		} else {
			if _, err := q.UnfollowUser(ctx, db.UnfollowUserParams{FollowerID: uuidParam(followerID), FolloweeID: target.ID}); err != nil {
				return err
			}
		}
		var err error
		count, err = q.CountFollowers(ctx, target.ID)
		return err
	}
	if err := s.withProfileTx(ctx, run); err != nil {
		return false, 0, fmt.Errorf("更新关注关系失败: %w", err)
	}
	return follow, count, nil
}

// GetPublic 返回公开资料与统计。不存在的用户与已停用用户都返回 ErrUserNotFound。
func (s *Service) GetPublic(ctx context.Context, handle string) (PublicUser, Stats, error) {
	user, stats, _, err := s.GetPublicView(ctx, uuid.Nil, handle)
	return user, stats, err
}

// GetPublicView 与 GetPublic 相同，但额外返回 viewerID 是否已关注此人。
// viewerID 为零值表示匿名访问，此时 following 恒为 false。
//
// 关注状态只用于公开页面的按钮展示，不影响可见性，因此无需鉴权即可查询；
// 但只有调用方确认令牌有效时才会传入 viewerID。
func (s *Service) GetPublicView(ctx context.Context, viewerID uuid.UUID, handle string) (PublicUser, Stats, bool, error) {
	if s.profile == nil {
		return PublicUser{}, Stats{}, false, fmt.Errorf("账号服务未配置资料查询")
	}
	row, err := s.profile.GetPublicUserByHandle(ctx, strings.TrimSpace(handle))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicUser{}, Stats{}, false, ErrUserNotFound
	}
	if err != nil {
		return PublicUser{}, Stats{}, false, fmt.Errorf("查询用户公开资料失败: %w", err)
	}
	postCount, err := s.profile.CountPostsByAuthor(ctx, row.ID)
	if err != nil {
		return PublicUser{}, Stats{}, false, fmt.Errorf("统计用户帖子数失败: %w", err)
	}
	followerCount, err := s.profile.CountFollowers(ctx, row.ID)
	if err != nil {
		return PublicUser{}, Stats{}, false, fmt.Errorf("统计粉丝数失败: %w", err)
	}
	followingCount, err := s.profile.CountFollowing(ctx, row.ID)
	if err != nil {
		return PublicUser{}, Stats{}, false, fmt.Errorf("统计关注数失败: %w", err)
	}
	following := false
	if viewerID != uuid.Nil && viewerID != row.ID.Bytes {
		following, err = s.profile.IsFollowing(ctx, db.IsFollowingParams{
			FollowerID: uuidParam(viewerID), FolloweeID: row.ID,
		})
		if err != nil {
			return PublicUser{}, Stats{}, false, fmt.Errorf("查询关注状态失败: %w", err)
		}
	}
	return PublicUser{
			ID:             row.ID.Bytes,
			Name:           row.Name,
			Handle:         row.Handle,
			Bio:            textValue(row.Bio),
			Institution:    textValue(row.Institution),
			ResearchFields: nonNil(row.ResearchFields),
			VerifyType:     string(row.VerifyType),
			CreatedAt:      row.CreatedAt.Time.UTC(),
		}, Stats{
			PostCount:      postCount,
			FollowerCount:  followerCount,
			FollowingCount: followingCount,
		}, following, nil
}

// UpdateProfile 局部更新本人资料，只写入请求中出现的字段。
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (User, error) {
	params, err := buildUpdateProfileParams(userID, input)
	if err != nil {
		return User{}, err
	}
	if s.profile == nil {
		return User{}, fmt.Errorf("账号服务未配置资料查询")
	}
	record, err := s.profile.UpdateUserProfile(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("更新用户资料失败: %w", err)
	}
	user := userFromDB(record)
	// 改了名字 / 机构 / 研究方向之后索引要跟着变，否则搜索里还是旧资料。
	s.indexUser(ctx, user)
	return user, nil
}

// buildUpdateProfileParams 做校验并组装参数。
// 单独抽出便于在没有数据库的单元测试里覆盖校验规则。
func buildUpdateProfileParams(userID uuid.UUID, input UpdateProfileInput) (db.UpdateUserProfileParams, error) {
	params := db.UpdateUserProfileParams{ID: uuidParam(userID)}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
			return db.UpdateUserProfileParams{}, fmt.Errorf("%w: 显示名长度须为 1 到 64 个字符", ValidationError)
		}
		params.Name = &name
	}
	if input.Bio != nil {
		bio := strings.TrimSpace(*input.Bio)
		if utf8.RuneCountInString(bio) > 280 {
			return db.UpdateUserProfileParams{}, fmt.Errorf("%w: 简介不能超过 280 个字符", ValidationError)
		}
		params.Bio = &bio
	}
	if input.Institution != nil {
		institution := strings.TrimSpace(*input.Institution)
		if utf8.RuneCountInString(institution) > 128 {
			return db.UpdateUserProfileParams{}, fmt.Errorf("%w: 机构名称不能超过 128 个字符", ValidationError)
		}
		params.Institution = &institution
	}
	if input.ResearchFields != nil {
		// 先按请求原样校验条数：契约是「最多 10 项」，不能靠去重把超限请求悄悄放过。
		if len(*input.ResearchFields) > 10 {
			return db.UpdateUserProfileParams{}, fmt.Errorf("%w: 研究方向最多 10 项", ValidationError)
		}
		fields := make([]string, 0, len(*input.ResearchFields))
		seen := make(map[string]struct{}, len(*input.ResearchFields))
		for _, raw := range *input.ResearchFields {
			field := strings.TrimSpace(raw)
			if n := utf8.RuneCountInString(field); n < 1 || n > 64 {
				return db.UpdateUserProfileParams{}, fmt.Errorf("%w: 研究方向每项须为 1 到 64 个字符", ValidationError)
			}
			if _, exists := seen[field]; exists {
				continue
			}
			seen[field] = struct{}{}
			fields = append(fields, field)
		}
		// 空切片而非 nil：nil 在 SQL 里是 NULL，会被 COALESCE 当作「不修改」。
		params.ResearchFields = fields
	}
	return params, nil
}

// withProfileTx 在事务中执行资料写入；没有连接池（单元测试替身）时直接执行。
func (s *Service) withProfileTx(ctx context.Context, fn func(ProfileQuerier) error) error {
	if s.pool == nil {
		return fn(s.profile)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(db.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

func uuidParam(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

func userFromDB(record db.User) User {
	return User{
		ID:             record.ID.Bytes,
		Email:          textValue(record.Email),
		Name:           record.Name,
		Handle:         record.Handle,
		Bio:            textValue(record.Bio),
		Institution:    textValue(record.Institution),
		ResearchFields: nonNil(record.ResearchFields),
		VerifyType:     string(record.VerifyType),
		Role:           string(record.Role),
		CreatedAt:      record.CreatedAt.Time.UTC(),
	}
}

func textValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	case pgtype.Text:
		return v.String
	}
	return ""
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
