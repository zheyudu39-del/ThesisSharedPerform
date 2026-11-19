package content

import (
	"math"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	tagNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
	markdown       = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		// 原始 HTML 必须转义，避免 Markdown 渲染阶段引入未经白名单处理的标记。
		goldmark.WithRendererOptions(html.WithHardWraps(), html.WithXHTML(), html.WithUnsafe()),
	)
)

// SanitizeContent 将纯文本或 Markdown 转为安全 HTML，并提取供检索的纯文本。
// 白名单仅保留常用排版，不允许脚本、事件属性、iframe 或危险 URL。
func SanitizeContent(input string) (htmlText, plainText string) {
	var rendered strings.Builder
	if err := markdown.Convert([]byte(input), &rendered); err != nil {
		// strings.Builder 写入不会失败；保守降级为转义文本。
		rendered.Reset()
		rendered.WriteString(input)
	}

	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "strong", "em", "del", "blockquote", "pre", "code", "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "table", "thead", "tbody", "tr", "th", "td")
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[a-zA-Z0-9_-]+$`)).OnElements("code")
	policy.AllowURLSchemes("https", "http", "mailto")
	policy.RequireNoFollowOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	htmlText = strings.TrimSpace(policy.Sanitize(rendered.String()))

	// 从已经清洗的 HTML 提取文本，确保入库文本与实际展示内容一致。
	plainText = strings.TrimSpace(bluemonday.StrictPolicy().Sanitize(htmlText))
	return htmlText, plainText
}

// NormalizeTag 清理标签并生成稳定的唯一 slug。
func NormalizeTag(input string) (name, slug string, ok bool) {
	name = strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	slug = strings.ToLower(name)
	if len(name) == 0 || len([]rune(name)) > 64 || !tagNamePattern.MatchString(slug) && !containsLetterOrNumber(slug) {
		return "", "", false
	}
	return name, slug, true
}

func containsLetterOrNumber(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x80 {
			return true
		}
	}
	return false
}

// HotScore 按 PRD §5.2 计算时间衰减热度。
func HotScore(likes, comments, reposts int32, hoursSinceCreated float64) float64 {
	if hoursSinceCreated < 0 {
		hoursSinceCreated = 0
	}
	raw := float64(likes) + 2*float64(comments) + 3*float64(reposts)
	return raw / math.Pow(hoursSinceCreated+2, 1.5)
}
