package group

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	// slugBaseMaxLen 是基础 slug 的最大长度。加上「-xxxx」后缀仍远小于
	// groups.slug 的 varchar(64)，无需在拼接后再截断。
	slugBaseMaxLen = 40
	// slugSuffixLen 是冲突后缀的字符数，36^4 ≈ 168 万种组合，
	// 对同一群名的并发建群足够分散。
	slugSuffixLen = 4
	// slugMaxAttempts 限制冲突重试次数，避免在极端情况下无限循环。
	slugMaxAttempts = 8
	// slugFallback 用于群名不含任何 ASCII 字母数字时（例如纯中文名）。
	// 中文转写需要拼音词表，这里选择退化为基础词 + 随机后缀，
	// 保证 slug 始终是 URL 安全且唯一的。
	slugFallback = "group"
	// slugAlphabet 排除容易混淆的字符，方便人工抄写与朗读。
	slugAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"
)

// Slugify 把群名转写成 URL 友好的基础 slug。
//
// 规则：ASCII 字母转小写，数字保留；空格、下划线、点号与连字符统一为「-」；
// 其他字符（中文、标点、表情）直接丢弃。连续连字符折叠，首尾连字符去掉。
// 结果为空时返回空串，由调用方决定退化策略。
func Slugify(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '_' || r == '-' || r == '.':
			// 折叠连续分隔符，且不在开头写连字符。
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			// 非 ASCII 字符无法直接进入 URL slug，丢弃。
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > slugBaseMaxLen {
		// 截断后可能以连字符结尾，再修剪一次。
		slug = strings.TrimRight(slug[:slugBaseMaxLen], "-")
	}
	return slug
}

// randomSuffix 生成 slugSuffixLen 个随机字符。crypto/rand 失败时返回错误，
// 而不是退回可预测的值——slug 虽非机密，但可预测会让人为制造冲突变得容易。
func randomSuffix() (string, error) {
	buf := make([]byte, slugSuffixLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 slug 后缀失败: %w", err)
	}
	out := make([]byte, slugSuffixLen)
	for i, value := range buf {
		out[i] = slugAlphabet[int(value)%len(slugAlphabet)]
	}
	return string(out), nil
}

// uniqueSlug 在 exists 判定下生成唯一 slug：先试基础形式，冲突则追加随机后缀。
// 依赖以回调形式注入，便于在没有数据库的单元测试里验证。
func uniqueSlug(base string, exists func(string) (bool, error), suffix func() (string, error)) (string, error) {
	if base == "" {
		base = slugFallback
	}
	taken, err := exists(base)
	if err != nil {
		return "", err
	}
	if !taken {
		return base, nil
	}
	for i := 0; i < slugMaxAttempts; i++ {
		s, err := suffix()
		if err != nil {
			return "", err
		}
		candidate := base + "-" + s
		taken, err := exists(candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: 无法生成唯一 slug，请换一个群名", ErrValidation)
}

// inviteCode 生成 16 字符的邀请码。
func inviteCode() (string, error) {
	const codeLen = 16
	buf := make([]byte, codeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成邀请码失败: %w", err)
	}
	out := make([]byte, codeLen)
	for i, value := range buf {
		out[i] = slugAlphabet[int(value)%len(slugAlphabet)]
	}
	return string(out), nil
}
