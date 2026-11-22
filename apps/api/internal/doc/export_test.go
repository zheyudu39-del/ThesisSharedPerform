package doc

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

// unzip 读出一个 docx 包内的文件内容。
func unzip(t *testing.T, data []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("打开 docx: %v", err)
	}
	for _, f := range reader.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开 %s: %v", name, err)
		}
		defer func() { _ = rc.Close() }()
		content, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("读取 %s: %v", name, err)
		}
		return string(content)
	}
	t.Fatalf("docx 缺少 %s", name)
	return ""
}

func renderDocx(t *testing.T, htmlText string) []byte {
	t.Helper()
	nodes, err := parseHTML(htmlText)
	if err != nil {
		t.Fatalf("parseHTML: %v", err)
	}
	data, err := htmlToDocx(nodes)
	if err != nil {
		t.Fatalf("htmlToDocx: %v", err)
	}
	return data
}

// TestDocxIncludesStyles 验证样式部件与关系都存在。
// 缺 styles.xml 时 Word / LibreOffice 会静默把标题、代码块退化成正文，
// 不会有任何报错——所以只能靠结构测试兜住。
func TestDocxIncludesStyles(t *testing.T) {
	data := renderDocx(t, `<h1>标题</h1><p>正文</p>`)

	styles := unzip(t, data, "word/styles.xml")
	for _, styleID := range []string{"Heading1", "Heading2", "Heading3", "Normal", "Quote", "Code", "ListParagraph"} {
		if !strings.Contains(styles, `w:styleId="`+styleID+`"`) {
			t.Fatalf("styles.xml 缺少样式 %s", styleID)
		}
	}

	// 关系没声明的话，样式部件不会被加载。
	rels := unzip(t, data, "word/_rels/document.xml.rels")
	if !strings.Contains(rels, "styles.xml") {
		t.Fatal("document.xml.rels 未声明 styles 关系")
	}

	// Content_Types 也要登记，否则 Office 可能拒绝打开。
	ct := unzip(t, data, "[Content_Types].xml")
	if !strings.Contains(ct, "/word/styles.xml") {
		t.Fatal("[Content_Types].xml 未登记 styles.xml")
	}
}

// TestDocxCodeBlockPreservesIndent 验证代码块的缩进与多行结构不被吃掉。
// 早期实现会 TrimSpace 每一行，导致缩进丢失、空行消失、多行挤成一行的文本。
func TestDocxCodeBlockPreservesIndent(t *testing.T) {
	data := renderDocx(t, "<pre><code>func main() {\n    fmt.Println(&#34;hi&#34;)\n}</code></pre>")
	document := unzip(t, data, "word/document.xml")

	if !strings.Contains(document, "    fmt.Println") {
		t.Fatalf("代码块缩进丢失：%s", document)
	}
	// 三行各自独立成段：数一下 Code 样式的段落数。
	if n := strings.Count(document, `w:val="Code"`); n != 3 {
		t.Fatalf("代码块应为 3 段，实际 %d 段：%s", n, document)
	}
}

// TestDocxCodeBlockBlankLine 验证代码块中间的空白行被保留成换行 run。
func TestDocxCodeBlockBlankLine(t *testing.T) {
	data := renderDocx(t, "<pre><code>a\n\nb</code></pre>")
	document := unzip(t, data, "word/document.xml")
	if !strings.Contains(document, "<w:br/>") {
		t.Fatalf("空行未保留：%s", document)
	}
}

// TestDocxEscapesContent 验证正文里的标签不会被当成 XML 结构。
func TestDocxEscapesContent(t *testing.T) {
	data := renderDocx(t, `<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>`)
	document := unzip(t, data, "word/document.xml")
	if strings.Contains(document, "<script>") {
		t.Fatalf("内容未转义：%s", document)
	}
}
