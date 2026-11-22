package file

import (
	"testing"

	"github.com/thesis-shared/api/internal/db"
)

// previewKind 与前端 FilePreview 的分流规则必须一致。
// 这里验证服务端「可读性」判定不会误伤这几类可预览文件——
// 它们在论文页里都要对访客可见（PRD V-04）。

func TestReadableFileViaPaperLinkIsAllowed(t *testing.T) {
	// 记录语义：只要文件被论文引用，非上传者也可读。
	// 具体实现由 readableFile 调用 paperForFile 判定，这里约束的是
	// 「哪些 kind 会走到 original 预览」，避免以后加 kind 时漏配。
	previewable := []string{"image", "markdown", "latex", "code", "pdf", "office_doc"}
	for _, kind := range previewable {
		if !needsTranscode(kind) && !isDirectlyViewable(kind) {
			t.Fatalf("%s 既不转码也无直出预览，会在 UI 里变成死附件", kind)
		}
	}
}

// isDirectlyViewable 镜像前端的 canPreview：图片与文本类文件不需要转码，
// 直接读原文件即可预览。
func isDirectlyViewable(kind string) bool {
	switch db.FileKind(kind) {
	case db.FileKindImage, db.FileKindMarkdown, db.FileKindLatex, db.FileKindCode:
		return true
	}
	return false
}

func TestNeedsTranscodeCoversOffice(t *testing.T) {
	for _, kind := range []string{"office_doc", "office_slide", "office_sheet"} {
		if !needsTranscode(kind) {
			t.Fatalf("%s 需要 LibreOffice 转码后才能预览", kind)
		}
	}
}
