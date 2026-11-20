package search

import (
	"html"
	"strings"
	"unicode"
)

// 片段参数：命中词前后各取 snippetContext 个字符（按 rune 计，避免切坏中文）。
// 之所以用「字符」而不是「词」，是因为 simple 分词对中文没有词边界，
// 只有按字取窗才能稳定地展示上下文。
const (
	snippetContext = 60
	// snippetMaxRunes 限制片段长度，防止 body 极长且命中很多时返回超大字符串。
	snippetMaxRunes = snippetContext*2 + 64
	// highlightOpen/Close 是唯一允许出现在片段里的标记。
	highlightOpen  = "<mark>"
	highlightClose = "</mark>"
)

// snippet 从 body 中提取包含关键词的上下文片段。
//
// 安全约定：先对原文做 HTML 转义，再插入 <mark>。顺序反过来（先拼标记再转义）
// 会把 <mark> 本身转成 &lt;mark&gt;，前端拿到的就不是高亮而是字面量；
// 而先转义再拼，则原文里的 <script> 一定变成 &lt;script&gt;，不会被执行。
//
// 匹配规则：
//   - 大小写不敏感（英文）；
//   - 先找完整关键词，找不到再逐个词找（用户输入多个词时，命中任一词即可）；
//   - 都找不到时返回开头一段（不截断原意），不加任何标记。
func snippet(body, query string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	// 在「未转义」的原文上定位，才能保证下标与 rune 边界一致；
	// 拿到窗口后再整体转义并插入标记。
	runes := []rune(body)
	start, length := locateMatch(runes, query)

	if start < 0 {
		// 没有命中：返回开头，让结果卡片仍能展示摘要。
		end := min(len(runes), snippetMaxRunes)
		return escapeSegment(runes[:end])
	}

	from := start - snippetContext
	if from < 0 {
		from = 0
	}
	to := start + length + snippetContext
	if to > len(runes) {
		to = len(runes)
	}

	var b strings.Builder
	if from > 0 {
		b.WriteString("…")
	}
	b.WriteString(escapeSegment(runes[from:start]))
	b.WriteString(highlightOpen)
	b.WriteString(escapeSegment(runes[start : start+length]))
	b.WriteString(highlightClose)
	b.WriteString(escapeSegment(runes[start+length : to]))
	if to < len(runes) {
		b.WriteString("…")
	}
	return b.String()
}

// locateMatch 在 runes 中寻找查询词的首次出现，返回起始下标与长度（rune 数）。
// 找不到返回 (-1, 0)。
func locateMatch(runes []rune, query string) (int, int) {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return -1, 0
	}

	// 优先整句匹配：用户搜「深度学习 模型」时，连写出现的位置最相关。
	whole := []rune(strings.ToLower(strings.TrimSpace(query)))
	if idx := indexFold(runes, whole); idx >= 0 {
		return idx, len(whole)
	}

	// 退化为逐词匹配，取最靠前的一处。
	bestStart, bestLen := -1, 0
	for _, term := range terms {
		needle := []rune(term)
		idx := indexFold(runes, needle)
		if idx < 0 {
			continue
		}
		if bestStart < 0 || idx < bestStart {
			bestStart, bestLen = idx, len(needle)
		}
	}
	return bestStart, bestLen
}

// queryTerms 把查询串拆成去重的小写词。中文没有空格，整段保留为一个词。
func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	seen := make(map[string]struct{}, len(fields))
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		terms = append(terms, f)
	}
	return terms
}

// indexFold 返回 needle 在 haystack 中首次出现的下标，忽略大小写；无则 -1。
//
// 逐 rune 比较时做 ToLower，而不是先把整段 haystack 转小写：
// 某些字符（如 'İ'）转小写后 rune 数量会变化，整体转换会让下标与原文错位，
// 进而把片段切在错误的字符上。
func indexFold(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if unicode.ToLower(haystack[i+j]) != unicode.ToLower(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// escapeSegment 把一段原文转义成可安全嵌入 HTML 的文本。
func escapeSegment(runes []rune) string {
	return html.EscapeString(string(runes))
}

// buildLikePattern 构造 ILIKE 模式：转义用户输入里的通配符后再包上 %。
//
// 不转义的话，搜索 "50%" 或 "a_b" 会变成通配符，匹配到大量无关结果。
// 这里用反斜杠转义，与 PostgreSQL LIKE 的默认 ESCAPE 字符一致。
func buildLikePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}
