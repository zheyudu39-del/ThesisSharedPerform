package doc

import (
	"fmt"
	"regexp"
	"strings"
)

// LaTeX 表格 → HTML 表格。
//
// 只处理 tabular / tabularx 的常见形态：列格式声明、& 分列、\\ 分行、
// \hline / \toprule 等横线命令。multicolumn / multirow 这类跨行跨列
// 的写法会降级（内容保留在单元格里，但合并关系丢失）——
// 完整支持需要一套 LaTeX 的列模型，与「把正文带进编辑器」的目标不成比例。

var (
	// \begin{tabular}{cc} 或 \begin{tabularx}{\linewidth}{cc}
	tabularBegin = regexp.MustCompile(`\\begin\{tabularx?\}(\{[^{}]*\})?\{[^{}]*\}`)
	tabularEnd   = regexp.MustCompile(`\\end\{tabularx?\}`)
	// 横线命令：hline / toprule / midrule / bottomrule / cline{...}
	tableRule = regexp.MustCompile(`\\(hline|toprule|midrule|bottomrule|cline\{[^{}]*\})`)
)

// latexTableToHTML 把一个含 tabular 环境的块转成 <table>。
// 无法解析时返回 ok=false，由调用方降级为纯文本。
func latexTableToHTML(chunk string) (string, bool) {
	start := tabularBegin.FindStringIndex(chunk)
	if start == nil {
		return "", false
	}
	end := tabularEnd.FindStringIndex(chunk)
	if end == nil || end[0] < start[1] {
		return "", false
	}

	body := chunk[start[1]:end[0]]

	// 去掉横线命令，它们不影响行列结构。
	body = tableRule.ReplaceAllString(body, "")

	// 按 \\ 分行。注意 \\[2pt] 这种带可选间距的写法也要吃掉。
	rows := regexp.MustCompile(`\\\\\s*(\[[^\]]*\])?`).Split(body, -1)

	var b strings.Builder
	b.WriteString("<table><tbody>")
	wroteRow := false

	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		// 单元格按 & 切分。\& 是字面与号，不能当分隔符——
		// 先用占位符保护，切完再还原。
		const ampPlaceholder = "\x00AMP\x00"
		row = strings.ReplaceAll(row, `\&`, ampPlaceholder)
		cells := strings.Split(row, "&")

		b.WriteString("<tr>")
		for _, cell := range cells {
			// 还原成裸 &：转义交给 convertLatexInline 统一处理，
			// 这里再转一次会变成 &amp;amp;。
			cell = strings.TrimSpace(strings.ReplaceAll(cell, ampPlaceholder, "&"))
			// 单元格里可能有行内公式与强调，交给行内转换器。
			// 用空 warnings 切片：表格单元格里的未识别命令不值得单独提示，
			// 否则一个 20 行的表格会刷出几十条重复警告。
			var ignored []string
			html := convertLatexInline(cell, &ignored)
			fmt.Fprintf(&b, "<td>%s</td>", html)
		}
		b.WriteString("</tr>")
		wroteRow = true
	}
	b.WriteString("</tbody></table>")

	if !wroteRow {
		return "", false
	}
	return b.String(), true
}
