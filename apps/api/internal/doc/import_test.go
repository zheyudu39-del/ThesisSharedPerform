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
	result, err := docxToHTML(content)
	if err != nil {
		t.Fatalf("docxToHTML: %v", err)
	}
	html := result.HTML
	warnings := result.Warnings
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
	parsed, err := docxToHTML(content)
	if err != nil {
		t.Fatalf("docxToHTML: %v", err)
	}
	html := parsed.HTML
	if strings.Contains(html, "<script>") {
		t.Fatalf("脚本标签未被转义：%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("转义结果不对：%s", html)
	}
}

func TestDocxToHTMLRejectsNonDocx(t *testing.T) {
	if _, err := docxToHTML([]byte("not a zip")); err == nil {
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
	html, _, err := latexToHTML(source)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if !strings.Contains(html, "<block-math>E = mc^2</block-math>") {
		t.Fatalf("公式未转成 block-math：%s", html)
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

// TestLatexInlineMath 验证行内公式转成 inline-math 节点。
func TestLatexInlineMath(t *testing.T) {
	html, _, err := latexToHTML(`\begin{document}
质能方程 $E=mc^2$ 是相对论的核心。
\end{document}`)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if !strings.Contains(html, "<inline-math>E=mc^2</inline-math>") {
		t.Fatalf("行内公式未转换：%s", html)
	}
}

// TestLatexAlign 验证多行公式包进 aligned 环境。
func TestLatexAlign(t *testing.T) {
	html, _, err := latexToHTML(`\begin{document}
\begin{align}
a &= b + c \
d &= e
\end{align}
\end{document}`)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if !strings.Contains(html, "begin{aligned}") {
		t.Fatalf("align 未包进 aligned：%s", html)
	}
}

// TestLatexTable 验证 tabular 转成 HTML 表格。
func TestLatexTable(t *testing.T) {
	html, _, err := latexToHTML(`\begin{document}
\begin{tabular}{cc}
\hline
姓名 & 分数 \\
\hline
张三 & 95 \\
李四 & 88 \\
\hline
\end{tabular}
\end{document}`)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	for _, want := range []string{"<table>", "<td>姓名</td>", "<td>95</td>", "<td>李四</td>"} {
		if !strings.Contains(html, want) {
			t.Fatalf("表格缺少 %q：%s", want, html)
		}
	}
	// 横线命令不应残留
	if strings.Contains(html, "hline") {
		t.Fatalf("hline 未被清理：%s", html)
	}
}

// TestLatexTableEscapedAmp 验证 \& 不被当成列分隔符。
func TestLatexTableEscapedAmp(t *testing.T) {
	html, _, err := latexToHTML(`\begin{document}
\begin{tabular}{cc}
A \& B & C \\
\end{tabular}
\end{document}`)
	if err != nil {
		t.Fatalf("latexToHTML: %v", err)
	}
	if !strings.Contains(html, "<td>A &amp; B</td>") {
		t.Fatalf("转义与号处理错误：%s", html)
	}
}

// TestDocxTable 验证 Word 表格转成 HTML 表格。
func TestDocxTable(t *testing.T) {
	content := buildDocx(t, `
<w:tbl>
  <w:tr>
    <w:tc><w:p><w:r><w:t>姓名</w:t></w:r></w:p></w:tc>
    <w:tc><w:p><w:r><w:t>分数</w:t></w:r></w:p></w:tc>
  </w:tr>
  <w:tr>
    <w:tc><w:p><w:r><w:t>张三</w:t></w:r></w:p></w:tc>
    <w:tc><w:p><w:r><w:t>95</w:t></w:r></w:p></w:tc>
  </w:tr>
</w:tbl>`)
	parsed, err := docxToHTML(content)
	if err != nil {
		t.Fatalf("docxToHTML: %v", err)
	}
	html := parsed.HTML
	for _, want := range []string{"<table>", "<td>姓名</td>", "<td>95</td>"} {
		if !strings.Contains(html, want) {
			t.Fatalf("Word 表格缺少 %q：%s", want, html)
		}
	}
}

// TestDocxImagePlaceholder 验证正文里的图片变成占位符。
func TestDocxImagePlaceholder(t *testing.T) {
	content := buildDocx(t, `
<w:p><w:r><w:t>前</w:t></w:r></w:p>
<w:p><w:r><w:drawing><wp:inline xmlns:wp="x"><a:graphic xmlns:a="y">
<a:graphicData><a:blip r:embed="rId9"/></a:graphicData>
</a:graphic></wp:inline></w:drawing></w:r></w:p>`)
	result, err := docxToHTML(content)
	if err != nil {
		t.Fatalf("docxToHTML: %v", err)
	}
	if !strings.Contains(result.HTML, imgPlaceholder("rId9")) {
		t.Fatalf("图片占位符缺失：%q", result.HTML)
	}
}

// TestDocxImageExtraction 验证图片能从压缩包里抽出来。
func TestDocxImageExtraction(t *testing.T) {
	// 1x1 PNG
	png := []byte("\x89PNG\r\n\x1a\n")
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	doc, _ := archive.Create("word/document.xml")
	_, _ = doc.Write([]byte(`<?xml version="1.0"?><w:document xmlns:w="w" xmlns:r="r"><w:body><w:p><w:r><w:drawing><wp:inline><a:graphic><a:graphicData><a:blip r:embed="rId7"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p></w:body></w:document>`))
	rels, _ := archive.Create("word/_rels/document.xml.rels")
	_, _ = rels.Write([]byte(`<?xml version="1.0"?><Relationships><Relationship Id="rId7" Type="http://x/relationships/image" Target="media/pic.png"/></Relationships>`))
	img, _ := archive.Create("word/media/pic.png")
	_, _ = img.Write(png)
	_ = archive.Close()

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	images, _, err := extractDocxImages(reader, []byte(`<a:blip r:embed="rId7"/>`))
	if err != nil {
		t.Fatalf("extractDocxImages: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("应抽出 1 张图片，实际 %d", len(images))
	}
	if images[0].mime != "image/png" {
		t.Fatalf("mime = %q", images[0].mime)
	}
}

// TestImageMIME 验证按扩展名与文件头识别格式。
func TestImageMIME(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"a.png", nil, "image/png"},
		{"a.JPG", nil, "image/jpeg"},
		{"a.emf", nil, ""},                 // 不支持的矢量格式
		{"unknown", []byte("\x89PNG"), "image/png"}, // 靠文件头
		{"unknown", []byte("GIF89a"), "image/gif"},
	}
	for _, tt := range tests {
		if got := imageMIME(tt.name, tt.data); got != tt.want {
			t.Fatalf("imageMIME(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
