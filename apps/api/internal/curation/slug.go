package curation

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	slugBaseMaxLen = 40
	slugSuffixLen  = 4
	slugMaxAttempts = 8
	// 与群组 slug 使用同一份字符表：排除 0/o、1/l 等易混淆字符，
	// 方便人工从链接里抄写。
	slugAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"
)

// Slugify 把标题转写成 URL 友好的基础 slug。
//
// 中文标题无法直接进入 URL，会被丢弃；此时返回空串，
// 由 uniqueSlug 退化为 "collection" + 随机后缀。
// 这与群组 slug 的策略一致，不引入拼音词表这样的额外依赖。
func Slugify(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '_' || r == '-' || r == '.':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > slugBaseMaxLen {
		slug = strings.TrimRight(slug[:slugBaseMaxLen], "-")
	}
	return slug
}

// randomSuffix 生成固定长度的随机后缀。
// 使用 crypto/rand 而非 math/rand：slug 虽非机密，但可预测会让
// 人为制造冲突变得容易。
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

// uniqueSlug 在 exists 判定下生成唯一 slug。
// 判定以回调注入，便于在没有数据库的单元测试里验证。
func uniqueSlug(base string, exists func(string) (bool, error), suffix func() (string, error)) (string, error) {
	if base == "" {
		base = "collection"
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
	return "", fmt.Errorf("%w: 无法生成唯一 slug，请换一个标题", ErrValidation)
}
