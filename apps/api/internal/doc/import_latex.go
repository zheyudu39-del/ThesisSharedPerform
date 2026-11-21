package doc

import (
	"fmt"
	"regexp"
	"strings"
)

// LaTeX → HTML 转换。
//
// 这是一个「够用」的转换器，不是完整的 TeX 实现：目标是把论文正文
// （章节、段落、强调、列表、公式占位）带进协同编辑器，而不是排版渲染。
// 完整的 TeX 引擎有几百个宏包，在服务端跑一遍既不现实也没必要——
// 用户要的是「能接着改」，不是「像素级还原」。
//
// 不支持的命令一律降级为纯文本，并把命令名记进 warning，
// 让用户知道哪些地方需要手工补。

var (
	// 注释：未转义的 % 到行尾。\% 是字面百分号，不能误删。
	latexComment = regexp.MustCompile(`(?m)(^|[^\\])%.*$`)

	latexSection = regexp.MustCompile(`\\(part|chapter|section|subsection|subsubsection|paragraph)\*?\{`)
	latexEnv     = regexp.MustCompile(`\\begin\{(\w+)\}`)
	latexCommand = regexp.MustCompile(`\\[a-zA-Z]+`)
)

// latexToHTML 把 LaTeX 源码转成 HTML 片段。
func latexToHTML(source string) (string, []string, error) {
	warnings := make([]string, 0, 3)

	// 1) 去掉注释。保留 \% 这类转义。
	source = latexComment.ReplaceAllStringFunc(source, func(match string) string {
		if idx := strings.IndexByte(match, '%'); idx >= 0 {
			return match[:idx]
		}
		return match
	})

	// 2) 只取 document 环境内的内容；没有 document 环境时按整篇处理
	//    （用户可能直接粘贴了一个片段）。
	body := source
	if start := strings.Index(source, `\begin{document}`); start >= 0 {
		body = source[start+len(`\begin{document}`):]
		if end := strings.Index(body, `\end{document}`); end >= 0 {
			body = body[:end]
		}
	}

	// 3) 先处理 verbatim 环境：里面的内容一个字都不能改。
	//    用占位符替换，最后再还原，避免被后续的转义与命令处理破坏。
	var verbatims []string
	body = replaceVerbatim(body, &verbatims)

	html := convertLatexBlocks(body, &warnings)

	// 4) 还原 verbatim。
	for i, code := range verbatims {
		html = strings.ReplaceAll(html, fmt.Sprintf("\x00VERBATIM%d\x00", i), "<pre><code>"+escapeHTML(code)+"</code></pre>")
	}

	if strings.TrimSpace(stripTags(html)) == "" {
		return "", warnings, fmt.Errorf("%w: 文档没有可导入的正文", ErrValidation)
	}

	if len(warnings) > 0 {
		warnings = dedupeStrings(warnings)
	}
	return html, warnings, nil
}

// replaceVerbatim 把 verbatim / lstlisting 环境抽出来换成占位符。
func replaceVerbatim(source string, out *[]string) string {
	for _, env := range []string{"verbatim", "lstlisting", "minted"} {
		begin := `\begin{` + env + `}`
		end := `\end{` + env + `}`
		for {
			start := strings.Index(source, begin)
			if start < 0 {
				break
			}
			stop := strings.Index(source[start:], end)
			if stop < 0 {
				break
			}
			code := source[start+len(begin) : start+stop]
			code = strings.TrimPrefix(code, "\n")
			placeholder := fmt.Sprintf("\x00VERBATIM%d\x00", len(*out))
			*out = append(*out, code)
			source = source[:start] + placeholder + source[start+stop+len(end):]
		}
	}
	return source
}

// convertLatexBlocks 按块级结构切分并转换。
func convertLatexBlocks(source string, warnings *[]string) string {
	var b strings.Builder

	// 按空行切段：LaTeX 里空行就是段落分隔。
	chunks := regexp.MustCompile(`\n\s*\n`).Split(source, -1)
	for _, chunk := range chunks {
		trimmed := strings.TrimSpace(chunk)
		if trimmed == "" {
			continue
		}
		writeLatexChunk(&b, trimmed, warnings)
	}
	return b.String()
}

func writeLatexChunk(b *strings.Builder, chunk string, warnings *[]string) {
	// 列表环境要先于章节命令判断：一个 chunk 可能以 \section 开头、
	// 但正文里含列表环境，若先按章节整块处理，列表就丢了。
	if strings.HasPrefix(chunk, `egin{itemize}`) || strings.HasPrefix(chunk, `egin{enumerate}`) {
		writeLatexList(b, chunk, warnings)
		return
	}

	// 章节命令
	if m := latexSection.FindStringSubmatch(chunk); m != nil {
		level := sectionLevel(m[1])
		title := extractBraced(chunk, strings.Index(chunk, m[0])+len(m[0])-1)
		if title != "" {
			fmt.Fprintf(b, "<h%d>%s</h%d>", level, convertLatexInline(title, warnings), level)
			// 标题后面的内容可能还有段落、列表、引用——
			// 递归按块处理，而不是整段当成一个 <p>。
			rest := strings.TrimSpace(chunk[strings.Index(chunk, m[0])+len(m[0])+len(title)+1:])
			if rest != "" {
				b.WriteString(convertLatexBlocks(rest, warnings))
			}
			return
		}
	}

	// 列表环境
	if strings.HasPrefix(chunk, `\begin{itemize}`) || strings.HasPrefix(chunk, `\begin{enumerate}`) {
		writeLatexList(b, chunk, warnings)
		return
	}

	// 引用环境
	if strings.HasPrefix(chunk, `\begin{quote}`) || strings.HasPrefix(chunk, `\begin{quotation}`) {
		inner := strings.TrimPrefix(chunk, `\begin{quote}`)
		inner = strings.TrimPrefix(inner, `\begin{quotation}`)
		inner = strings.TrimSuffix(strings.TrimSpace(inner), `\end{quote}`)
		inner = strings.TrimSuffix(strings.TrimSpace(inner), `\end{quotation}`)
		fmt.Fprintf(b, "<blockquote><p>%s</p></blockquote>", convertLatexInline(inner, warnings))
		return
	}

	// 表格环境：tabular / tabularx 转成标准 HTML 表格。
	// 放在公式之前判断：表格里的单元格可能含 $...$，先按表格切开更稳妥。
	if strings.Contains(chunk, `\begin{tabular}`) || strings.Contains(chunk, `\begin{tabularx}`) {
		if html, ok := latexTableToHTML(chunk); ok {
			b.WriteString(html)
			return
		}
		*warnings = append(*warnings, "部分 LaTeX 表格无法解析，已按纯文本保留")
	}

	// 公式环境：转成编辑器认识的 blockMath 节点，由 KaTeX 渲染。
	//
	// 用 <block-math> 这个标签名而不是 <div data-type="blockMath">：
	// TipTap 的数学扩展按节点名解析，标签名必须与扩展注册的名字对应
	// （blockMath / inlineMath），否则内容会被当成普通文本。
	if env := latexEnv.FindStringSubmatch(chunk); env != nil {
		switch env[1] {
		case "equation", "equation*", "gather", "gather*", "displaymath", "math":
			inner := stripEnvironment(chunk, env[1])
			fmt.Fprintf(b, "<block-math>%s</block-math>", escapeHTML(strings.TrimSpace(inner)))
			return
		case "align", "align*":
			// align 是多行公式，KaTeX 的 aligned 环境能正确渲染。
			inner := stripEnvironment(chunk, env[1])
			fmt.Fprintf(b, "<block-math>\\begin{aligned}%s\\end{aligned}</block-math>", escapeHTML(strings.TrimSpace(inner)))
			return
		}
	}

	// 普通段落
	html := convertLatexInline(chunk, warnings)
	if strings.TrimSpace(html) != "" {
		fmt.Fprintf(b, "<p>%s</p>", html)
	}
}

func writeLatexList(b *strings.Builder, chunk string, warnings *[]string) {
	ordered := strings.HasPrefix(chunk, `\begin{enumerate}`)
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	inner := chunk
	for _, env := range []string{"itemize", "enumerate"} {
		inner = strings.TrimPrefix(inner, `\begin{`+env+`}`)
		inner = strings.TrimSuffix(strings.TrimSpace(inner), `\end{`+env+`}`)
	}

	items := regexp.MustCompile(`\\item\b`).Split(inner, -1)
	b.WriteString("<" + tag + ">")
	for _, item := range items {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		fmt.Fprintf(b, "<li>%s</li>", convertLatexInline(text, warnings))
	}
	b.WriteString("</" + tag + ">")
}

// convertLatexInline 处理行内标记。
func convertLatexInline(text string, warnings *[]string) string {
	// 记录遇到的不支持命令，让用户知道哪些没转过来。
	for _, cmd := range latexCommand.FindAllString(text, -1) {
		switch cmd {
		case `\textbf`, `\textit`, `\emph`, `\underline`, `\texttt`, `\textsc`,
			`\text`, `\cite`, `\ref`, `\label`, `\footnote`, `\url`, `\href`,
			`\item`, `\par`, `\newline`, `\quad`, `\qquad`, `\hspace`, `\vspace`,
			`\noindent`, `\centering`, `\small`, `\large`, `\Large`, `\normalsize`,
			`\begin`, `\end`, `\includegraphics`, `\caption`, `\maketitle`:
			// 已知命令，下面逐个处理或安全丢弃。
		default:
			*warnings = append(*warnings, "部分 LaTeX 命令未转换："+cmd)
		}
	}

	text = regexp.MustCompile(`\\textbf\{([^{}]*)\}`).ReplaceAllString(text, "<strong>$1</strong>")
	text = regexp.MustCompile(`\\textit\{([^{}]*)\}`).ReplaceAllString(text, "<em>$1</em>")
	text = regexp.MustCompile(`\\emph\{([^{}]*)\}`).ReplaceAllString(text, "<em>$1</em>")
	text = regexp.MustCompile(`\\underline\{([^{}]*)\}`).ReplaceAllString(text, "<u>$1</u>")
	text = regexp.MustCompile(`\\texttt\{([^{}]*)\}`).ReplaceAllString(text, "<code>$1</code>")
	text = regexp.MustCompile(`\\text\{([^{}]*)\}`).ReplaceAllString(text, "$1")

	// 行内公式：转成 inlineMath 节点，由 KaTeX 渲染。
	text = regexp.MustCompile(`\$([^$]+)\$`).ReplaceAllString(text, "<inline-math>$1</inline-math>")

	// 换行与间距。
	text = strings.ReplaceAll(text, `\\`, "<br>")
	text = strings.ReplaceAll(text, `\newline`, "<br>")
	text = strings.ReplaceAll(text, `\par`, "\n\n")
	text = regexp.MustCompile(`\\[a-zA-Z]+\*?(\[[^\]]*\])?(\{[^{}]*\})?`).ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, `\&`, "&amp;")
	text = strings.ReplaceAll(text, `\%`, "%")
	text = strings.ReplaceAll(text, `\_`, "_")
	text = strings.ReplaceAll(text, `\#`, "#")
	text = strings.ReplaceAll(text, `\$`, "$")
	text = strings.ReplaceAll(text, `\{`, "{")
	text = strings.ReplaceAll(text, `\}`, "}")
	text = strings.ReplaceAll(text, `\ `, " ")

	// 转义裸的 HTML 特殊字符，但保留我们刚插入的标签。
	text = escapeExceptTags(text)
	return strings.TrimSpace(text)
}

// escapeExceptTags 转义 HTML 特殊字符，但放过已知的行内标签。
func escapeExceptTags(s string) string {
	known := []string{
		"<strong>", "</strong>", "<em>", "</em>", "<u>", "</u>",
		"<code>", "</code>", "<br>", "<inline-math>", "</inline-math>",
	}
	// 用占位符保护标签，转义后再还原。
	for i, tag := range known {
		s = strings.ReplaceAll(s, tag, fmt.Sprintf("\x00TAG%d\x00", i))
	}
	s = escapeHTML(s)
	for i, tag := range known {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00TAG%d\x00", i), tag)
	}
	return s
}

func sectionLevel(cmd string) int {
	switch cmd {
	case "part", "chapter":
		return 1
	case "section":
		return 1
	case "subsection":
		return 2
	case "subsubsection":
		return 3
	case "paragraph":
		return 4
	}
	return 1
}

// extractBraced 从 openBrace 位置的 { 开始取出配对花括号里的内容。
func extractBraced(s string, openBrace int) string {
	if openBrace < 0 || openBrace >= len(s) || s[openBrace] != '{' {
		return ""
	}
	depth := 0
	for i := openBrace; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[openBrace+1 : i]
			}
		}
	}
	return ""
}

func stripEnvironment(chunk, env string) string {
	inner := strings.TrimPrefix(chunk, `\begin{`+env+`}`)
	return strings.TrimSuffix(strings.TrimSpace(inner), `\end{`+env+`}`)
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(html string) string { return tagPattern.ReplaceAllString(html, "") }

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
