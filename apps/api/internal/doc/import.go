package doc

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// 导入能力：把外部文件（Word / LaTeX）转成 HTML，交给前端写进协同文档。
//
// 为什么是「转 HTML」而不是在服务端直接写 Y.Doc：
// TipTap 的协作扩展把正文存在 Y.Doc 的 XmlFragment("default") 里，
// 服务端要写它就必须自己构造 ProseMirror 的节点树——那等于在 Go 里
// 重新实现一个编辑器 schema，且与前端 TipTap 版本强耦合。
//
// 走 HTML 这条路，前端只需一次 editor.commands.setContent(html)，
// 内容就进了 Y.Doc，所有协作者实时看到。转换逻辑留在 Go 侧，
// 前端不必为每种格式写解析器。

// maxImportBytes 限制导入文件体积。
// 与上传上限一致，避免把整个文件读进内存时被超大文件拖垮。
const maxImportBytes = 32 << 20

// ImportResult 是一次导入的产物。
type ImportResult struct {
	HTML  string `json:"html"`
	Title string `json:"title"`
	// Format 回显识别出的格式，便于前端提示。
	Format string `json:"format"`
	// Warnings 是转换中的降级说明（例如「图片已忽略」）。
	// 如实返回而不是静默丢弃，用户才知道哪些内容没带过来。
	Warnings []string `json:"warnings"`
}

// ImportFile 把文件内容转成可写入协同文档的 HTML。
//
// format 取 "docx" 或 "tex"；传空时按文件名后缀推断。
func (s *Service) ImportFile(ctx context.Context, userID, docID uuid.UUID, filename string, content []byte, format string) (ImportResult, error) {
	// 与导出对称：导入也是写正文，要求 editor 及以上。
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanEdit); err != nil {
		return ImportResult{}, err
	}
	if len(content) == 0 {
		return ImportResult{}, fmt.Errorf("%w: 文件内容为空", ErrValidation)
	}
	if len(content) > maxImportBytes {
		return ImportResult{}, fmt.Errorf("%w: 文件超过 %d MB", ErrValidation, maxImportBytes>>20)
	}

	if format == "" {
		format = inferImportFormat(filename)
	}

	switch format {
	case "docx":
		html, warnings, err := docxToHTML(content)
		if err != nil {
			return ImportResult{}, err
		}
		return ImportResult{HTML: html, Title: stripExt(filename), Format: "docx", Warnings: warnings}, nil
	case "tex":
		html, warnings, err := latexToHTML(string(content))
		if err != nil {
			return ImportResult{}, err
		}
		return ImportResult{HTML: html, Title: stripExt(filename), Format: "tex", Warnings: warnings}, nil
	default:
		return ImportResult{}, fmt.Errorf("%w: 不支持的导入格式 %q（仅支持 docx 与 tex）", ErrValidation, format)
	}
}

func inferImportFormat(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".docx"):
		return "docx"
	case strings.HasSuffix(lower, ".tex"), strings.HasSuffix(lower, ".latex"):
		return "tex"
	}
	return ""
}

func stripExt(filename string) string {
	if i := strings.LastIndexByte(filename, '.'); i > 0 {
		return filename[:i]
	}
	return filename
}

// ---------------------------------------------------------------------------
// DOCX → HTML
// ---------------------------------------------------------------------------

// docxToHTML 解析 Word 文档的正文段落与基础样式。
//
// 只处理 OOXML 的 word/document.xml，不处理图片、表格、脚注——
// 这些在协同编辑器里需要额外的节点类型，超出「把正文带过来」的范围。
// 遇到不支持的内容会记录 warning，让用户知道哪些没转过来。
func docxToHTML(content []byte) (string, []string, error) {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", nil, fmt.Errorf("%w: 文件不是有效的 docx", ErrValidation)
	}

	var documentXML []byte
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			reader, err := file.Open()
			if err != nil {
				return "", nil, fmt.Errorf("%w: 无法读取文档正文", ErrValidation)
			}
			documentXML, err = io.ReadAll(io.LimitReader(reader, maxImportBytes))
			_ = reader.Close()
			if err != nil {
				return "", nil, fmt.Errorf("%w: 读取文档正文失败", ErrValidation)
			}
			break
		}
	}
	if documentXML == nil {
		return "", nil, fmt.Errorf("%w: 不是 Word 文档（缺少 word/document.xml）", ErrValidation)
	}

	doc, err := parseWordDocument(documentXML)
	if err != nil {
		return "", nil, fmt.Errorf("%w: 文档结构无法解析", ErrValidation)
	}

	var b strings.Builder
	var warnings []string
	hasImage := false
	hasTable := false

	for _, para := range doc.Body.Paragraphs {
		style := para.styleID()
		html := renderWordParagraph(para)
		if strings.TrimSpace(html) == "" {
			continue
		}
		switch {
		case isHeadingStyle(style):
			level := headingLevel(style)
			fmt.Fprintf(&b, "<h%d>%s</h%d>", level, html, level)
		case strings.Contains(strings.ToLower(style), "quote"):
			fmt.Fprintf(&b, "<blockquote><p>%s</p></blockquote>", html)
		default:
			fmt.Fprintf(&b, "<p>%s</p>", html)
		}
		if strings.Contains(html, "<img") {
			hasImage = true
		}
	}
	for _, table := range doc.Body.Tables {
		if len(table.Rows) > 0 {
			hasTable = true
		}
	}

	if hasImage {
		warnings = append(warnings, "文档中的图片未导入（协同编辑器暂不支持内嵌图片）")
	}
	if hasTable {
		warnings = append(warnings, "文档中的表格未导入（可粘贴为纯文本后手动整理）")
	}

	html := b.String()
	if strings.TrimSpace(html) == "" {
		return "", warnings, fmt.Errorf("%w: 文档没有可导入的正文", ErrValidation)
	}
	return html, warnings, nil
}

// wordDocument 是 word/document.xml 的最小投影。
// 只声明我们真正读取的部分，其余元素由 encoding/xml 忽略。
type wordDocument struct {
	XMLName xml.Name `xml:"document"`
	Body    wordBody `xml:"body"`
}

type wordBody struct {
	Paragraphs []wordParagraph `xml:"p"`
	Tables     []wordTable     `xml:"tbl"`
}

type wordTable struct {
	Rows []struct{} `xml:"tr"`
}

type wordParagraph struct {
	Props wordParagraphProps `xml:"pPr"`
	Runs  []wordRun          `xml:"r"`
}

type wordParagraphProps struct {
	Style wordStyle `xml:"pStyle"`
}

type wordStyle struct {
	Val string `xml:"val,attr"`
}

type wordRun struct {
	Props wordRunProps `xml:"rPr"`
	Text  string       `xml:"t"`
	// 换行符 <w:br/>
	Breaks []struct{} `xml:"br"`
}

type wordRunProps struct {
	Bold   *struct{} `xml:"b"`
	Italic *struct{} `xml:"i"`
	Strike *struct{} `xml:"strike"`
	Code   *struct{} `xml:"rStyle"`
}

func parseWordDocument(data []byte) (wordDocument, error) {
	var doc wordDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return wordDocument{}, err
	}
	return doc, nil
}

func (p wordParagraph) styleID() string { return p.Props.Style.Val }

// isHeadingStyle 识别 Word 内置标题样式。
// 中文版 Word 会把样式名存成 "1"/"2" 或 "Heading1"/"标题 1"，
// 这里统一按「包含 heading 或以 1-6 结尾」判断。
func isHeadingStyle(style string) bool {
	lower := strings.ToLower(style)
	if strings.Contains(lower, "heading") || strings.Contains(style, "标题") {
		return true
	}
	return len(lower) == 1 && lower[0] >= '1' && lower[0] <= '6'
}

func headingLevel(style string) int {
	for _, r := range style {
		if r >= '1' && r <= '6' {
			return int(r - '0')
		}
	}
	return 1
}

// renderWordParagraph 把一个段落渲染成行内 HTML。
func renderWordParagraph(para wordParagraph) string {
	var b strings.Builder
	for _, run := range para.Runs {
		text := escapeHTML(run.Text)
		if text == "" && len(run.Breaks) == 0 {
			continue
		}
		// 按「先内后外」包裹：粗体在最里，斜体、删除线依次包外层，
		// 与 HTML 语义一致（虽然视觉上无差别，但序列化回来时顺序稳定）。
		if run.Props.Bold != nil {
			text = "<strong>" + text + "</strong>"
		}
		if run.Props.Italic != nil {
			text = "<em>" + text + "</em>"
		}
		if run.Props.Strike != nil {
			text = "<s>" + text + "</s>"
		}
		b.WriteString(text)
		for range run.Breaks {
			b.WriteString("<br>")
		}
	}
	return b.String()
}

// escapeHTML 转义 HTML 特殊字符，防止文档内容被当作标签解析。
func escapeHTML(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}
