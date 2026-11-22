package file

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

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

// TestTruncateRunes 验证按字符截断不会劈开多字节字符。
// 错误信息里常带中文文件名，按字节切会产出非法 UTF-8。
func TestTruncateRunes(t *testing.T) {
	s := "论文转换失败：文档损坏"
	got := truncateRunes(s, 5)
	if got != "论文转换失" {
		t.Fatalf("truncateRunes = %q, want %q", got, "论文转换失")
	}
	if got := truncateRunes("short", 10); got != "short" {
		t.Fatalf("短字符串不应被截断，got %q", got)
	}
}

// TestFindSoffice 在装了 LibreOffice 的环境里应能找到可执行文件。
// 没装时跳过，而不是失败——CI 环境未必有。
func TestFindSoffice(t *testing.T) {
	path, err := findSoffice()
	if err != nil {
		if runtime.GOOS == "windows" {
			if _, statErr := os.Stat(`C:\Program Files\LibreOffice\program\soffice.exe`); statErr == nil {
				t.Fatalf("soffice 存在于标准路径但 findSoffice 没找到: %v", err)
			}
		}
		t.Skip("环境未安装 LibreOffice，跳过")
	}
	if _, err := exec.LookPath(path); err != nil {
		t.Fatalf("findSoffice 返回的路径不可执行: %s", path)
	}
	if filepath.Ext(path) == "" && runtime.GOOS != "windows" {
		t.Logf("soffice 路径: %s", path)
	}
}
