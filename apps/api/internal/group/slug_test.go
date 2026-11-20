package group

import (
	"errors"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "普通英文", in: "Machine Learning Lab", want: "machine-learning-lab"},
		{name: "大小写折叠", in: "NLP Group", want: "nlp-group"},
		{name: "下划线与点号", in: "graph_theory.group", want: "graph-theory-group"},
		{name: "连续分隔符折叠", in: "a  --  b", want: "a-b"},
		{name: "首尾分隔符去除", in: "-- hello --", want: "hello"},
		{name: "中文丢弃", in: "机器学习 Lab", want: "lab"},
		{name: "纯中文返回空", in: "机器学习", want: ""},
		{name: "数字保留", in: "Group 42", want: "group-42"},
		// 10 段「abcdefghij」共 109 字符（含分隔符），截断到 40：
		// "abcdefghij-" 重复 3 次占 33 位，再补 7 位 "abcdefg"。
		{name: "超长截断且不以连字符结尾", in: strings.Repeat("abcdefghij ", 10), want: strings.Repeat("abcdefghij-", 3) + "abcdefg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Slugify(tt.in); got != tt.want {
				t.Fatalf("Slugify(%q) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSlugifyNeverExceedsLimit(t *testing.T) {
	got := Slugify(strings.Repeat("a", 200))
	if len(got) > slugBaseMaxLen {
		t.Fatalf("slug 长度 = %d，超过上限 %d", len(got), slugBaseMaxLen)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("slug 不应以连字符结尾: %q", got)
	}
}

func TestUniqueSlug(t *testing.T) {
	t.Run("基础形式未占用时直接使用", func(t *testing.T) {
		got, err := uniqueSlug("research", func(string) (bool, error) { return false, nil }, randomSuffix)
		if err != nil || got != "research" {
			t.Fatalf("got = %q, err = %v", got, err)
		}
	})

	t.Run("空基础名退化为 group", func(t *testing.T) {
		got, err := uniqueSlug("", func(string) (bool, error) { return false, nil }, randomSuffix)
		if err != nil || got != slugFallback {
			t.Fatalf("got = %q, err = %v", got, err)
		}
	})

	t.Run("冲突时追加随机后缀", func(t *testing.T) {
		taken := map[string]bool{"research": true}
		suffixCalled := 0
		got, err := uniqueSlug("research", func(s string) (bool, error) {
			return taken[s], nil
		}, func() (string, error) {
			suffixCalled++
			return "a1b2", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "research-a1b2" {
			t.Fatalf("got = %q，期望 research-a1b2", got)
		}
		if suffixCalled != 1 {
			t.Fatalf("后缀生成次数 = %d，期望 1", suffixCalled)
		}
	})

	t.Run("后缀连续冲突直到放弃", func(t *testing.T) {
		_, err := uniqueSlug("x", func(string) (bool, error) { return true, nil }, func() (string, error) {
			return "zzzz", nil
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v，期望 ErrValidation", err)
		}
	})

	t.Run("查询错误向上传播", func(t *testing.T) {
		sentinel := errors.New("db down")
		_, err := uniqueSlug("x", func(string) (bool, error) { return false, sentinel }, randomSuffix)
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v，期望透传 sentinel", err)
		}
	})
}

func TestInviteCodeShape(t *testing.T) {
	seen := make(map[string]struct{}, 32)
	for i := 0; i < 32; i++ {
		code, err := inviteCode()
		if err != nil {
			t.Fatalf("生成邀请码失败: %v", err)
		}
		if len(code) != 16 {
			t.Fatalf("邀请码长度 = %d，期望 16", len(code))
		}
		for _, r := range code {
			if !strings.ContainsRune(slugAlphabet, r) {
				t.Fatalf("邀请码含非字母表字符 %q: %q", r, code)
			}
		}
		if _, dup := seen[code]; dup {
			t.Fatalf("邀请码重复: %q", code)
		}
		seen[code] = struct{}{}
	}
}

func TestRandomSuffixShape(t *testing.T) {
	suffix, err := randomSuffix()
	if err != nil {
		t.Fatalf("生成后缀失败: %v", err)
	}
	if len(suffix) != slugSuffixLen {
		t.Fatalf("后缀长度 = %d，期望 %d", len(suffix), slugSuffixLen)
	}
}
