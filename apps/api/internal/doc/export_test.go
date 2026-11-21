package doc

import (
	"strings"
	"testing"
)

// TestHTMLToMarkdown 覆盖 TipTap StarterKit 会产出的结构。
func TestHTMLToMarkdown(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string // 必须出现的片段
	}{
		{
			name: "标题与段落",
			html: `<h1>引言</h1><p>这是正文。</p>`,
			want: []string{"# 引言", "这是正文。"},
		},
		{
			name: "粗体与斜体",
			html: `<p>这是<strong>重点</strong>和<em>强调</em>。</p>`,
			want: []string{"**重点**", "*强调*"},
		},
		{
			name: "删除线",
			html: `<p><s>删掉</s></p>`,
			want: []string{"~~删掉~~"},
		},
		{
			name: "行内代码",
			html: `<p>调用 <code>fmt.Println</code> 输出。</p>`,
			want: []string{"`fmt.Println`"},
		},
		{
			name: "代码块带语言",
			html: `<pre><code class="language-go">func main() {}</code></pre>`,
			want: []string{"```go", "func main() {}"},
		},
		{
			name: "无序列表",
			html: `<ul><li>第一</li><li>第二</li></ul>`,
			want: []string{"- 第一", "- 第二"},
		},
		{
			name: "有序列表",
			html: `<ol><li>一</li><li>二</li></ol>`,
			want: []string{"1. 一", "2. 二"},
		},
		{
			name: "链接",
			html: `<p><a href="https://example.com">示例</a></p>`,
			want: []string{"[示例](https://example.com)"},
		},
		{
			name: "引用块",
			html: `<blockquote><p>引用的话</p></blockquote>`,
			want: []string{"> 引用的话"},
		},
		{
			name: "分割线",
			html: `<hr>`,
			want: []string{"---"},
		},
		{
			name: "特殊字符原样保留",
			html: `<p>a &lt; b &amp; c &gt; d</p>`,
			want: []string{"a < b & c > d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := parseHTML(tt.html)
			if err != nil {
				t.Fatalf("parseHTML: %v", err)
			}
			got := htmlToMarkdown(nodes)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("Markdown 缺少 %q\n实际输出:\n%s", want, got)
				}
			}
		})
	}
}

// TestHTMLToMarkdownNestedList 验证嵌套列表的缩进。
func TestHTMLToMarkdownNestedList(t *testing.T) {
	nodes, err := parseHTML(`<ul><li>外层<ul><li>内层</li></ul></li></ul>`)
	if err != nil {
		t.Fatalf("parseHTML: %v", err)
	}
	got := htmlToMarkdown(nodes)
	if !strings.Contains(got, "  - 内层") {
		t.Fatalf("嵌套列表缺少缩进\n实际输出:\n%s", got)
	}
}

// TestHTMLToLaTeX 验证 LaTeX 输出可编译的基本结构。
func TestHTMLToLaTeX(t *testing.T) {
	nodes, err := parseHTML(`<h1>标题</h1><p>正文 & 符号</p>`)
	if err != nil {
		t.Fatalf("parseHTML: %v", err)
	}
	got := htmlToLaTeX(nodes, "论文标题")

	required := []string{
		`\documentclass`,
		`\usepackage[UTF8]{ctex}`,
		`\title{论文标题}`,
		`\begin{document}`,
		`\end{document}`,
		`\section{标题}`,
		`正文 \& 符号`, // & 必须转义，否则 LaTeX 编译失败
	}
	for _, want := range required {
		if !strings.Contains(got, want) {
			t.Fatalf("LaTeX 缺少 %q\n实际输出:\n%s", want, got)
		}
	}
}

// TestEscapeLaTeX 验证特殊字符转义。
//
// 断言期望的转义序列逐一出现。不用「输出里不能出现 #1」这类否定断言：
// 转义结果 \#1 本身就包含子串 #1，否定断言会误伤自己。
func TestEscapeLaTeX(t *testing.T) {
	got := escapeLaTeX(`50% & $5 #1 a_b {c} ~d ^e \f`)
	want := []string{
		`50\%`, `\&`, `\$5`, `\#1`, `a\_b`, `\{c\}`, `\textasciitilde{}d`, `\textasciicircum{}e`,
	}
	for _, fragment := range want {
		if !strings.Contains(got, fragment) {
			t.Fatalf("缺少转义 %q\n输出: %s", fragment, got)
		}
	}
	// 反斜杠必须恰好转义一次：先转义 \ 会把后插入的 \& 再转一次，变成 \\&。
	if strings.Count(got, `\textbackslash{}`) != 1 {
		t.Fatalf("反斜杠应恰好转义一次，输出: %s", got)
	}
}

// TestHTMLToDocx 验证 DOCX 是合法的 zip 且包含必需的部件。
func TestHTMLToDocx(t *testing.T) {
	nodes, err := parseHTML(`<h1>标题</h1><p>正文</p>`)
	if err != nil {
		t.Fatalf("parseHTML: %v", err)
	}
	content, err := htmlToDocx(nodes)
	if err != nil {
		t.Fatalf("htmlToDocx: %v", err)
	}
	// zip 魔数 PK\x03\x04
	if len(content) < 4 || content[0] != 'P' || content[1] != 'K' {
		t.Fatal("输出不是 zip 格式")
	}
	// document.xml 应包含正文文本
	text := string(content)
	if !strings.Contains(text, "word/document.xml") {
		t.Fatal("缺少 word/document.xml")
	}
}

// TestSafeFilename 验证文件名清洗：去掉路径分隔符与控制字符，保留中文。
func TestSafeFilename(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"中文保留", "多模态综述", "多模态综述"},
		{"去掉路径分隔符", "../../etc/passwd", "etcpasswd"},
		{"去掉引号", `a"b`, "ab"},
		{"空标题退化", "   ", "document"},
		{"过长截断", strings.Repeat("a", 100), strings.Repeat("a", 60)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeFilename(tt.title); got != tt.want {
				t.Fatalf("safeFilename(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

// TestCodeLanguage 验证从 class 里提取语言名。
func TestCodeLanguage(t *testing.T) {
	nodes, err := parseHTML(`<pre><code class="language-python hljs">x = 1</code></pre>`)
	if err != nil {
		t.Fatalf("parseHTML: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("解析结果为空")
	}
	if got := codeLanguage(nodes[0]); got != "python" {
		t.Fatalf("codeLanguage = %q, want python", got)
	}
}
