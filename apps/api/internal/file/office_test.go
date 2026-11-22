package file

import "testing"

func TestSourceExt(t *testing.T) {
	tests := map[string]string{
		"pdf":          ".pdf",
		"office_doc":   ".docx",
		"office_slide": ".pptx",
		"office_sheet": ".xlsx",
		"image":        ".bin",
		"markdown":     ".bin",
	}
	for kind, want := range tests {
		if got := sourceExt(kind); got != want {
			t.Fatalf("sourceExt(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestSourceExtMatchesLibreOffice 验证扩展名是 LibreOffice 能识别的类型。
// 这是转换能否成功的前提：扩展名未知会被当成「source file could not be loaded」。
func TestSourceExtMatchesLibreOffice(t *testing.T) {
	for _, kind := range []string{"office_doc", "office_slide", "office_sheet"} {
		ext := sourceExt(kind)
		if ext == ".bin" || ext == ".pdf" {
			t.Fatalf("%s 的扩展名 %q 无法让 LibreOffice 识别文档类型", kind, ext)
		}
	}
}

func TestNeedsTranscode(t *testing.T) {
	yes := []string{"pdf", "office_doc", "office_slide", "office_sheet"}
	no := []string{"image", "markdown", "latex", "code", "video", "archive", "other"}
	for _, kind := range yes {
		if !needsTranscode(kind) {
			t.Fatalf("%s 应当需要转码", kind)
		}
	}
	for _, kind := range no {
		if needsTranscode(kind) {
			t.Fatalf("%s 不应需要转码", kind)
		}
	}
}

// TestFilterFor 验证不同 Office 类型走各自的 LibreOffice 导出过滤器。
// 用错过滤器的表现不是报错，而是产出排版错乱的 PDF。
func TestFilterFor(t *testing.T) {
	tests := map[string]string{
		"a.docx": "pdf:writer_pdf_Export",
		"a.ppt":  "pdf:impress_pdf_Export",
		"a.pptx": "pdf:impress_pdf_Export",
		"a.xlsx": "pdf:calc_pdf_Export",
		"a.csv":  "pdf:calc_pdf_Export",
		"a.odt":  "pdf:writer_pdf_Export",
	}
	for name, want := range tests {
		if got := string(filterFor(name)); got != want {
			t.Fatalf("filterFor(%q) = %q, want %q", name, got, want)
		}
	}
}
