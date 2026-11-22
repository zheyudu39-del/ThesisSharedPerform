package search

import (
	"bytes"
	"strings"
	"testing"
)

// PDF 文本串允许 \000（缺字占位），但 PostgreSQL 的 text 列不接受 NUL。
// Word 经 LibreOffice 转出的 PDF 常带这种字符，之前会导致整段文本层
// 索引失败（SQLSTATE 22021），表现为「索引文件文本层失败」。
func TestExtractTextStripsNul(t *testing.T) {
	stream := []byte("BT /F1 12 Tf 10 700 Td (Hel\x00lo) Tj (World\x00) Tj ET")
	pdf := []byte("%PDF-1.4\n1 0 obj\n<< /Length " + decimal(len(stream)) + " >>\nstream\n")
	pdf = append(pdf, stream...)
	pdf = append(pdf, []byte("\nendstream\nendobj\n%%EOF\n")...)

	got, err := ExtractText(bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if strings.ContainsRune(got, 0) {
		t.Fatalf("提取结果仍含 NUL：%q", got)
	}
	if !strings.Contains(got, "Hel") || !strings.Contains(got, "lo") {
		t.Fatalf("NUL 两侧的文字不应丢失：%q", got)
	}
}

// 其它 C0 控制字符同样不能进 text 列。
func TestNormalizeWhitespaceDropsControlChars(t *testing.T) {
	got := normalizeWhitespace("a\x01b\x1fc\x7fd")
	for _, r := range got {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("仍含控制字符 %#x：%q", r, got)
		}
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "d") {
		t.Fatalf("文字被吞掉：%q", got)
	}
}

func decimal(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
