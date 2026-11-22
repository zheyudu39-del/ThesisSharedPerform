package officeconv

import (
	"os"
	"runtime"
	"testing"
)

// TestTrimRunes 验证按字符截断不会劈开多字节字符。
// 错误信息里常带中文文件名，按字节切会产出非法 UTF-8。
func TestTrimRunes(t *testing.T) {
	got := Trim("论文转换失败：文档损坏", 5)
	if got != "论文转换失" {
		t.Fatalf("Trim = %q, want %q", got, "论文转换失")
	}
	if got := Trim("short", 10); got != "short" {
		t.Fatalf("短字符串不应被截断，got %q", got)
	}
	if got := Trim("  两边有空格  ", 100); got != "两边有空格" {
		t.Fatalf("应裁剪首尾空白，got %q", got)
	}
}

// TestFindSoffice 在装了 LibreOffice 的环境里应能找到可执行文件。
// 没装时跳过，而不是失败——CI 环境未必有。
func TestFindSoffice(t *testing.T) {
	path, err := find()
	if err != nil {
		if runtime.GOOS == "windows" {
			if _, statErr := os.Stat(`C:\Program Files\LibreOffice\program\soffice.exe`); statErr == nil {
				t.Fatalf("soffice 存在于标准路径但 find() 没找到: %v", err)
			}
		}
		t.Skip("环境未安装 LibreOffice，跳过")
	}
	if _, err := os.Stat(path); err != nil && runtime.GOOS == "windows" {
		t.Fatalf("find() 返回的路径不存在: %s", path)
	}
}

// TestAvailable 确认在没有 LibreOffice 的环境下返回 false 而不是 panic。
func TestAvailable(t *testing.T) {
	_ = Available()
}
