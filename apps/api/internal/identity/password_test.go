package identity

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("HashPassword() 未返回 PHC 编码: %q", hash)
	}
	valid, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !valid {
		t.Fatalf("VerifyPassword() = %v, %v；希望为 true, nil", valid, err)
	}
	valid, err = VerifyPassword(hash, "wrong password")
	if err != nil || valid {
		t.Fatalf("错误密码校验 = %v, %v；希望为 false, nil", valid, err)
	}
}

func TestPasswordSaltsAreRandom(t *testing.T) {
	first, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("同一密码的两次摘要相同，盐不是随机的")
	}
}

func TestVerifyPasswordRejectsMalformedPHC(t *testing.T) {
	cases := []string{
		"",
		"plain text",
		"$argon2id$v=19$m=65536,t=1,p=4$bad-base64$bad-base64",
		"$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"$argon2id$v=18$m=65536,t=1,p=4$c2FsdA$a2V5",
	}
	for _, hash := range cases {
		if valid, err := VerifyPassword(hash, "password"); err == nil || valid {
			t.Errorf("VerifyPassword(%q) = %v, %v；应拒绝", hash, valid, err)
		}
	}
}
