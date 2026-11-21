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
		parsed, err := docxToHTML(content)
		if err != nil {
			return ImportResult{}, err
		}
		warnings := parsed.Warnings

		// 图片：上传到对象存储后，把正文里的占位符换成稳定地址。
		// 地址形如 /api/v1/documents/{docID}/assets/{assetID}——
		// 不放预签名 URL，因为文档是长期的，预签名会过期。
		if len(parsed.Images) > 0 {
			mapping, imageWarnings, err := s.persistDocImages(ctx, userID, docID, parsed.Images)
			if err != nil {
				return ImportResult{}, err
			}
			warnings = append(warnings, imageWarnings...)
			html := imgPlaceholderRegexp.ReplaceAllStringFunc(parsed.HTML, func(placeholder string) string {
				sub := imgPlaceholderRegexp.FindStringSubmatch(placeholder)
				if len(sub) < 2 {
					return ""
				}
				assetID, ok := mapping[sub[1]]
				if !ok {
					// 这张图没上传成功：整段占位符去掉，避免正文里留下乱码。
					return ""
				}
				return fmt.Sprintf(`<img src="/api/v1/documents/%s/assets/%s" alt="文档图片">`, docID, assetID)
			})
			parsed.HTML = html
		} else if strings.Contains(parsed.HTML, imgPlaceholderPrefix) {
			// 抽不出图片（例如只有 EMF），占位符要清掉。
			parsed.HTML = imgPlaceholderRegexp.ReplaceAllString(parsed.HTML, "")
		}

		return ImportResult{HTML: parsed.HTML, Title: stripExt(filename), Format: "docx", Warnings: warnings}, nil
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

// docxResult 是 docx 解析的中间产物。
//
// 图片不直接转成 <img src>：地址要等图片上传到对象存储后才知道，
// 而解析阶段还没有 docID。因此正文里先放占位符，由 ImportFile 统一替换。
type docxResult struct {
	HTML   string
	Images []docxImage
	// Warnings 是内容层面的降级提示（与图片上传失败无关）。
	Warnings []string
}

// docxToHTML 解析 Word 文档的正文段落、基础样式、表格与图片。
//
// 图片以占位符 \x00IMG:{relID}\x00 出现在 HTML 里，由调用方替换成
// 真实的资源地址。脚注、批注、文本框不在处理范围内。
func docxToHTML(content []byte) (docxResult, error) {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return docxResult{}, fmt.Errorf("%w: 文件不是有效的 docx", ErrValidation)
	}

	var documentXML []byte
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			reader, err := file.Open()
			if err != nil {
				return docxResult{}, fmt.Errorf("%w: 无法读取文档正文", ErrValidation)
			}
			documentXML, err = io.ReadAll(io.LimitReader(reader, maxImportBytes))
			_ = reader.Close()
			if err != nil {
				return docxResult{}, fmt.Errorf("%w: 读取文档正文失败", ErrValidation)
			}
			break
		}
	}
	if documentXML == nil {
		return docxResult{}, fmt.Errorf("%w: 不是 Word 文档（缺少 word/document.xml）", ErrValidation)
	}

	doc, err := parseWordDocument(documentXML)
	if err != nil {
		return docxResult{}, fmt.Errorf("%w: 文档结构无法解析", ErrValidation)
	}

	var b strings.Builder
	var warnings []string

	for _, para := range doc.Body.Paragraphs {
		style := para.styleID()
		html := renderWordParagraph(para)
		// 只有图片、没有文字的段落也要保留——否则图片会整段消失。
		if strings.TrimSpace(html) == "" && !strings.Contains(html, imgPlaceholderPrefix) {
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
	}

	// 表格：转成编辑器能识别的 HTML <table>。
	// 单元格里的段落合并成一段文本（Word 的一个单元格可以有多个段落，
	// 但表格单元格在编辑器里只有一个文本块）。
	for _, table := range doc.Body.Tables {
		html, ok := wordTableToHTML(table)
		if !ok {
			continue
		}
		b.WriteString(html)
	}

	images, imageWarnings, err := extractDocxImages(archive, documentXML)
	if err != nil {
		return docxResult{}, err
	}
	warnings = append(warnings, imageWarnings...)

	html := b.String()
	if strings.TrimSpace(stripTags(html)) == "" && !strings.Contains(html, imgPlaceholderPrefix) {
		return docxResult{}, fmt.Errorf("%w: 文档没有可导入的正文", ErrValidation)
	}
	return docxResult{HTML: html, Images: images, Warnings: warnings}, nil
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
	Rows []wordTableRow `xml:"tr"`
}

type wordTableRow struct {
	Cells []wordTableCell `xml:"tc"`
}

type wordTableCell struct {
	Paragraphs []wordParagraph `xml:"p"`
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
	// 图片：<w:drawing><wp:inline><a:graphic><a:blip r:embed="rIdN"/>
	// 只取 blip 上的 embed 属性——那是 Word 关联图片条目的方式。
	Drawings []wordDrawing `xml:"drawing"`
}

type wordDrawing struct {
	Inline *struct {
		Graphic struct {
			Data struct {
				Blip struct {
					Embed string `xml:"embed,attr"`
				} `xml:"blip"`
			} `xml:"graphicData"`
		} `xml:"graphic"`
	} `xml:"inline"`
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
		// 图片先输出占位符：地址要等上传完成后才知道。
		for _, drawing := range run.Drawings {
			if drawing.Inline == nil {
				continue
			}
			relID := drawing.Inline.Graphic.Data.Blip.Embed
			if relID != "" {
				b.WriteString(imgPlaceholder(relID))
			}
		}

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

// wordTableToHTML 把一个 Word 表格转成 HTML <table>。
//
// Word 的表格没有表头概念（只有「首行重复」这种排版属性），
// 因此统一用 <td>。编辑器里用户可以自己把首行改成表头。
func wordTableToHTML(table wordTable) (string, bool) {
	if len(table.Rows) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("<table><tbody>")
	wroteRow := false
	for _, row := range table.Rows {
		if len(row.Cells) == 0 {
			continue
		}
		b.WriteString("<tr>")
		for _, cell := range row.Cells {
			// 一个单元格可能有多个段落；用空格拼接，保留文字但不保留分段。
			parts := make([]string, 0, len(cell.Paragraphs))
			for _, para := range cell.Paragraphs {
				if text := strings.TrimSpace(stripTags(renderWordParagraph(para))); text != "" {
					parts = append(parts, text)
				}
			}
			fmt.Fprintf(&b, "<td>%s</td>", escapeHTML(strings.Join(parts, " ")))
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
