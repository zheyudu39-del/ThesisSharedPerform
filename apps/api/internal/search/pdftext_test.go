package search

import (
	"bytes"
	"compress/zlib"
	"strings"
	"testing"
)

// buildPDF 拼一个最小可解析的 PDF：一个内容流 + 一个 ToUnicode CMap。
// 真实 PDF 由 LaTeX / Word 生成，结构远比这个复杂；这里只验证
// ExtractText 对「FlateDecode + Tj/TJ + CMap」这条主路径的处理。
func buildPDF(t *testing.T, content string, cmap string) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")

	writeObj := func(num int, body string, stream []byte) {
		buf.WriteString(strings.TrimSpace(itoa(num)) + " 0 obj\n")
		buf.WriteString(body)
		if stream != nil {
			buf.WriteString("\nstream\n")
			buf.Write(stream)
			buf.WriteString("\nendstream")
		}
		buf.WriteString("\nendobj\n")
	}

	writeObj(1, "<< /Length 0 /Filter /FlateDecode >>", deflate(t, content))
	writeObj(2, "<< /Length 0 /Filter /FlateDecode >>", deflate(t, cmap))
	buf.WriteString("trailer\n<<>>\n%%EOF\n")
	return buf.Bytes()
}

func deflate(t *testing.T, s string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zlib.NewWriter(&out)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭压缩流失败: %v", err)
	}
	return out.Bytes()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestExtractText_LatinContentStream(t *testing.T) {
	content := `BT /F1 12 Tf 72 720 Td (Hello World) Tj ET
BT /F1 12 Tf 72 700 Td [(Deep) -200 (Learning)] TJ ET
BT /F1 12 Tf 72 680 Td (Second \(escaped\) line) Tj ET`

	got, err := ExtractText(bytes.NewReader(buildPDF(t, content, "")))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	for _, want := range []string{"Hello World", "Deep Learning", "Second (escaped) line"} {
		if !strings.Contains(got, want) {
			t.Errorf("提取结果缺少 %q，实际为 %q", want, got)
		}
	}
}

func TestExtractText_ToUnicodeTwoByte(t *testing.T) {
	// Identity-H 风格：两字节码位，经 CMap 映射到中文。
	content := `BT /F1 12 Tf <00010002> Tj ET`
	cmap := `begincmap
1 begincodespacerange <0000> <FFFF> endcodespacerange
2 beginbfchar
<0001> <6DF1>
<0002> <5EA6>
endbfchar
endcmap`

	got, err := ExtractText(bytes.NewReader(buildPDF(t, content, cmap)))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(got, "深度") {
		t.Fatalf("中文映射失败，实际为 %q", got)
	}
}

func TestExtractText_BFRange(t *testing.T) {
	content := `BT /F1 12 Tf <002000210022> Tj ET`
	cmap := `1 begincodespacerange <0000> <FFFF> endcodespacerange
1 beginbfrange
<0020> <0022> <0041>
endbfrange`

	got, err := ExtractText(bytes.NewReader(buildPDF(t, content, cmap)))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(got, "ABC") {
		t.Fatalf("bfrange 映射失败，实际为 %q", got)
	}
}

// 扫描版 PDF：只有图片、没有文本显示操作符，应返回空文本而不是错误。
func TestExtractText_ImageOnlyReturnsEmpty(t *testing.T) {
	content := `q 612 0 0 792 0 0 cm /Im1 Do Q`

	got, err := ExtractText(bytes.NewReader(buildPDF(t, content, "")))
	if err != nil {
		t.Fatalf("扫描件不应报错: %v", err)
	}
	if got != "" {
		t.Fatalf("扫描件应返回空文本，实际为 %q", got)
	}
}

func TestExtractText_RejectsNonPDF(t *testing.T) {
	if _, err := ExtractText(strings.NewReader("GIF89a not a pdf")); err == nil {
		t.Fatal("非 PDF 应返回错误")
	}
}

func TestExtractText_HexStringAndEscapes(t *testing.T) {
	// 十六进制字符串 + 八进制转义。
	content := `BT <48656C6C6F> Tj (\101\102) Tj ET`

	got, err := ExtractText(bytes.NewReader(buildPDF(t, content, "")))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "AB") {
		t.Fatalf("十六进制/转义解析失败，实际为 %q", got)
	}
}
