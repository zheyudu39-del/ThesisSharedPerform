package curation

import (
	"errors"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"英文标题", "Weekly Must Read", "weekly-must-read"},
		{"纯中文标题退化为空", "本周必读 · 多模态", ""},
		{"中英混合只保留 ASCII", "AI 前沿 Frontier", "ai-frontier"},
		{"连续分隔符折叠", "a  --  b", "a-b"},
		{"首尾分隔符去掉", "  -abc-  ", "abc"},
		{"数字保留", "Top 10 Papers", "top-10-papers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Slugify(tt.title); got != tt.want {
				t.Fatalf("Slugify(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

// TestSlugifyTruncates 验证超长标题被截断到上限以内。
// collections.slug 是 varchar(64)，截断后还要给随机后缀留余量。
func TestSlugifyTruncates(t *testing.T) {
	got := Slugify(strings.Repeat("a", 200))
	if len(got) > slugBaseMaxLen {
		t.Fatalf("slug 长度 %d 超过上限 %d", len(got), slugBaseMaxLen)
	}
}

// TestUniqueSlug 验证冲突重试逻辑。
func TestUniqueSlug(t *testing.T) {
	taken := map[string]bool{"weekly": true, "weekly-aaaa": true}

	exists := func(s string) (bool, error) { return taken[s], nil }
	suffixes := []string{"aaaa", "bbbb"}
	i := 0
	suffix := func() (string, error) {
		s := suffixes[i]
		i++
		return s, nil
	}

	got, err := uniqueSlug("weekly", exists, suffix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "weekly-bbbb" {
		t.Fatalf("uniqueSlug = %q, want %q", got, "weekly-bbbb")
	}
}

// TestUniqueSlugFallback 验证空标题退化为 collection 前缀。
func TestUniqueSlugFallback(t *testing.T) {
	exists := func(string) (bool, error) { return false, nil }
	suffix := func() (string, error) { return "zzzz", nil }

	got, err := uniqueSlug("", exists, suffix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "collection" {
		t.Fatalf("uniqueSlug = %q, want %q", got, "collection")
	}
}

// TestUniqueSlugExhausted 验证重试耗尽后返回校验错误而非死循环。
func TestUniqueSlugExhausted(t *testing.T) {
	exists := func(string) (bool, error) { return true, nil }
	suffix := func() (string, error) { return "aaaa", nil }

	_, err := uniqueSlug("weekly", exists, suffix)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}
