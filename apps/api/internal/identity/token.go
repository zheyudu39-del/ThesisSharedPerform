package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenManager 负责 HS256 访问令牌的签发和校验。
type TokenManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	if ttl <= 0 {
		ttl = 168 * time.Hour
	}
	return &TokenManager{secret: []byte(secret), ttl: ttl, now: time.Now}
}

// Issue 为用户签发包含 sub、iat、exp 的 JWT。
func (m *TokenManager) Issue(userID uuid.UUID) (string, error) {
	if len(m.secret) == 0 {
		return "", errors.New("JWT 密钥不能为空")
	}
	now := m.now().UTC()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse 校验签名和有效期，只接受 HS256，并返回用户 UUID。
func (m *TokenManager) Parse(tokenString string) (uuid.UUID, error) {
	if len(m.secret) == 0 {
		return uuid.Nil, InvalidToken
	}
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("不支持的签名算法: %s", token.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithTimeFunc(m.now))
	if err != nil {
		return uuid.Nil, InvalidToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, InvalidToken
	}
	return userID, nil
}
