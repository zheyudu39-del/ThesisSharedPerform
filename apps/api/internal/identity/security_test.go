package identity

import (
	"errors"
	"strings"
	"testing"
)

// 被篡改的 PHC 摘要不应诱发超大内存分配。
func TestVerifyPasswordRejectsExcessiveParams(t *testing.T) {
	cases := []struct {
		name string
		hash string
	}{
		{"超大内存", "$argon2id$v=19$m=9999999,t=1,p=4$c2FsdA$a2V5"},
		{"过大迭代", "$argon2id$v=19$m=65536,t=99,p=4$c2FsdA$a2V5"},
		{"过大并行度", "$argon2id$v=19$m=65536,t=1,p=999$c2FsdA$a2V5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, err := VerifyPassword(c.hash, "password")
			if ok {
				t.Fatal("超限参数不应通过校验")
			}
			if err == nil {
				t.Fatal("超限参数应返回错误")
			}
		})
	}
}

// 正常摘要仍应可用。
func TestVerifyPasswordAcceptsValidHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("生成摘要失败: %v", err)
	}
	if ok, err := VerifyPassword(hash, "correct horse battery staple"); err != nil || !ok {
		t.Fatalf("正确密码应校验通过, ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword(hash, "wrong password"); ok {
		t.Fatal("错误密码不应通过")
	}
}

// 每次生成的盐都不同，摘要因此不同。
func TestHashPasswordUsesUniqueSalt(t *testing.T) {
	a, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("相同密码两次生成的摘要不应相同（盐必须随机）")
	}
}

// 密码长度上限校验。
func TestValidateRegisterPasswordBounds(t *testing.T) {
	base := RegisterInput{Email: "a@b.com", Name: "测试", Handle: "tester"}

	short := base
	short.Password = "1234567"
	if err := validateRegister(short); !errors.Is(err, ValidationError) {
		t.Fatalf("7 位密码应被拒绝, got %v", err)
	}

	long := base
	long.Password = strings.Repeat("a", 129)
	if err := validateRegister(long); !errors.Is(err, ValidationError) {
		t.Fatalf("129 位密码应被拒绝, got %v", err)
	}

	ok := base
	ok.Password = "12345678"
	if err := validateRegister(ok); err != nil {
		t.Fatalf("8 位密码应通过, got %v", err)
	}
}
