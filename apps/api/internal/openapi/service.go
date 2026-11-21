package openapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

const (
	// keyPrefix "tsk_" 标识这是本平台的 API Key。
	// 前缀让泄露扫描工具（如 GitHub secret scanning）能识别并告警。
	keyPrefix = "tsk_"
	// keyRandomBytes 是随机部分的字节数。32 字节 = 256 位熵，
	// 暴力枚举在物理上不可行，因此存储端只需 SHA-256 而无需慢哈希。
	keyRandomBytes = 32
	// keyDisplayLen 是展示用前缀长度（含 "tsk_"）。
	keyDisplayLen = 12
	// maxKeyNameLen 与 api_keys.name 的 varchar(64) 一致。
	maxKeyNameLen = 64
)

// Key 是密钥元数据 DTO。永远不含明文与哈希。
type Key struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// CreatedKey 是创建密钥的响应：明文只在这里出现一次。
type CreatedKey struct {
	Key Key `json:"key"`
	// Plaintext 只在本响应里返回。服务端不保存，丢失只能重建。
	Plaintext string `json:"plaintext"`
}

// CreateKeyInput 是创建密钥入参。
type CreateKeyInput struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

// Querier 限定服务使用的生成查询，便于单元测试注入替身。
type Querier interface {
	CreateAPIKey(context.Context, db.CreateAPIKeyParams) (db.ApiKey, error)
	ListAPIKeys(context.Context, pgtype.UUID) ([]db.ApiKey, error)
	GetAPIKeyByHash(context.Context, string) (db.ApiKey, error)
	RevokeAPIKey(context.Context, db.RevokeAPIKeyParams) (int64, error)
	TouchAPIKey(context.Context, pgtype.UUID) error
}

// Service 提供密钥管理与认证。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	now     func() time.Time
}

// NewService 构造服务。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: db.New(pool), now: time.Now}
}

// CreateKey 生成一把新密钥并返回明文。
//
// 明文只出现在返回值里：数据库存的是 SHA-256 哈希，
// 此后再也无法还原。这是与用户口令相同的取舍——
// 宁可让用户重新生成，也不让密钥在库里以可用形式存在。
func (s *Service) CreateKey(ctx context.Context, userID uuid.UUID, input CreateKeyInput) (CreatedKey, error) {
	if userID == uuid.Nil {
		return CreatedKey{}, ErrUnauthorized
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return CreatedKey{}, fmt.Errorf("%w: 名称不能为空", ErrValidation)
	}
	if utf8.RuneCountInString(name) > maxKeyNameLen {
		return CreatedKey{}, fmt.Errorf("%w: 名称不能超过 %d 个字符", ErrValidation, maxKeyNameLen)
	}
	if len(input.Scopes) == 0 {
		return CreatedKey{}, fmt.Errorf("%w: 至少选择一个权限范围", ErrValidation)
	}
	// 去重并校验：未知 scope 直接拒绝（见 isValidScope 的说明）。
	seen := make(map[string]bool, len(input.Scopes))
	scopes := make([]string, 0, len(input.Scopes))
	for _, scope := range input.Scopes {
		scope = strings.TrimSpace(scope)
		if !isValidScope(scope) {
			return CreatedKey{}, fmt.Errorf("%w: 未知的权限范围 %q", ErrValidation, scope)
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return CreatedKey{}, fmt.Errorf("%w: 过期时间必须晚于当前时间", ErrValidation)
	}

	plaintext, hash, prefix, err := generateKey()
	if err != nil {
		return CreatedKey{}, err
	}

	expiresAt := pgtype.Timestamptz{}
	if input.ExpiresAt != nil {
		expiresAt = pgtype.Timestamptz{Time: input.ExpiresAt.UTC(), Valid: true}
	}

	created, err := s.queries.CreateAPIKey(ctx, db.CreateAPIKeyParams{
		UserID:    uuidParam(userID),
		Name:      name,
		KeyHash:   hash,
		KeyPrefix: prefix,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return CreatedKey{}, fmt.Errorf("创建 API Key 失败: %w", err)
	}

	return CreatedKey{Key: toKey(created), Plaintext: plaintext}, nil
}

// ListKeys 列出当前用户未撤销的密钥。
func (s *Service) ListKeys(ctx context.Context, userID uuid.UUID) ([]Key, error) {
	if userID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	rows, err := s.queries.ListAPIKeys(ctx, uuidParam(userID))
	if err != nil {
		return nil, fmt.Errorf("查询 API Key 失败: %w", err)
	}
	keys := make([]Key, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, toKey(row))
	}
	return keys, nil
}

// RevokeKey 撤销密钥。撤销立即生效：认证查询带 revoked_at IS NULL。
func (s *Service) RevokeKey(ctx context.Context, userID, keyID uuid.UUID) error {
	if userID == uuid.Nil {
		return ErrUnauthorized
	}
	affected, err := s.queries.RevokeAPIKey(ctx, db.RevokeAPIKeyParams{
		ID: uuidParam(keyID), UserID: uuidParam(userID),
	})
	if err != nil {
		return fmt.Errorf("撤销 API Key 失败: %w", err)
	}
	if affected == 0 {
		// 密钥不存在、不属于当前用户、或已撤销 —— 统一返回同一个错误，
		// 不让调用方区分（尤其是不能区分「不属于你」与「不存在」）。
		return ErrKeyNotFound
	}
	return nil
}

// Authenticate 校验明文密钥，返回用户 ID 与 scopes。
func (s *Service) Authenticate(ctx context.Context, rawKey string) (uuid.UUID, []string, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" || !strings.HasPrefix(rawKey, keyPrefix) {
		return uuid.Nil, nil, ErrUnauthorized
	}

	row, err := s.queries.GetAPIKeyByHash(ctx, hashKey(rawKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, ErrUnauthorized
	}
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("校验 API Key 失败: %w", err)
	}

	// 记录使用时间。失败不影响认证结果——这只是展示用的元数据，
	// 为了它让一次合法请求失败不划算。
	if err := s.queries.TouchAPIKey(ctx, row.ID); err != nil {
		// 不返回错误，也不打日志：这是高频路径，日志会淹没真正的问题。
		_ = err
	}

	return fromPG(row.UserID), row.Scopes, nil
}

// generateKey 生成明文密钥、哈希与展示前缀。
func generateKey() (plaintext, hash, prefix string, err error) {
	buf := make([]byte, keyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("生成 API Key 失败: %w", err)
	}
	// base64url 无填充：32 字节 → 43 字符，加上 "tsk_" 共 47 字符。
	// 不含 +/= 等字符，便于放进 URL、curl 命令与配置文件。
	plaintext = keyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	prefix = plaintext[:keyDisplayLen]
	return plaintext, hashKey(plaintext), prefix, nil
}

// hashKey 计算密钥的 SHA-256 十六进制串。
//
// 不加盐：密钥本身是 256 位随机数，不存在字典攻击的空间；
// 加盐反而会让「按哈希查表」变成全表扫描。
func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func toKey(row db.ApiKey) Key {
	return Key{
		ID:         fromPG(row.ID),
		Name:       row.Name,
		Prefix:     row.KeyPrefix,
		Scopes:     row.Scopes,
		LastUsedAt: fromPGTimePtr(row.LastUsedAt),
		ExpiresAt:  fromPGTimePtr(row.ExpiresAt),
		CreatedAt:  fromPGTime(row.CreatedAt),
	}
}

func uuidParam(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

func fromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

func fromPGTime(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time
}

func fromPGTimePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
