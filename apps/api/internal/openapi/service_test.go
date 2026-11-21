package openapi

import (
	"strings"
	"testing"
)

// TestGenerateKey 验证密钥格式与哈希。
func TestGenerateKey(t *testing.T) {
	plaintext, hash, prefix, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey: %v", err)
	}

	// "tsk_" + 32 字节的 base64url 无填充 = 4 + 43 = 47 字符
	if len(plaintext) != 47 {
		t.Fatalf("密钥长度 = %d, want 47（%s）", len(plaintext), plaintext)
	}
	if !strings.HasPrefix(plaintext, "tsk_") {
		t.Fatalf("密钥缺少 tsk_ 前缀: %s", plaintext)
	}
	// base64url 不应出现 + / =
	for _, bad := range []string{"+", "/", "="} {
		if strings.Contains(plaintext[4:], bad) {
			t.Fatalf("密钥含非 URL 安全字符 %q: %s", bad, plaintext)
		}
	}
	// SHA-256 十六进制串固定 64 字符
	if len(hash) != 64 {
		t.Fatalf("哈希长度 = %d, want 64", len(hash))
	}
	if hash != hashKey(plaintext) {
		t.Fatal("返回的哈希与重新计算的不一致")
	}
	// 前缀是明文的前 12 个字符，且不足以还原密钥
	if prefix != plaintext[:12] {
		t.Fatalf("前缀 = %q, want %q", prefix, plaintext[:12])
	}
	if strings.Contains(plaintext, prefix+prefix) {
		t.Fatal("前缀不应能拼出完整密钥")
	}
}

// TestGenerateKeyUnique 验证两次生成不会撞车。
func TestGenerateKeyUnique(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		plaintext, _, _, err := generateKey()
		if err != nil {
			t.Fatalf("generateKey: %v", err)
		}
		if seen[plaintext] {
			t.Fatalf("第 %d 次生成重复密钥", i)
		}
		seen[plaintext] = true
	}
}

// TestHashKey 验证哈希是确定性的，且不同输入得到不同输出。
func TestHashKey(t *testing.T) {
	a := hashKey("tsk_abc")
	b := hashKey("tsk_abc")
	c := hashKey("tsk_abd")
	if a != b {
		t.Fatal("相同输入应得到相同哈希")
	}
	if a == c {
		t.Fatal("不同输入应得到不同哈希")
	}
}

// TestHasScope 验证 scope 判定。
func TestHasScope(t *testing.T) {
	scopes := []string{ScopeReadPapers, ScopeReadUsers}
	if !HasScope(scopes, ScopeReadPapers) {
		t.Fatal("应包含 read:papers")
	}
	if HasScope(scopes, ScopeReadPosts) {
		t.Fatal("不应包含 read:posts")
	}
	if HasScope(nil, ScopeReadPapers) {
		t.Fatal("空 scopes 不应通过任何判定")
	}
}

// TestIsValidScope 验证未知 scope 被拒绝。
//
// 静默忽略拼错的 scope 会让用户在调用时才发现 403，排查成本高得多。
func TestIsValidScope(t *testing.T) {
	for _, scope := range AllScopes() {
		if !isValidScope(scope) {
			t.Fatalf("%q 应当是合法 scope", scope)
		}
	}
	for _, bad := range []string{"", "read", "read:everything", "admin", "READ:PAPERS"} {
		if isValidScope(bad) {
			t.Fatalf("%q 不应是合法 scope", bad)
		}
	}
}
