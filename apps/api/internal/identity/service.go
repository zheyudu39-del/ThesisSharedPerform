package identity

import (
	"context"
	"errors"
	"fmt"
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

// Service 处理注册、登录和用户资料查询。
type Service struct {
	querier Querier
	pool    *pgxpool.Pool
	tokens  *TokenManager
}

func NewService(pool *pgxpool.Pool, tokens *TokenManager) *Service {
	return &Service{querier: db.New(pool), pool: pool, tokens: tokens}
}

func newService(querier Querier, tokens *TokenManager) *Service {
	return &Service{querier: querier, tokens: tokens}
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
