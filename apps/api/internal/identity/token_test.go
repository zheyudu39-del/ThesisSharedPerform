package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestTokenIssueAndParse(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	manager := NewTokenManager("test-secret", 2*time.Hour)
	manager.now = func() time.Time { return now }
	userID := uuid.New()
	tokenString, err := manager.Issue(userID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	parsed, err := manager.Parse(tokenString)
	if err != nil || parsed != userID {
		t.Fatalf("Parse() = %v, %v；希望为 %v, nil", parsed, err, userID)
	}
	var claims jwt.RegisteredClaims
	_, err = jwt.ParseWithClaims(tokenString, &claims, func(*jwt.Token) (any, error) {
		return []byte("test-secret"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !claims.IssuedAt.Time.Equal(now) || !claims.ExpiresAt.Time.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("iat/exp 错误: iat=%v exp=%v", claims.IssuedAt, claims.ExpiresAt)
	}
}

func TestTokenRejectsInvalidInputs(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	manager := NewTokenManager("test-secret", time.Hour)
	manager.now = func() time.Time { return now }
	userID := uuid.New()

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Subject: userID.String(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	})
	noneToken, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	wrongAlgorithm := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.RegisteredClaims{
		Subject: userID.String(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	})
	hs384Token, err := wrongAlgorithm.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: userID.String(), IssuedAt: jwt.NewNumericDate(now.Add(-2 * time.Hour)), ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
	})
	expiredToken, err := expired.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	for name, tokenString := range map[string]string{
		"空令牌": "", "乱码": "not-a-jwt", "alg none": noneToken,
		"非 HS256": hs384Token, "过期": expiredToken,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := manager.Parse(tokenString); !errors.Is(err, InvalidToken) {
				t.Fatalf("Parse() error = %v；希望为 InvalidToken", err)
			}
		})
	}
}

func TestTokenRejectsWrongSecret(t *testing.T) {
	issuer := NewTokenManager("secret-one", time.Hour)
	tokenString, err := issuer.Issue(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewTokenManager("secret-two", time.Hour).Parse(tokenString); !errors.Is(err, InvalidToken) {
		t.Fatalf("错误密钥的 Parse() error = %v；希望为 InvalidToken", err)
	}
}
