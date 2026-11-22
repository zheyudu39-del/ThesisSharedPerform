package content

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNormalizeTagSlug 验证标签 slug 归一化与写入时保持一致。
//
// 写入侧（NormalizeTag）用 strings.ToLower(name)，查询侧必须用同一套规则，
// 否则「机器学习」写进去、前端拼出的小写 slug 查不到。
func TestNormalizeTagSlug(t *testing.T) {
	tests := map[string]string{
		"machinelearning":     "machinelearning",
		"  MachineLearning  ": "machinelearning",
		"机器学习":                "机器学习",
		"#tag":                "#tag",
	}
	for input, want := range tests {
		got, err := normalizeTagSlug(input)
		if err != nil {
			t.Fatalf("normalizeTagSlug(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeTagSlug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeTagSlugRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "   ", "\t\n"} {
		if _, err := normalizeTagSlug(input); !errors.Is(err, ErrValidation) {
			t.Fatalf("normalizeTagSlug(%q) err = %v, want ErrValidation", input, err)
		}
	}
	// 超过 64 字节（tags.slug 列上限）
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := normalizeTagSlug(string(long)); !errors.Is(err, ErrValidation) {
		t.Fatalf("超长标签应当被拒绝")
	}
}

// TestTagSlugMatchesWritePath 交叉验证：写入侧与查询侧对同一输入给出同一 slug。
// 两侧规则一旦分叉，话题页就会查不到刚发出去的标签。
func TestTagSlugMatchesWritePath(t *testing.T) {
	for _, input := range []string{"Machine Learning", "NLP", "深度学习", "  spaced  "} {
		_, writtenSlug, ok := NormalizeTag(input)
		if !ok {
			continue
		}
		queried, err := normalizeTagSlug(writtenSlug)
		if err != nil {
			t.Fatalf("写入侧产出的 slug %q 查询侧拒绝：%v", writtenSlug, err)
		}
		if queried != writtenSlug {
			t.Fatalf("两侧不一致：写入 %q，查询 %q", writtenSlug, queried)
		}
	}
}

// TestEncodeCursorTagStream 验证话题页能用游标翻页。
func TestEncodeCursorTagStream(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	encoded, err := EncodeCursor("tag", now, id, nil)
	if err != nil {
		t.Fatalf("EncodeCursor(tag): %v", err)
	}
	gotTime, gotID, _, err := DecodeCursor(encoded, "tag")
	if err != nil {
		t.Fatalf("DecodeCursor(tag): %v", err)
	}
	if gotID != id || gotTime.UnixNano() != now.UnixNano() {
		t.Fatalf("往返不一致")
	}
	// 跨 stream 复用必须被拒绝
	if _, _, _, err := DecodeCursor(encoded, "all"); err == nil {
		t.Fatal("tag 游标不应用于 all")
	}
}
