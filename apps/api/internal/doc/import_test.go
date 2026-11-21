package doc

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildDocx 构造一个最小但合法的 docx（只含 word/document.xml）。
func buildDocx(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	file, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	document := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>` + body + `</w:body></w:document>`
	if _, err := file.Write([]byte(document)); err != nil {
		t.Fatalf("write document.xml: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestDocxToHTML(t *testing.T) {
	content := buildDocx(t, `
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>引言</w:t></w:r></w:p>
<w:p><w:r><w:t>这是正文。</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>加粗</w:t></w:r><w:r><w:t>普通</w:t></w:r></w:p>
`)
	html, warnings, err := docxToHTML(content)
	if err != nil {
		t.Fatalf("docxToHTML: %v", err)
	}
	for _, want := range []string{"<h1>引言</h1>", "<p>这是正文。</p>", "<strong>加粗</strong>普通"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML 缺少 %q\n实际：%s", want, html)
		}
	}
	if len(warnings) != 0 {
		t.Fatalf("不应有 warning，实际：%v", warnings)
	}
}

// TestDocxToHTMLEscapes 验证文档内容不会被当成标签解析。
func TestDocxToHTMLEscapes(t *testing.T) {
	content := buildDocx(t, `<w:p><w:r><w:t>&lt;script&gt;alert(1)&lt;/script&gt;</w:t></w:r></w:p>`)
	html, _, err := docxToHTML(content)
	if err != nil {
		t.Fatalf("docxToHTML: %v", err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("脚本标签未被转义：%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("转义结果不对：%s", html)
	}
}

func TestDocxToHTMLRejectsNonDocx(t *testing.T) {
	if _, _, err := docxToHTML([]byte("not a zip")); err == nil {
		t.Fatal("非 zip 输入应当报错")
	}
}

func TestLatexToHTML(t *testing.T) {
	source := `\documentclass{article}
\begin{document}
\section{引言}
这是第一段，包含 \textbf{加粗} 与 \emph{强调}。

\subsection{方法}
\begin{itemize}
\item 第一点
\item 第二点
\end{itemize}

\begin{quote}
引用的内容
\end{quote}
\end{document}`

	html, _, err := latexToHTML(source)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}

	want := []string{
		"<h1>引言</h1>",
		"<strong>加粗</strong>",
		"<em>强调</em>",
		"<h2>方法</h2>",
		"<ul>",
		"<li>第一点</li>",
		"<blockquote>",
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("HTML 缺少 %q\n实际：%s", fragment, html)
		}
	}
}

// TestLatexComments 验证注释被剔除，但 \% 这类转义保留。
func TestLatexComments(t *testing.T) {
	source := `\begin{document}
正文 % 这是注释
百分比是 50\% 整。
\end{document}`
	html, _, err := latexToHTML(source)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if strings.Contains(html, "这是注释") {
		t.Fatalf("注释未被剔除：%s", html)
	}
	if !strings.Contains(html, "50%") {
		t.Fatalf("转义百分号丢失：%s", html)
	}
}

// TestLatexVerbatim 验证代码环境原样保留。
func TestLatexVerbatim(t *testing.T) {
	source := `\begin{document}
\begin{verbatim}
func main() { fmt.Println("<b>") }
\end{verbatim}
\end{document}`
	html, _, err := latexToHTML(source)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if !strings.Contains(html, "<pre><code>") {
		t.Fatalf("缺少代码块：%s", html)
	}
	// 代码里的 <b> 必须被转义，不能变成标签。
	if strings.Contains(html, "<b>") {
		t.Fatalf("代码内容未被转义：%s", html)
	}
}

// TestLatexEquation 验证公式以源码形式保留而不是丢失。
func TestLatexEquation(t *testing.T) {
	source := `\begin{document}
\begin{equation}
E = mc^2
\end{equation}
\end{document}`
	html, warnings, err := latexToHTML(source)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if !strings.Contains(html, "E = mc^2") {
		t.Fatalf("公式内容丢失：%s", html)
	}
	if len(warnings) == 0 {
		t.Fatal("应提示公式以源码形式保留")
	}
}

// TestLatexWithoutDocumentEnv 验证片段（无 document 环境）也能导入。
func TestLatexWithoutDocumentEnv(t *testing.T) {
	html, _, err := latexToHTML(`\section{标题}
正文段落。`)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if !strings.Contains(html, "<h1>标题</h1>") {
		t.Fatalf("片段导入失败：%s", html)
	}
}

func TestInferImportFormat(t *testing.T) {
	tests := map[string]string{
		"paper.docx":  "docx",
		"PAPER.DOCX":  "docx",
		"thesis.tex":  "tex",
		"notes.latex": "tex",
		"unknown.pdf": "",
	}
	for name, want := range tests {
		if got := inferImportFormat(name); got != want {
			t.Fatalf("inferImportFormat(%q) = %q, want %q", name, got, want)
		}
	}
}
