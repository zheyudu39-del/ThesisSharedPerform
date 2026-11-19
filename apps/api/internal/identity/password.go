package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16

	// 解析上限：拒绝被篡改的摘要导致内存/CPU 耗尽。
	maxArgonMemory  = 256 * 1024 // 256 MiB
	maxArgonTime    = 10
	maxArgonThreads = 32
	maxArgonKeyLen  = 1024
	maxArgonSaltLen = 1024
)

// HashPassword 使用 Argon2id 和随机盐生成标准 PHC 编码的密码摘要。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐失败: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return encoded, nil
}

// VerifyPassword 解析 PHC 摘要，并以常数时间比较派生密钥。
func VerifyPassword(encodedHash, password string) (bool, error) {
	var version int
	var memory, time uint32
	var threads uint8
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("无效的 Argon2id PHC 摘要")
	}
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, errors.New("无效的 Argon2id 版本")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, errors.New("无效的 Argon2id 参数")
	}
	salt64, key64 := parts[4], parts[5]
	if version != argon2.Version || memory == 0 || time == 0 || threads == 0 {
		return false, errors.New("不支持的 Argon2id 参数")
	}
	// 参数上限校验：防止被篡改的摘要诱发超大内存分配。
	if memory > maxArgonMemory || time > maxArgonTime || threads > maxArgonThreads {
		return false, errors.New("Argon2id 参数超出允许范围")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(salt64)
	if err != nil || len(salt) == 0 || len(salt) > maxArgonSaltLen {
		return false, errors.New("无效的 Argon2id 盐")
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(key64)
	if err != nil || len(expected) == 0 || len(expected) > maxArgonKeyLen {
		return false, errors.New("无效的 Argon2id 密钥")
	}

	actual := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
