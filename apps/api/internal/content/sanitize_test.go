package content

import (
	"math"
	"strings"
	"testing"
)

func TestSanitizeContent(t *testing.T) {
	htmlText, text := SanitizeContent("**你好** <script>alert(1)</script> [链接](javascript:alert(1))")
	if strings.Contains(strings.ToLower(htmlText), "script") || strings.Contains(htmlText, "javascript:") {
		t.Fatalf("危险内容未被清除: %s", htmlText)
	}
	if !strings.Contains(htmlText, "<strong>你好</strong>") {
		t.Fatalf("Markdown 未渲染: %s", htmlText)
	}
	if strings.Contains(text, "<") || !strings.Contains(text, "你好") {
		t.Fatalf("纯文本不正确: %s", text)
	}
}

func TestHotScore(t *testing.T) {
	if got := HotScore(1, 2, 3, 0); math.Abs(got-14/math.Pow(2, 1.5)) > 1e-10 {
		t.Fatalf("热度分错误: %v", got)
	}
	if got := HotScore(0, 0, 0, 24); got != 0 {
		t.Fatalf("无互动应为零: %v", got)
	}
	if HotScore(10, 0, 0, -1) != HotScore(10, 0, 0, 0) {
		t.Fatal("未来时间应按零小时计算")
	}
}

func TestNormalizeTag(t *testing.T) {
	name, slug, ok := NormalizeTag(" 机器学习  ")
	if !ok || name != "机器学习" || slug != "机器学习" {
		t.Fatalf("标签规范化失败: %q %q %v", name, slug, ok)
	}
	if _, _, ok := NormalizeTag("!!!"); ok {
		t.Fatal("应拒绝无文字标签")
	}
}
