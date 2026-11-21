package citation

import (
	"fmt"
	"strings"
	"unicode"
)

// BibEntry 是 BibTeX 解析后的中间结果：字段名统一小写，键名保留原始大小写。
//
// 解析器刻意保持「宽松」：真实世界的 .bib 文件里有大量非标准写法
// （字段换行、值里带花括号保护大小写、作者用 and 连接、未知条目类型）。
// 过于严格只会让用户导入失败却说不清原因，因此这里只拒绝结构性错误。
type BibEntry struct {
	Type   string
	Key    string
	Fields map[string]string
}

// bibEntryTypes 是允许导入的条目类型。未知类型统一降级为 misc，
// 而不是报错——用户从各种数据库导出的 .bib 常有 @online、@phdthesis 等，
// 直接丢弃这些条目比降级更糟。
var bibEntryTypes = map[string]string{
	"article":       "article",
	"inproceedings": "inproceedings",
	"conference":    "inproceedings",
	"book":          "book",
	"misc":          "misc",
	"online":        "misc",
	"phdthesis":     "misc",
	"mastersthesis": "misc",
	"techreport":    "misc",
	"incollection":  "inproceedings",
	"unpublished":   "misc",
}

// ParseBibTeX 解析一段 BibTeX 文本，返回其中的条目。
//
// 只支持任务要求的字段集合；@comment、@preamble、@string 会被跳过。
// 空文本或没有任何条目时返回空切片且不报错——前端粘贴空框不应是错误。
func ParseBibTeX(input string) ([]BibEntry, error) {
	p := &bibParser{src: []rune(input)}
	return p.parseAll()
}

type bibParser struct {
	src []rune
	pos int
}

func (p *bibParser) eof() bool { return p.pos >= len(p.src) }

func (p *bibParser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *bibParser) next() rune {
	r := p.src[p.pos]
	p.pos++
	return r
}

// skipSpace 跳过空白与 % 行注释（BibTeX 允许 % 到行尾的注释）。
func (p *bibParser) skipSpace() {
	for !p.eof() {
		r := p.peek()
		if unicode.IsSpace(r) {
			p.pos++
			continue
		}
		if r == '%' {
			for !p.eof() && p.next() != '\n' {
			}
			continue
		}
		return
	}
}

func (p *bibParser) parseAll() ([]BibEntry, error) {
	entries := make([]BibEntry, 0, 8)
	// 同一份输入内重复的键以最后一次为准：后出现的条目通常更完整。
	index := make(map[string]int)
	for {
		p.skipSpace()
		if p.eof() {
			break
		}
		if p.next() != '@' {
			// 条目之外的杂项文本（如中文说明）直接跳过，直到下一个 @。
			continue
		}
		p.skipSpace()
		typeName := strings.ToLower(p.readIdentifier())
		if typeName == "" {
			continue
		}
		p.skipSpace()
		switch typeName {
		case "comment", "preamble", "string":
			// 这些不是文献条目；跳过其内容（可能是 {..} 或 (..)）。
			if p.peek() == '{' || p.peek() == '(' {
				_, _ = p.readBalanced()
			}
			continue
		}
		body, ok := p.readBalanced()
		if !ok {
			return nil, fmt.Errorf("%w: 条目 @%s 缺少闭合花括号", ErrValidation, typeName)
		}
		entry, err := parseBibBody(typeName, body)
		if err != nil {
			return nil, err
		}
		if entry.Key == "" {
			// 没有 cite key 的条目无法被正文引用，导入后也只是死数据。
			return nil, fmt.Errorf("%w: 条目 @%s 缺少 cite key", ErrValidation, typeName)
		}
		if existing, ok := index[entry.Key]; ok {
			entries[existing] = entry
			continue
		}
		index[entry.Key] = len(entries)
		entries = append(entries, entry)
	}
	return entries, nil
}

func (p *bibParser) readIdentifier() string {
	start := p.pos
	for !p.eof() {
		r := p.peek()
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			p.pos++
			continue
		}
		break
	}
	return string(p.src[start:p.pos])
}

// readBalanced 读取从当前分隔符开始、到匹配分隔符为止的内容（不含分隔符）。
// 支持 {..} 与 (..) 两种包裹形式，并正确跳过嵌套花括号与引号内的字符。
func (p *bibParser) readBalanced() (string, bool) {
	if p.eof() {
		return "", false
	}
	open := p.next()
	var close rune
	switch open {
	case '{':
		close = '}'
	case '(':
		close = ')'
	default:
		return "", false
	}
	depth := 1
	start := p.pos
	inQuote := false
	for !p.eof() {
		r := p.next()
		if r == '"' && depth == 1 {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch r {
		case '{':
			depth++
		case close:
			depth--
			if depth == 0 {
				return string(p.src[start : p.pos-1]), true
			}
		}
	}
	return "", false
}

// parseBibBody 拆分「key, field = value, ...」。
func parseBibBody(typeName, body string) (BibEntry, error) {
	entry := BibEntry{
		Type:   bibEntryTypes[typeName],
		Fields: make(map[string]string, 12),
	}
	if entry.Type == "" {
		entry.Type = "misc"
	}
	rest := body
	// key 到第一个逗号为止；没有逗号说明只有 key 没有字段。
	if idx := topLevelIndex(rest, ','); idx >= 0 {
		entry.Key = strings.TrimSpace(rest[:idx])
		rest = rest[idx+1:]
	} else {
		entry.Key = strings.TrimSpace(rest)
		return entry, nil
	}
	fields, err := parseBibFields(rest)
	if err != nil {
		return BibEntry{}, err
	}
	entry.Fields = fields
	return entry, nil
}

// topLevelIndex 找到不在花括号/引号内的第一个目标字符。
func topLevelIndex(s string, target rune) int {
	depth := 0
	inQuote := false
	for i, r := range s {
		if r == '"' && depth == 0 {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch r {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		default:
			if r == target && depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseBibFields 解析字段列表，支持 name = value 与 value 中的 # 拼接。
func parseBibFields(s string) (map[string]string, error) {
	fields := make(map[string]string, 12)
	pos := 0
	runes := []rune(s)
	for pos < len(runes) {
		for pos < len(runes) && (unicode.IsSpace(runes[pos]) || runes[pos] == ',') {
			pos++
		}
		if pos >= len(runes) {
			break
		}
		// 字段名
		start := pos
		for pos < len(runes) && (unicode.IsLetter(runes[pos]) || unicode.IsDigit(runes[pos]) || runes[pos] == '_' || runes[pos] == '-') {
			pos++
		}
		name := strings.ToLower(string(runes[start:pos]))
		for pos < len(runes) && unicode.IsSpace(runes[pos]) {
			pos++
		}
		if pos >= len(runes) || runes[pos] != '=' {
			// 没有等号的碎片（通常是尾部多余逗号或空白）忽略。
			if pos < len(runes) {
				pos++
			}
			continue
		}
		pos++
		value, next, err := readBibValue(runes, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		if name != "" {
			fields[name] = value
		}
	}
	return fields, nil
}

// readBibValue 读取一个字段值，支持 {..}、".."、裸 token 以及 # 拼接。
func readBibValue(runes []rune, pos int) (string, int, error) {
	var parts []string
	for {
		for pos < len(runes) && unicode.IsSpace(runes[pos]) {
			pos++
		}
		if pos >= len(runes) {
			break
		}
		var part string
		switch runes[pos] {
		case '{':
			// 花括号值：允许嵌套。
			depth := 0
			start := pos
			for pos < len(runes) {
				switch runes[pos] {
				case '{':
					depth++
				case '}':
					depth--
				}
				pos++
				if depth == 0 {
					break
				}
			}
			if depth != 0 {
				return "", pos, fmt.Errorf("%w: 字段值缺少闭合花括号", ErrValidation)
			}
			part = string(runes[start+1 : pos-1])
		case '"':
			pos++
			start := pos
			depth := 0
			for pos < len(runes) {
				r := runes[pos]
				if r == '{' {
					depth++
				} else if r == '}' && depth > 0 {
					depth--
				}
				if r == '"' && depth == 0 {
					break
				}
				pos++
			}
			if pos >= len(runes) {
				return "", pos, fmt.Errorf("%w: 字段值缺少闭合引号", ErrValidation)
			}
			part = string(runes[start:pos])
			pos++
		default:
			start := pos
			for pos < len(runes) && runes[pos] != ',' && runes[pos] != '#' && !unicode.IsSpace(runes[pos]) {
				pos++
			}
			part = string(runes[start:pos])
		}
		parts = append(parts, part)
		for pos < len(runes) && unicode.IsSpace(runes[pos]) {
			pos++
		}
		if pos < len(runes) && runes[pos] == '#' {
			pos++
			continue
		}
		break
	}
	return cleanBibTeXText(strings.Join(parts, "")), pos, nil
}

// ParseAuthors 解析 BibTeX 的 author 字段。
//
// 支持两种写法：
//   - "Last, First and Last2, First2"（姓, 名）
//   - "First Last and First2 Last2"（名 姓）
//
// 整段被花括号包裹的视为机构作者（literal）。
func ParseAuthors(raw string) []Author {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []Author{}
	}
	parts := splitAuthors(raw)
	authors := make([]Author, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		authors = append(authors, parseAuthor(part))
	}
	return authors
}

// splitAuthors 按顶层的 " and " 切分，避免把花括号内的 and 当分隔符。
func splitAuthors(raw string) []string {
	var parts []string
	depth := 0
	lower := []rune(raw)
	start := 0
	for i := 0; i < len(lower); i++ {
		switch lower[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		}
		if depth != 0 {
			continue
		}
		// 匹配 " and "（前后是空白/边界），大小写不敏感。
		if i+5 <= len(lower) && strings.EqualFold(string(lower[i:i+5]), " and ") {
			parts = append(parts, string(lower[start:i]))
			i += 4
			start = i + 1
		}
	}
	parts = append(parts, string(lower[start:]))
	return parts
}

func parseAuthor(raw string) Author {
	raw = strings.TrimSpace(raw)
	// 整个姓名被额外一层花括号包裹：机构作者。
	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") && balancedOuter(raw) {
		inner := strings.TrimSpace(raw[1 : len(raw)-1])
		return Author{Literal: cleanBibTeXText(inner)}
	}
	// 花括号可能只包住姓（保护复姓或大小写），先做保护性替换再切分。
	protected := protectBraces(raw)
	if idx := strings.Index(protected, ","); idx >= 0 {
		family := cleanBibTeXText(strings.TrimSpace(protected[:idx]))
		rest := strings.TrimSpace(protected[idx+1:])
		given := rest
		// "Last, Jr, First"：中间的 Jr 是后缀，忽略；名取最后一段。
		if j := strings.LastIndex(rest, ","); j >= 0 {
			given = strings.TrimSpace(rest[j+1:])
		}
		return Author{Family: family, Given: cleanBibTeXText(given)}
	}
	fields := strings.Fields(protected)
	if len(fields) == 1 {
		return Author{Family: cleanBibTeXText(fields[0])}
	}
	family := cleanBibTeXText(fields[len(fields)-1])
	given := cleanBibTeXText(strings.Join(fields[:len(fields)-1], " "))
	return Author{Family: family, Given: given}
}

// balancedOuter 判断首尾花括号是否互相匹配（而不是两个独立的分组）。
func balancedOuter(s string) bool {
	depth := 0
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 && i != len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// protectBraces 把花括号替换成占位符，避免它们干扰逗号/空格的切分；
// cleanBibTeXText 会再把占位符去掉。
func protectBraces(s string) string {
	return strings.NewReplacer("{", "", "}", "").Replace(s)
}

// accentLetters 是常见重音命令覆盖的基字母。
var accentLetters = []string{"a", "e", "i", "o", "u", "A", "E", "I", "O", "U", "n", "c", "C"}

// accentMarks 把 LaTeX 的重音命令映射到 Unicode 组合附加符。
var accentMarks = map[rune]rune{
	'\'': '́', // 锐音符
	'`':  '̀', // 抑音符
	'"':  '̈', // 分音符
	'^':  '̂', // 抑扬符
	'~':  '̃', // 波浪符
}

// accentReplacer 把「命令 + 字母」直接替换成预组合字符，覆盖最常用的集合。
// 组合式替换覆盖不到的生僻字母会退化成去掉重音符号的基字母，
// 比保留 "\'x" 这样的原始标记更适合展示。
var accentReplacer = func() *strings.Replacer {
	pairs := make([]string, 0, len(accentMarks)*len(accentLetters)*2)
	for mark, combining := range accentMarks {
		for _, letter := range accentLetters {
			base := []rune(letter)[0]
			composed := string([]rune{base, combining})
			// 预组合字符（NFC）优先，拿不到时保留组合序列，浏览器仍能正常显示。
			if nfc, ok := nfcCompose(base, combining); ok {
				composed = nfc
			}
			prefix := `\` + string(mark)
			pairs = append(pairs,
				prefix+"{"+letter+"}", composed,
				prefix+letter, composed,
			)
		}
	}
	return strings.NewReplacer(pairs...)
}()

// nfcCompose 用一张小表覆盖拉丁字母的常用预组合形式。
// 不引入 golang.org/x/text/unicode/norm：只为一个展示函数增加依赖不划算。
func nfcCompose(base, combining rune) (string, bool) {
	table := map[[2]rune]string{
		{'a', '́'}: "á", {'e', '́'}: "é", {'i', '́'}: "í",
		{'o', '́'}: "ó", {'u', '́'}: "ú",
		{'A', '́'}: "Á", {'E', '́'}: "É", {'I', '́'}: "Í",
		{'O', '́'}: "Ó", {'U', '́'}: "Ú",
		{'a', '̀'}: "à", {'e', '̀'}: "è", {'i', '̀'}: "ì",
		{'o', '̀'}: "ò", {'u', '̀'}: "ù",
		{'a', '̈'}: "ä", {'e', '̈'}: "ë", {'i', '̈'}: "ï",
		{'o', '̈'}: "ö", {'u', '̈'}: "ü",
		{'A', '̈'}: "Ä", {'O', '̈'}: "Ö", {'U', '̈'}: "Ü",
		{'a', '̂'}: "â", {'e', '̂'}: "ê", {'i', '̂'}: "î",
		{'o', '̂'}: "ô", {'u', '̂'}: "û",
		{'n', '̃'}: "ñ", {'a', '̃'}: "ã", {'o', '̃'}: "õ",
		{'c', '́'}: "ć", {'C', '́'}: "Ć",
	}
	v, ok := table[[2]rune{base, combining}]
	return v, ok
}

// latexSpecial 处理非重音的命令，例如 \ss、\ae、\&。
var latexSpecial = strings.NewReplacer(
	`\ss`, "ß", `\o`, "ø", `\O`, "Ø", `\ae`, "æ", `\AE`, "Æ",
	`\c{c}`, "ç", `\c{C}`, "Ç",
	`\&`, "&", `\%`, "%", `\_`, "_", `\#`, "#", `\$`, "$",
)

// cleanBibTeXText 去掉 BibTeX 的保护性花括号并转换常见 LaTeX 重音。
//
// 数据库里存的是「可直接展示的文本」而不是原始标记：引用渲染在
// 前端和服务端都要用到，两边都做一遍转换既重复又容易不一致。
// raw_bibtex 字段仍保留原始文本，需要精确回放时以它为准。
func cleanBibTeXText(s string) string {
	s = strings.TrimSpace(s)
	s = accentReplacer.Replace(s)
	s = latexSpecial.Replace(s)
	var b strings.Builder
	b.Grow(len(s))
	skipCommand := false
	for _, r := range s {
		if skipCommand {
			// 未知的 LaTeX 命令（如 \H、\v）只保留其参数字母，
			// 去掉命令名本身：展示层不该出现反斜杠。
			if unicode.IsLetter(r) {
				continue
			}
			skipCommand = false
		}
		switch r {
		case '\\':
			skipCommand = true
		case '{', '}':
			continue
		case '~':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// isCJK 判断文本是否含中日韩字符，用于决定 GB/T 7714 的「等」还是 et al.
func isCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}
