package doc

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// 导出格式。
const (
	FormatMarkdown = "md"
	FormatLaTeX    = "tex"
	FormatDocx     = "docx"
)

// maxExportHTML 限制前端提交的 HTML 体积。
// 协同编辑器里的一篇文档通常几十 KB；1 MB 足够覆盖长文，
// 又能挡住把导出接口当作上传通道的滥用。
const maxExportHTML = 1 << 20

// ExportResult 是一次导出的产物。
type ExportResult struct {
	Content     []byte
	Filename    string
	ContentType string
}

// ExportHTML 把编辑器提交的 HTML 转成目标格式。
//
// 为什么由前端提交 HTML：正文是 Yjs 二进制（CRDT），Go 侧没有成熟的
// 解析实现；而浏览器里 TipTap 已经持有一份完整的文档树，
// editor.getHTML() 是最可靠的序列化入口。服务端只做格式转换与权限校验，
// 不参与 CRDT 解析。
//
// 权限：PRD D-08 要求「无权限用户只读，且不可导出」，因此这里要求
// editor 及以上；viewer 返回 ErrForbidden。
func (s *Service) ExportHTML(ctx context.Context, userID, docID uuid.UUID, rawHTML, format string) (ExportResult, error) {
	// 先校验权限，再解析内容：避免无权限用户通过响应时间/错误码
	// 判断自己提交的 HTML 是否合法。
	_, err := s.requireRole(ctx, s.queries, userID, docID, CanEdit)
	if err != nil {
		return ExportResult{}, err
	}
	if len(rawHTML) == 0 {
		return ExportResult{}, fmt.Errorf("%w: 文档内容为空", ErrValidation)
	}
	if len(rawHTML) > maxExportHTML {
		return ExportResult{}, fmt.Errorf("%w: 文档内容过大，无法导出", ErrValidation)
	}

	title := s.documentTitle(ctx, docID)
	nodes, err := parseHTML(rawHTML)
	if err != nil {
		return ExportResult{}, fmt.Errorf("%w: 文档内容无法解析", ErrValidation)
	}

	switch format {
	case FormatMarkdown, "":
		return ExportResult{
			Content:     []byte(htmlToMarkdown(nodes)),
			Filename:    safeFilename(title) + ".md",
			ContentType: "text/markdown; charset=utf-8",
		}, nil
	case FormatLaTeX:
		return ExportResult{
			Content:     []byte(htmlToLaTeX(nodes, title)),
			Filename:    safeFilename(title) + ".tex",
			ContentType: "application/x-tex; charset=utf-8",
		}, nil
	case FormatDocx:
		content, err := htmlToDocx(nodes)
		if err != nil {
			return ExportResult{}, err
		}
		return ExportResult{
			Content:     content,
			Filename:    safeFilename(title) + ".docx",
			ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		}, nil
	default:
		return ExportResult{}, fmt.Errorf("%w: 不支持的导出格式 %q", ErrValidation, format)
	}
}

// documentTitle 读取文档标题用于文件名。查不到时退化为 "document"，
// 不因标题缺失让导出失败。
func (s *Service) documentTitle(ctx context.Context, docID uuid.UUID) string {
	row, err := s.queries.GetDocumentByID(ctx, uuidParam(docID))
	if err != nil {
		return "document"
	}
	if strings.TrimSpace(row.Title) == "" {
		return "document"
	}
	return row.Title
}

// safeFilename 把标题清洗成安全的文件名。
//
// 只保留字母、数字、汉字、空格与少量标点；路径分隔符与控制字符一律替换。
// 这既是防止 Content-Disposition 头注入，也避免客户端把它当成路径。
func safeFilename(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	for _, r := range strings.TrimSpace(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 0x4e00 && r <= 0x9fff: // 常用汉字
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "document"
	}
	runes := []rune(out)
	if len(runes) > 60 {
		out = string(runes[:60])
	}
	return out
}

// ---------------------------------------------------------------------------
// HTML 解析
// ---------------------------------------------------------------------------

// parseHTML 把 HTML 片段解析成节点列表。
//
// 用 x/net/html 而不是正则：TipTap 的输出包含嵌套列表、行内标记、
// 代码块等结构，正则无法正确还原层级。
func parseHTML(raw string) ([]*html.Node, error) {
	// 用一个 div 作为解析上下文，等价于浏览器的 innerHTML 语义：
	// 片段里的 <li>、<tr> 等会被包进合适的父元素，而不是被丢弃。
	ctxNode := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(raw), ctxNode)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// walk 深度优先遍历节点。返回 false 表示停止遍历。
func walk(node *html.Node, fn func(*html.Node) bool) bool {
	if !fn(node) {
		return false
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if !walk(child, fn) {
			return false
		}
	}
	return true
}

// textContent 提取节点的纯文本（不含子元素结构）。
func textContent(node *html.Node) string {
	var b strings.Builder
	walk(node, func(n *html.Node) bool {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		return true
	})
	return b.String()
}

// ---------------------------------------------------------------------------
// HTML → Markdown
// ---------------------------------------------------------------------------

// htmlToMarkdown 把节点列表转换为 Markdown。
//
// 只覆盖 TipTap StarterKit 会产出的结构：标题、段落、粗体/斜体/删除线、
// 行内代码、代码块、有序/无序列表、引用、分割线、链接、图片。
// 未知标签按「透明容器」处理——继续遍历子节点，不丢内容。
func htmlToMarkdown(nodes []*html.Node) string {
	var b strings.Builder
	for _, node := range nodes {
		renderMarkdownBlock(&b, node, 0)
	}
	// 折叠三个以上连续换行，保持 Markdown 干净。
	out := b.String()
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out) + "\n"
}

func renderMarkdownBlock(b *strings.Builder, node *html.Node, depth int) {
	if node.Type == html.TextNode {
		if text := strings.TrimSpace(node.Data); text != "" {
			b.WriteString(text)
			b.WriteString("\n\n")
		}
		return
	}
	if node.Type != html.ElementNode {
		return
	}

	switch node.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(node.Data[1] - '0')
		b.WriteString(strings.Repeat("#", level))
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(renderMarkdownInline(node)))
		b.WriteString("\n\n")
	case "p":
		text := strings.TrimSpace(renderMarkdownInline(node))
		if text != "" {
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	case "ul", "ol":
		renderMarkdownList(b, node, depth, node.Data == "ol")
		b.WriteString("\n")
	case "blockquote":
		// 只渲染子节点：把 node 自身再交给 htmlToMarkdown 会无限递归。
		var inner strings.Builder
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderMarkdownBlock(&inner, child, depth)
		}
		for _, line := range strings.Split(strings.TrimSpace(inner.String()), "\n") {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	case "pre":
		code := textContent(node)
		language := codeLanguage(node)
		b.WriteString("```")
		b.WriteString(language)
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(code, "\n"))
		b.WriteString("\n```\n\n")
	case "hr":
		b.WriteString("---\n\n")
	case "br":
		b.WriteString("\n")
	default:
		// 透明容器：继续渲染子节点。
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderMarkdownBlock(b, child, depth)
		}
	}
}

// renderMarkdownList 渲染有序/无序列表，支持嵌套。
func renderMarkdownList(b *strings.Builder, node *html.Node, depth int, ordered bool) {
	index := 1
	for item := node.FirstChild; item != nil; item = item.NextSibling {
		if item.Type != html.ElementNode || item.Data != "li" {
			continue
		}
		indent := strings.Repeat("  ", depth)
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", index)
			index++
		}

		// li 的第一个段落作为列表项正文，其余块（含嵌套列表）缩进续行。
		var inline strings.Builder
		var nested []*html.Node
		for child := item.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && (child.Data == "ul" || child.Data == "ol") {
				nested = append(nested, child)
				continue
			}
			inline.WriteString(renderMarkdownInline(child))
		}
		b.WriteString(indent)
		b.WriteString(marker)
		b.WriteString(strings.TrimSpace(inline.String()))
		b.WriteString("\n")

		for _, child := range nested {
			renderMarkdownList(b, child, depth+1, child.Data == "ol")
		}
	}
}

// renderMarkdownInline 渲染行内内容（不含块级换行）。
func renderMarkdownInline(node *html.Node) string {
	var b strings.Builder
	renderMarkdownInlineNode(&b, node)
	return b.String()
}

func renderMarkdownInlineNode(b *strings.Builder, node *html.Node) {
	switch node.Type {
	case html.TextNode:
		b.WriteString(node.Data)
		return
	case html.ElementNode:
		// 继续处理
	default:
		return
	}

	switch node.Data {
	case "strong", "b":
		inner := renderMarkdownInlineChildren(node)
		if inner != "" {
			b.WriteString("**")
			b.WriteString(inner)
			b.WriteString("**")
		}
	case "em", "i":
		inner := renderMarkdownInlineChildren(node)
		if inner != "" {
			b.WriteString("*")
			b.WriteString(inner)
			b.WriteString("*")
		}
	case "s", "del", "strike":
		inner := renderMarkdownInlineChildren(node)
		if inner != "" {
			b.WriteString("~~")
			b.WriteString(inner)
			b.WriteString("~~")
		}
	case "code":
		// 行内代码用反引号包裹；内容里的反引号用双反引号分隔。
		code := textContent(node)
		if strings.Contains(code, "`") {
			b.WriteString("`` ")
			b.WriteString(code)
			b.WriteString(" ``")
		} else {
			b.WriteString("`")
			b.WriteString(code)
			b.WriteString("`")
		}
	case "a":
		href := attr(node, "href")
		text := renderMarkdownInlineChildren(node)
		if text == "" {
			text = href
		}
		if href == "" {
			b.WriteString(text)
			return
		}
		b.WriteString("[")
		b.WriteString(text)
		b.WriteString("](")
		b.WriteString(href)
		b.WriteString(")")
	case "img":
		src := attr(node, "src")
		alt := attr(node, "alt")
		if src == "" {
			return
		}
		b.WriteString("![")
		b.WriteString(alt)
		b.WriteString("](")
		b.WriteString(src)
		b.WriteString(")")
	case "br":
		b.WriteString("\n")
	default:
		b.WriteString(renderMarkdownInlineChildren(node))
	}
}

func renderMarkdownInlineChildren(node *html.Node) string {
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		renderMarkdownInlineNode(&b, child)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// HTML → LaTeX
// ---------------------------------------------------------------------------

// htmlToLaTeX 生成可直接交给 pdflatex/xelatex 编译的完整文档。
//
// 中文支持靠 ctex 宏包；用户若用 pdflatex 需自行换用 ctexart 文档类。
// 这里选择 article + ctex 宏包，兼容性更好。
func htmlToLaTeX(nodes []*html.Node, title string) string {
	var body strings.Builder
	for _, node := range nodes {
		renderLaTeXBlock(&body, node, 0)
	}

	var b strings.Builder
	b.WriteString("\\documentclass[11pt]{article}\n")
	b.WriteString("\\usepackage[UTF8]{ctex}\n")
	b.WriteString("\\usepackage{hyperref}\n")
	b.WriteString("\\usepackage{graphicx}\n")
	b.WriteString("\\usepackage{enumitem}\n")
	b.WriteString("\\title{")
	b.WriteString(escapeLaTeX(title))
	b.WriteString("}\n")
	b.WriteString("\\date{}\n")
	b.WriteString("\\begin{document}\n")
	b.WriteString("\\maketitle\n\n")
	b.WriteString(body.String())
	b.WriteString("\n\\end{document}\n")
	return b.String()
}

func renderLaTeXBlock(b *strings.Builder, node *html.Node, depth int) {
	if node.Type == html.TextNode {
		if text := strings.TrimSpace(node.Data); text != "" {
			b.WriteString(escapeLaTeX(text))
			b.WriteString("\n\n")
		}
		return
	}
	if node.Type != html.ElementNode {
		return
	}

	switch node.Data {
	case "h1":
		b.WriteString("\\section{")
		b.WriteString(renderLaTeXInline(node))
		b.WriteString("}\n\n")
	case "h2":
		b.WriteString("\\subsection{")
		b.WriteString(renderLaTeXInline(node))
		b.WriteString("}\n\n")
	case "h3", "h4", "h5", "h6":
		b.WriteString("\\subsubsection{")
		b.WriteString(renderLaTeXInline(node))
		b.WriteString("}\n\n")
	case "p":
		text := strings.TrimSpace(renderLaTeXInline(node))
		if text != "" {
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	case "ul", "ol":
		env := "itemize"
		if node.Data == "ol" {
			env = "enumerate"
		}
		b.WriteString("\\begin{")
		b.WriteString(env)
		b.WriteString("}\n")
		for item := node.FirstChild; item != nil; item = item.NextSibling {
			if item.Type != html.ElementNode || item.Data != "li" {
				continue
			}
			b.WriteString("  \\item ")
			b.WriteString(strings.TrimSpace(renderLaTeXInline(item)))
			b.WriteString("\n")
			for child := item.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && (child.Data == "ul" || child.Data == "ol") {
					renderLaTeXBlock(b, child, depth+1)
				}
			}
		}
		b.WriteString("\\end{")
		b.WriteString(env)
		b.WriteString("}\n\n")
	case "blockquote":
		b.WriteString("\\begin{quote}\n")
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderLaTeXBlock(b, child, depth)
		}
		b.WriteString("\\end{quote}\n\n")
	case "pre":
		b.WriteString("\\begin{verbatim}\n")
		b.WriteString(textContent(node))
		b.WriteString("\n\\end{verbatim}\n\n")
	case "hr":
		b.WriteString("\\par\\noindent\\rule{\\linewidth}{0.4pt}\\par\n\n")
	default:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderLaTeXBlock(b, child, depth)
		}
	}
}

func renderLaTeXInline(node *html.Node) string {
	var b strings.Builder
	renderLaTeXInlineNode(&b, node)
	return b.String()
}

func renderLaTeXInlineNode(b *strings.Builder, node *html.Node) {
	switch node.Type {
	case html.TextNode:
		b.WriteString(escapeLaTeX(node.Data))
		return
	case html.ElementNode:
	default:
		return
	}

	switch node.Data {
	case "strong", "b":
		b.WriteString("\\textbf{")
		b.WriteString(renderLaTeXInlineChildren(node))
		b.WriteString("}")
	case "em", "i":
		b.WriteString("\\textit{")
		b.WriteString(renderLaTeXInlineChildren(node))
		b.WriteString("}")
	case "s", "del", "strike":
		b.WriteString("\\sout{")
		b.WriteString(renderLaTeXInlineChildren(node))
		b.WriteString("}")
	case "code":
		b.WriteString("\\texttt{")
		b.WriteString(escapeLaTeX(textContent(node)))
		b.WriteString("}")
	case "a":
		href := attr(node, "href")
		text := renderLaTeXInlineChildren(node)
		if href == "" {
			b.WriteString(text)
			return
		}
		b.WriteString("\\href{")
		b.WriteString(escapeLaTeXURL(href))
		b.WriteString("}{")
		b.WriteString(text)
		b.WriteString("}")
	case "br":
		b.WriteString("\\\\\n")
	default:
		b.WriteString(renderLaTeXInlineChildren(node))
	}
}

func renderLaTeXInlineChildren(node *html.Node) string {
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		renderLaTeXInlineNode(&b, child)
	}
	return b.String()
}

// escapeLaTeX 转义 LaTeX 特殊字符。
// 顺序重要：反斜杠必须最先处理，否则会把后续插入的转义序列再次转义。
func escapeLaTeX(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`&`, `\&`,
		`%`, `\%`,
		`$`, `\$`,
		`#`, `\#`,
		`_`, `\_`,
		`{`, `\{`,
		`}`, `\}`,
		`~`, `\textasciitilde{}`,
		`^`, `\textasciicircum{}`,
	)
	return replacer.Replace(s)
}

// escapeLaTeXURL 转义 URL 中的特殊字符（href 参数里 \& 是必需的）。
func escapeLaTeXURL(s string) string {
	return strings.NewReplacer(`\`, ``, `{`, `%7B`, `}`, `%7D`, `&`, `\&`, `#`, `\#`, `%`, `\%`).Replace(s)
}

// ---------------------------------------------------------------------------
// HTML → DOCX
// ---------------------------------------------------------------------------

// htmlToDocx 生成最小可用的 Word 文档（OOXML）。
//
// 为什么手写而不引第三方库：docx 的最小结构只有三个 XML 部件
// （[Content_Types].xml、_rels/.rels、word/document.xml），
// 用 archive/zip 直接拼装即可，不必为一个导出功能引入重量级依赖。
// 代价是样式支持有限——标题、段落、粗斜体、列表、代码块足够覆盖
// 论文协作稿的常见结构。
func htmlToDocx(nodes []*html.Node) ([]byte, error) {
	var body strings.Builder
	for _, node := range nodes {
		renderDocxBlock(&body, node)
	}

	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + body.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>` +
		`</w:body></w:document>`

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
		`</Relationships>`

	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	files := []struct {
		name    string
		content string
	}{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"word/document.xml", documentXML},
	}
	for _, file := range files {
		w, err := archive.Create(file.name)
		if err != nil {
			return nil, fmt.Errorf("生成 Word 文档失败: %w", err)
		}
		if _, err := w.Write([]byte(file.content)); err != nil {
			return nil, fmt.Errorf("生成 Word 文档失败: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("生成 Word 文档失败: %w", err)
	}
	return buf.Bytes(), nil
}

func renderDocxBlock(b *strings.Builder, node *html.Node) {
	if node.Type == html.TextNode {
		if text := strings.TrimSpace(node.Data); text != "" {
			writeDocxParagraph(b, text, "Normal", false)
		}
		return
	}
	if node.Type != html.ElementNode {
		return
	}

	switch node.Data {
	case "h1":
		writeDocxParagraph(b, textContent(node), "Heading1", false)
	case "h2":
		writeDocxParagraph(b, textContent(node), "Heading2", false)
	case "h3", "h4", "h5", "h6":
		writeDocxParagraph(b, textContent(node), "Heading3", false)
	case "p":
		writeDocxParagraph(b, textContent(node), "Normal", false)
	case "pre":
		for _, line := range strings.Split(textContent(node), "\n") {
			writeDocxParagraph(b, line, "Code", false)
		}
	case "ul", "ol":
		for item := node.FirstChild; item != nil; item = item.NextSibling {
			if item.Type == html.ElementNode && item.Data == "li" {
				writeDocxParagraph(b, "• "+strings.TrimSpace(textContent(item)), "ListParagraph", false)
			}
		}
	case "blockquote":
		writeDocxParagraph(b, textContent(node), "Quote", true)
	case "hr":
		writeDocxParagraph(b, "──────────", "Normal", false)
	default:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderDocxBlock(b, child)
		}
	}
}

// writeDocxParagraph 写一个段落。style 为 Word 内置样式名；
// italic 用于引用块。
func writeDocxParagraph(b *strings.Builder, text, style string, italic bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	b.WriteString(`<w:p><w:pPr><w:pStyle w:val="`)
	b.WriteString(xmlEscape(style))
	b.WriteString(`"/></w:pPr><w:r>`)
	if italic {
		b.WriteString(`<w:rPr><w:i/></w:rPr>`)
	}
	b.WriteString(`<w:t xml:space="preserve">`)
	b.WriteString(xmlEscape(text))
	b.WriteString(`</w:t></w:r></w:p>`)
}

// xmlEscape 转义 XML 文本内容。用 encoding/xml 而不是字符串替换，
// 保证与 XML 规范的实体集合一致。
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// attr 读取元素属性，不存在时返回空串。
func attr(node *html.Node, key string) string {
	for _, a := range node.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// codeLanguage 从 <pre><code class="language-go"> 里取出语言名，
// 用于 Markdown 代码块标注。取不到时返回空串。
func codeLanguage(pre *html.Node) string {
	var language string
	walk(pre, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "code" {
			class := attr(n, "class")
			for _, field := range strings.Fields(class) {
				if after, ok := strings.CutPrefix(field, "language-"); ok {
					language = after
					return false
				}
			}
		}
		return true
	})
	return language
}
