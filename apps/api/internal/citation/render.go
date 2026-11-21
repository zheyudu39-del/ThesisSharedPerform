package citation

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Format 是支持的参考文献格式。
type Format string

const (
	// FormatGBT7714 是国标 GB/T 7714-2015，中文学位论文的默认格式。
	FormatGBT7714 Format = "gbt7714"
	// FormatAPA 是 APA 第 7 版。
	FormatAPA Format = "apa"
	// FormatIEEE 是 IEEE 会议/期刊格式。
	FormatIEEE Format = "ieee"
)

// ParseFormat 解析外部传入的格式名，未知值返回 false。
//
// 宽容处理常见的书写差异（大小写、空格、斜杠、连字符）：
// 前端切换格式时可能把 "GB/T 7714" 直接塞进 query，用户不该因为
// 一个分隔符而看到 422。
func ParseFormat(raw string) (Format, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.NewReplacer(" ", "", "/", "", "-", "", "_", "").Replace(normalized)
	switch Format(normalized) {
	case FormatGBT7714:
		return FormatGBT7714, true
	case FormatAPA:
		return FormatAPA, true
	case FormatIEEE:
		return FormatIEEE, true
	}
	return "", false
}

// RenderBibliography 把引用条目渲染成指定格式的参考文献表。
//
// 排序规则：
//   - GB/T 7714 与 APA：按第一作者姓氏（family）的字母序，同姓再按年份与题名，
//     保证输出稳定；
//   - IEEE：按传入顺序编号。传进来的顺序就是条目入库顺序（近似正文引用
//     顺序）；如果调用方没有顺序信息，传空切片即可得到空结果，而不是
//     悄悄换成另一种排序——编号与正文标注必须一一对应。
//
// 渲染是纯函数：不读数据库、不依赖时间，同一份输入永远得到同一份输出。
func RenderBibliography(format Format, entries []Entry) ([]BibliographyEntry, error) {
	switch format {
	case FormatGBT7714, FormatAPA, FormatIEEE:
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}

	// IEEE 保序，其余两种按姓氏排序。复制一份再排，避免修改调用方的切片。
	ordered := make([]Entry, len(entries))
	copy(ordered, entries)
	if format != FormatIEEE {
		sort.SliceStable(ordered, func(i, j int) bool {
			return sortKey(ordered[i]) < sortKey(ordered[j])
		})
	}

	result := make([]BibliographyEntry, 0, len(ordered))
	for i, entry := range ordered {
		var text string
		switch format {
		case FormatGBT7714:
			text = renderGB(entry)
		case FormatAPA:
			text = renderAPA(entry)
		case FormatIEEE:
			text = renderIEEE(entry, i+1)
		}
		result = append(result, BibliographyEntry{
			CiteKey: entry.CiteKey,
			Text:    text,
			SortKey: sortKey(entry),
		})
	}
	return result, nil
}

// sortKey 构造稳定的排序键：第一作者姓氏（小写）+ 年份 + 题名。
// 用零填充的年份保证字符串比较与数值比较一致（未知年份排最前）。
func sortKey(e Entry) string {
	family := strings.ToLower(firstAuthorFamily(e.Authors))
	year := "0000"
	if e.Year > 0 {
		year = fmt.Sprintf("%04d", e.Year)
	}
	return family + "\x00" + year + "\x00" + strings.ToLower(e.Title)
}

func firstAuthorFamily(authors []Author) string {
	for _, a := range authors {
		if a.Family != "" {
			return a.Family
		}
		if a.Literal != "" {
			return a.Literal
		}
		if a.Given != "" {
			return a.Given
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 作者名渲染
// ---------------------------------------------------------------------------

// initialsWithDots 把 "Ashish" 转成 "A."，"John Ronald" 转成 "J. R."。
// 已经带点的缩写（"A."）保持原样，避免出现 "A.."。
func initialsWithDots(given string) string {
	fields := strings.Fields(given)
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		// 中文姓名不做缩写：单字名的「名」被拆成首字母会失去意义。
		if isCJK(field) {
			parts = append(parts, field)
			continue
		}
		runes := []rune(field)
		for i, r := range runes {
			if unicode.IsLetter(r) {
				parts = append(parts, string(unicode.ToUpper(r))+".")
				// 只取首字母；连字符姓名的第二段在 APA 里通常也缩写。
				if i+1 < len(runes) && runes[i+1] == '-' {
					continue
				}
				break
			}
		}
	}
	return strings.Join(parts, " ")
}

// initialsNoDots 与 initialsWithDots 相同，但不加句点（GB/T 风格）。
func initialsNoDots(given string) string {
	fields := strings.Fields(given)
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if isCJK(field) {
			parts = append(parts, field)
			continue
		}
		for _, r := range field {
			if unicode.IsLetter(r) {
				parts = append(parts, string(unicode.ToUpper(r)))
				break
			}
		}
	}
	return strings.Join(parts, " ")
}

// gbName 输出 GB/T 7714 的作者名：西文「姓 首字母」，中文「姓名」。
func gbName(a Author) string {
	if a.Literal != "" {
		return a.Literal
	}
	if isCJK(a.Family) || isCJK(a.Given) {
		return a.Family + a.Given
	}
	family := strings.ToUpper(a.Family)
	if a.Given == "" {
		return family
	}
	return strings.TrimSpace(family + " " + initialsNoDots(a.Given))
}

// apaName 输出 APA 的作者名：Family, A. B.
func apaName(a Author) string {
	if a.Literal != "" {
		return a.Literal
	}
	if isCJK(a.Family) || isCJK(a.Given) {
		return a.Family + a.Given
	}
	if a.Given == "" {
		return a.Family
	}
	return a.Family + ", " + initialsWithDots(a.Given)
}

// ieeeName 输出 IEEE 的作者名：A. B. Family
func ieeeName(a Author) string {
	if a.Literal != "" {
		return a.Literal
	}
	if isCJK(a.Family) || isCJK(a.Given) {
		return a.Family + a.Given
	}
	if a.Given == "" {
		return a.Family
	}
	return initialsWithDots(a.Given) + " " + a.Family
}

// joinGB 按 GB/T 7714 规则连接作者：超过 3 人只列前 3 人，
// 中文条目加「等」，西文条目加 "et al."。
func joinGB(authors []Author) string {
	if len(authors) == 0 {
		return ""
	}
	names := make([]string, 0, 4)
	for _, a := range authors {
		names = append(names, gbName(a))
	}
	if len(names) > 3 {
		names = names[:3]
		marker := "et al."
		if isCJK(firstAuthorFamily(authors)) {
			marker = "等"
		}
		return strings.Join(names, ", ") + ", " + marker
	}
	return strings.Join(names, ", ")
}

// joinAPA 按 APA 第 7 版规则连接作者：
// 20 人以内全部列出，最后两人之间用 "&"；超过 20 人列前 19 人 + "…" + 末位。
func joinAPA(authors []Author) string {
	if len(authors) == 0 {
		return ""
	}
	names := make([]string, 0, len(authors))
	for _, a := range authors {
		names = append(names, apaName(a))
	}
	if len(names) == 1 {
		return names[0]
	}
	if len(names) > 20 {
		return strings.Join(names[:19], ", ") + ", … " + names[len(names)-1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", & " + names[len(names)-1]
}

// joinIEEE 按 IEEE 习惯连接作者：超过 6 人用 et al.。
func joinIEEE(authors []Author) string {
	if len(authors) == 0 {
		return ""
	}
	names := make([]string, 0, len(authors))
	for _, a := range authors {
		names = append(names, ieeeName(a))
	}
	if len(names) > 6 {
		return strings.Join(names[:6], ", ") + ", et al."
	}
	if len(names) == 1 {
		return names[0]
	}
	if len(names) == 2 {
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// ---------------------------------------------------------------------------
// 单条文献渲染
// ---------------------------------------------------------------------------

// gbTypeCode 返回 GB/T 7714 的文献类型标识。
// 有 URL 但没有期刊/出版信息的 misc 视为电子资源 [EB/OL]。
func gbTypeCode(e Entry) string {
	switch e.EntryType {
	case "article":
		return "J"
	case "inproceedings":
		return "C"
	case "book":
		return "M"
	default:
		if e.URL != "" && e.Container == "" {
			return "EB/OL"
		}
		return "Z"
	}
}

// renderGB 渲染 GB/T 7714-2015 格式。
//
// 目标形状：作者. 题名[J]. 刊名, 年, 卷(期): 页.
// 缺失字段整段省略，而不是留下 ", , " 这样的空槽。
func renderGB(e Entry) string {
	var b strings.Builder
	if authors := joinGB(e.Authors); authors != "" {
		b.WriteString(authors)
		b.WriteString(". ")
	}
	b.WriteString(strings.TrimSuffix(e.Title, "."))
	b.WriteString("[" + gbTypeCode(e) + "]")

	// 期刊/会议/出版社 + 年 + 卷(期)
	segments := make([]string, 0, 3)
	if e.Container != "" {
		segments = append(segments, e.Container)
	}
	if e.Year > 0 {
		segments = append(segments, strconv.Itoa(int(e.Year)))
	}
	volIssue := e.Volume
	if e.Issue != "" {
		if volIssue != "" {
			volIssue += "(" + e.Issue + ")"
		} else {
			volIssue = "(" + e.Issue + ")"
		}
	}
	if volIssue != "" {
		segments = append(segments, volIssue)
	}
	if len(segments) > 0 {
		b.WriteString(". ")
		b.WriteString(strings.Join(segments, ", "))
	}
	if e.Pages != "" {
		b.WriteString(": " + e.Pages)
	}
	b.WriteString(".")

	if e.DOI != "" {
		b.WriteString(" DOI: " + e.DOI + ".")
	} else if e.URL != "" {
		b.WriteString(" " + e.URL + ".")
	}
	return b.String()
}

// renderAPA 渲染 APA 第 7 版格式。
//
// 目标形状：Author, A. A. (Year). Title. Journal, Vol(Issue), pages. DOI
// 未知年份写 (n.d.)；没有期刊信息时用出版社替代。
func renderAPA(e Entry) string {
	var b strings.Builder
	if authors := joinAPA(e.Authors); authors != "" {
		b.WriteString(authors)
		b.WriteString(" ")
	}
	if e.Year > 0 {
		fmt.Fprintf(&b, "(%d). ", e.Year)
	} else {
		b.WriteString("(n.d.). ")
	}
	title := strings.TrimSpace(e.Title)
	b.WriteString(title)
	if !strings.HasSuffix(title, ".") && !strings.HasSuffix(title, "?") && !strings.HasSuffix(title, "!") {
		b.WriteString(".")
	}

	if e.Container != "" {
		b.WriteString(" " + e.Container)
		if e.Volume != "" {
			b.WriteString(", " + e.Volume)
		}
		if e.Issue != "" {
			b.WriteString("(" + e.Issue + ")")
		}
		if e.Pages != "" {
			b.WriteString(", " + e.Pages)
		}
		b.WriteString(".")
	} else if e.Publisher != "" {
		b.WriteString(" " + strings.TrimSuffix(e.Publisher, ".") + ".")
	}

	if e.DOI != "" {
		b.WriteString(" https://doi.org/" + strings.TrimPrefix(e.DOI, "https://doi.org/"))
	} else if e.URL != "" {
		b.WriteString(" " + e.URL)
	}
	return strings.TrimSpace(b.String())
}

// renderIEEE 渲染 IEEE 格式，n 是条目序号。
//
// 目标形状：A. A. Author, "Title," Journal, vol. X, no. Y, pp. Z, Year.
// 书籍不使用引号，改用斜体（纯文本下省略），并直接跟出版社与年份。
func renderIEEE(e Entry, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] ", n)

	authors := joinIEEE(e.Authors)
	title := strings.TrimSuffix(e.Title, ".")

	if e.EntryType == "book" {
		if authors != "" {
			b.WriteString(authors + ", ")
		}
		b.WriteString(title + ".")
		if e.Publisher != "" {
			b.WriteString(" " + e.Publisher + ",")
		}
		if e.Year > 0 {
			fmt.Fprintf(&b, " %d.", e.Year)
		}
		return b.String()
	}

	if authors != "" {
		b.WriteString(authors + ", ")
	}
	b.WriteString(`"` + title + `,"`)

	parts := make([]string, 0, 5)
	if e.Container != "" {
		parts = append(parts, e.Container)
	}
	if e.Volume != "" {
		parts = append(parts, "vol. "+e.Volume)
	}
	if e.Issue != "" {
		parts = append(parts, "no. "+e.Issue)
	}
	if e.Pages != "" {
		parts = append(parts, "pp. "+e.Pages)
	}
	if e.Year > 0 {
		parts = append(parts, strconv.Itoa(int(e.Year)))
	}
	if len(parts) > 0 {
		b.WriteString(" " + strings.Join(parts, ", "))
	}
	b.WriteString(".")

	if e.DOI != "" {
		b.WriteString(" doi: " + e.DOI + ".")
	}
	return b.String()
}
