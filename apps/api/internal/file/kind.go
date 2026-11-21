package file

import (
	"errors"
	"path"
	"strings"

	"github.com/thesis-shared/api/internal/db"
)

// ErrUnsupportedType 表示上传类型不在白名单内（PRD §6.4 / 技术架构 §4.1）。
var ErrUnsupportedType = errors.New("不支持的文件类型")

// MaxUploadBytes 是单文件上限：100MB（PRD 6.4 / 架构 §4.1 直传上限）。
const MaxUploadBytes int64 = 100 << 20

// allowedMIME 是允许上传的 MIME 白名单，值为对应的 file_kind。
// 只收录内联预览链路能处理的类型；可执行文件、压缩包、视频一律拒绝。
var allowedMIME = map[string]db.FileKind{
	"application/pdf": db.FileKindPdf,

	"image/png":  db.FileKindImage,
	"image/jpeg": db.FileKindImage,
	"image/webp": db.FileKindImage,

	"application/msword": db.FileKindOfficeDoc,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": db.FileKindOfficeDoc,

	"application/vnd.ms-powerpoint":                                             db.FileKindOfficeSlide,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": db.FileKindOfficeSlide,

	"application/vnd.ms-excel": db.FileKindOfficeSheet,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": db.FileKindOfficeSheet,

	"text/markdown": db.FileKindMarkdown,
	"text/plain":    db.FileKindMarkdown,

	// LaTeX 源文件。浏览器对 .tex 的 File.type 常见三种取值：
	// 空串、text/x-tex、application/x-tex（Chrome/Firefox 不一），
	// 前端会按扩展名兜底成 text/x-tex，这里三种都收。
	// file_kind 枚举里本来就有 latex，不需要加迁移。
	"text/x-tex":         db.FileKindLatex,
	"application/x-tex":  db.FileKindLatex,
	"application/x-latex": db.FileKindLatex,
}

// MIMEKind 返回 MIME 对应的 file_kind；不在白名单时 ok=false。
//
// 参数大小写与尾随参数（如 "text/plain; charset=utf-8"）都会被规范化，
// 因为浏览器的 File.type 并不保证干净。
func MIMEKind(mime string) (db.FileKind, bool) {
	normalized := normalizeMIME(mime)
	kind, ok := allowedMIME[normalized]
	return kind, ok
}

// AllowedMIME 报告该 MIME 是否在白名单内。
func AllowedMIME(mime string) bool {
	_, ok := MIMEKind(mime)
	return ok
}

func normalizeMIME(mime string) string {
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	return strings.ToLower(strings.TrimSpace(mime))
}

// extFor 根据 MIME 推断存储用的扩展名。扩展名只影响可读性与 Content-Type
// 嗅探，真实类型以签名后的 Content-Type 为准。
func extFor(mime string) string {
	switch normalizeMIME(mime) {
	case "application/pdf":
		return ".pdf"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.ms-powerpoint":
		return ".ppt"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	case "application/vnd.ms-excel":
		return ".xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "text/markdown":
		return ".md"
	case "text/plain":
		return ".txt"
	case "text/x-tex", "application/x-tex", "application/x-latex":
		return ".tex"
	default:
		return ""
	}
}

// safeExt 优先沿用原始文件名的扩展名（限长、限字符），
// 否则回退到 MIME 推断。避免把用户可控的完整文件名拼进存储 key。
func safeExt(originalName, mime string) string {
	ext := strings.ToLower(path.Ext(originalName))
	if len(ext) >= 2 && len(ext) <= 10 && isSimpleExt(ext) {
		return ext
	}
	return extFor(mime)
}

func isSimpleExt(ext string) bool {
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
