package search

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// 这个文件实现一个「够用就好」的 PDF 文本层提取器。
//
// 为什么不用第三方库：本项目的依赖清单刻意保持精简（见 go.mod），
// 而 PDF 解析库（如 pdfcpu / unidoc）动辄数万行且各有许可证约束。
// 检索场景只需要正文文字，不需要渲染、注解或表单，因此手写一个只处理
// content stream 的最小解析器，比引入整套解析框架更可控。
//
// 能力边界（已知限制，写在这里避免调用方误解）：
//   - 只处理 FlateDecode（zlib）压缩的流；其他滤镜（LZW、ASCII85、DCT）跳过；
//   - 只处理文本型 PDF：Tj / TJ / ' / " 四个文本显示操作符；
//   - 扫描件（整页是图片、没有文本层）提取结果为空——这是格式决定的，
//     不是 bug，需要 OCR 才能解决；
//   - 字体编码依赖 ToUnicode CMap；Identity-H 字体缺少 ToUnicode 时
//     码位是字形号，无法还原成文字，这类文本会被跳过而不是输出乱码。

const (
	// maxPDFBytes 限制读取的 PDF 大小：上传上限 100MB，但文本层提取只需要
	// 一个可用的上限，避免异常文件把内存吃满。
	maxPDFBytes = 64 << 20
	// maxDecodedBytes 限制单个流解压后的体积，防止 zip 炸弹。
	maxDecodedBytes = 32 << 20
	// maxTextRunes 限制最终文本长度（约 200 万字符），足够覆盖长篇论文。
	maxTextRunes = 2_000_000
	// wordGapThreshold 是 TJ 数组里视为「词间空格」的位移阈值（单位：千分之一 em）。
	wordGapThreshold = -150
)

// ErrNotPDF 表示输入不是 PDF（魔数不匹配或超出大小上限）。
var ErrNotPDF = errors.New("不是 PDF 文件")

// ExtractText 从 PDF 读取文本层。
//
// 返回的文本按内容流顺序拼接，只保留可读字符；扫描版 PDF 返回空字符串
// 且 err 为 nil——调用方据此判断「没有文本层」，而不是当成解析失败。
func ExtractText(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxPDFBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取 PDF: %w", err)
	}
	if len(data) > maxPDFBytes {
		return "", fmt.Errorf("%w: 超过 %d MB", ErrNotPDF, maxPDFBytes>>20)
	}

	// PDF 规范要求文件头出现在前 1024 字节内（前面可能被工具塞了注释）。
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	if !bytes.Contains(head, []byte("%PDF-")) {
		return "", ErrNotPDF
	}

	objects := parsePDFObjects(data)
	decoder := buildDecoder(objects)

	var out strings.Builder
	for _, obj := range objects {
		raw, dict, ok := splitStream(obj.body)
		if !ok {
			continue
		}
		stream := decodeStream(raw, dict)
		// 内容流一定包含 BT/ET；用它过滤掉图片、字体、CMap 等二进制流。
		if len(stream) == 0 || !bytes.Contains(stream, []byte("BT")) {
			continue
		}
		text := extractContentText(stream, decoder)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out.WriteString(text)
		out.WriteByte('\n')
	}

	text := normalizeWhitespace(out.String())
	if runes := []rune(text); len(runes) > maxTextRunes {
		text = string(runes[:maxTextRunes])
	}
	return text, nil
}

// ---------------------------------------------------------------------------
// 对象与流的解析
// ---------------------------------------------------------------------------

type pdfObject struct {
	num  int
	body []byte
}

// parsePDFObjects 扫描所有 "N G obj ... endobj"，返回按出现顺序排列的对象体。
//
// 顺序很重要：内容流按页面顺序出现在文件里，保持原顺序能让提取出的文本
// 与文档阅读顺序大体一致。
func parsePDFObjects(data []byte) []pdfObject {
	objects := make([]pdfObject, 0, 64)
	seen := make(map[int]struct{}, 64)
	pos := 0
	for pos < len(data) {
		rel := bytes.Index(data[pos:], []byte(" obj"))
		if rel < 0 {
			break
		}
		idx := pos + rel
		num, ok := objectNumberBefore(data, idx)
		if !ok {
			pos = idx + 4
			continue
		}
		bodyStart := idx + 4
		relEnd := bytes.Index(data[bodyStart:], []byte("endobj"))
		var body []byte
		if relEnd < 0 {
			body = data[bodyStart:]
		} else {
			body = data[bodyStart : bodyStart+relEnd]
		}
		// 增量更新会重复定义同一个对象号，以第一次出现的为准
		// （与阅读器「后定义覆盖」相反，但检索场景下差异可忽略）。
		if _, dup := seen[num]; !dup {
			seen[num] = struct{}{}
			objects = append(objects, pdfObject{num: num, body: body})
		}
		if relEnd < 0 {
			break
		}
		pos = bodyStart + relEnd + len("endobj")
	}
	return objects
}

// objectNumberBefore 从 " obj" 前面解析出对象号（跳过代数）。
func objectNumberBefore(data []byte, idx int) (int, bool) {
	i := idx - 1
	for i >= 0 && isPDFSpace(data[i]) {
		i--
	}
	genEnd := i
	for i >= 0 && data[i] >= '0' && data[i] <= '9' {
		i--
	}
	if i == genEnd {
		return 0, false
	}
	for i >= 0 && isPDFSpace(data[i]) {
		i--
	}
	numEnd := i
	for i >= 0 && data[i] >= '0' && data[i] <= '9' {
		i--
	}
	if i == numEnd {
		return 0, false
	}
	num, err := strconv.Atoi(string(data[i+1 : numEnd+1]))
	if err != nil {
		return 0, false
	}
	return num, true
}

// splitStream 把对象体拆成「流数据」与「字典部分」。
func splitStream(body []byte) (raw, dict []byte, ok bool) {
	i := bytes.Index(body, []byte("stream"))
	if i < 0 {
		return nil, nil, false
	}
	dict = body[:i]
	start := i + len("stream")
	// stream 关键字后必须跟 CRLF 或 LF。
	if start < len(body) && body[start] == '\r' {
		start++
	}
	if start < len(body) && body[start] == '\n' {
		start++
	}
	end := bytes.LastIndex(body, []byte("endstream"))
	if end < 0 || end < start {
		return nil, nil, false
	}
	raw = bytes.TrimRight(body[start:end], "\r\n")
	return raw, dict, true
}

// decodeStream 按字典里的滤镜解压流。不支持的滤镜返回 nil。
func decodeStream(raw, dict []byte) []byte {
	if bytes.Contains(dict, []byte("/FlateDecode")) {
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil
		}
		defer func() { _ = zr.Close() }()
		out, err := io.ReadAll(io.LimitReader(zr, maxDecodedBytes))
		if err != nil {
			return nil
		}
		return out
	}
	if bytes.Contains(dict, []byte("/Filter")) {
		// 其他滤镜（LZW / ASCII85 / DCT 等）不在支持范围内。
		return nil
	}
	return raw
}

// ---------------------------------------------------------------------------
// ToUnicode CMap
// ---------------------------------------------------------------------------

type textDecoder struct {
	cmap map[uint32]string
	// twoByte 表示这份文档的字体用双字节编码（Identity-H / UCS2）。
	// 单双字节混排的文档极少见，出现时以双字节为准。
	twoByte bool
}

func buildDecoder(objects []pdfObject) *textDecoder {
	d := &textDecoder{cmap: make(map[uint32]string, 256)}
	for _, obj := range objects {
		raw, dict, ok := splitStream(obj.body)
		if !ok {
			continue
		}
		stream := decodeStream(raw, dict)
		if len(stream) == 0 {
			continue
		}
		if bytes.Contains(stream, []byte("beginbfchar")) || bytes.Contains(stream, []byte("beginbfrange")) {
			d.parseCMap(stream)
		}
	}
	return d
}

func (d *textDecoder) parseCMap(stream []byte) {
	for rest := stream; ; {
		i := bytes.Index(rest, []byte("beginbfchar"))
		if i < 0 {
			break
		}
		j := bytes.Index(rest[i:], []byte("endbfchar"))
		if j < 0 {
			break
		}
		d.parseBFChar(rest[i+len("beginbfchar") : i+j])
		rest = rest[i+j+len("endbfchar"):]
	}
	for rest := stream; ; {
		i := bytes.Index(rest, []byte("beginbfrange"))
		if i < 0 {
			break
		}
		j := bytes.Index(rest[i:], []byte("endbfrange"))
		if j < 0 {
			break
		}
		d.parseBFRange(rest[i+len("beginbfrange") : i+j])
		rest = rest[i+j+len("endbfrange"):]
	}
}

// parseBFChar 处理 "<src> <dst>" 成对的映射。
func (d *textDecoder) parseBFChar(block []byte) {
	i := 0
	for {
		src, next, ok := readHexToken(block, i)
		if !ok {
			return
		}
		dst, next2, ok := readHexToken(block, next)
		if !ok {
			return
		}
		i = next2
		code := codeFromBytes(src)
		d.noteWidth(len(src))
		if s, ok := decodeTarget(dst); ok {
			d.cmap[code] = s
		}
	}
}

// parseBFRange 处理 "<lo> <hi> <dst>" 与 "<lo> <hi> [<d1> <d2> ...]" 两种形式。
func (d *textDecoder) parseBFRange(block []byte) {
	i := 0
	for i < len(block) {
		lo, next, ok := readHexToken(block, i)
		if !ok {
			i++
			continue
		}
		hi, next2, ok := readHexToken(block, next)
		if !ok {
			i = next
			continue
		}
		i = next2
		d.noteWidth(len(lo))
		loCode := codeFromBytes(lo)
		hiCode := codeFromBytes(hi)
		if hiCode < loCode || hiCode-loCode > 0xFFFF {
			continue
		}
		for i < len(block) && isPDFSpace(block[i]) {
			i++
		}
		if i < len(block) && block[i] == '[' {
			i++
			for code := loCode; code <= hiCode; code++ {
				dst, next3, ok := readHexToken(block, i)
				if !ok {
					break
				}
				i = next3
				if s, ok := decodeTarget(dst); ok {
					d.cmap[code] = s
				}
			}
			for i < len(block) && block[i] != ']' {
				i++
			}
			if i < len(block) {
				i++
			}
			continue
		}
		dst, next4, ok := readHexToken(block, i)
		if !ok {
			continue
		}
		i = next4
		base, ok := decodeTarget(dst)
		if !ok {
			continue
		}
		baseRune, _ := utf8.DecodeRuneInString(base)
		for code := loCode; code <= hiCode; code++ {
			d.cmap[code] = string(baseRune + rune(code-loCode))
		}
	}
}

func (d *textDecoder) noteWidth(n int) {
	if n >= 2 {
		d.twoByte = true
	}
}

// decodeString 把 PDF 字符串按当前编码解码成文本。
func (d *textDecoder) decodeString(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	// UTF-16BE 带 BOM：PDF 规范允许直接内嵌 Unicode，此时不需要 CMap。
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		return decodeUTF16BE(raw[2:])
	}

	var b strings.Builder
	if d.twoByte {
		for i := 0; i+1 < len(raw); i += 2 {
			code := uint32(raw[i])<<8 | uint32(raw[i+1])
			if s, ok := d.cmap[code]; ok {
				b.WriteString(s)
			}
			// 没有映射说明是字形号：输出只会得到乱码，宁可丢弃。
		}
		return b.String()
	}

	for _, c := range raw {
		if s, ok := d.cmap[uint32(c)]; ok {
			b.WriteString(s)
			continue
		}
		switch {
		case c == '\t' || c == '\n' || c == '\r':
			b.WriteByte(' ')
		case c >= 0x20 && c < 0x7F:
			b.WriteByte(c)
		default:
			// WinAnsi / Latin-1 兜底：高位字节按码位直接映射，
			// 对西文 PDF 足够；中文 PDF 通常带 ToUnicode，不会走到这里。
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}

// codeFromBytes 把 1–4 字节的大端编码转成整数。
func codeFromBytes(b []byte) uint32 {
	var code uint32
	for _, c := range b {
		code = code<<8 | uint32(c)
	}
	return code
}

// decodeTarget 把 ToUnicode 的目标字节转成字符串。
//
// 目标可以是多个 UTF-16 码元（连字、组合字符），整体保留比只取首字符
// 更准确——截断会丢掉变音符号，出现 "frmly" 这种缺字的文本。
func decodeTarget(dst []byte) (string, bool) {
	switch {
	case len(dst) == 0:
		return "", false
	case len(dst) == 1:
		return string(rune(dst[0])), true
	case len(dst)%2 == 0:
		s := decodeUTF16BE(dst)
		if s == "" {
			return "", false
		}
		return s, true
	default:
		return string(rune(dst[0])), true
	}
}

func decodeUTF16BE(b []byte) string {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
	}
	return string(utf16.Decode(units))
}

// ---------------------------------------------------------------------------
// 内容流
// ---------------------------------------------------------------------------

// extractContentText 扫描内容流，收集文本显示操作符里的字符串。
//
// 这里不是完整的 PDF 解释器：不跟踪图形状态、不处理 XObject 递归，
// 但保留了「换行」与「TJ 大位移 → 空格」这两条对可读性影响最大的线索。
func extractContentText(stream []byte, d *textDecoder) string {
	var out strings.Builder
	var pending []byte
	arrayDepth := 0

	flush := func() {
		if len(pending) == 0 {
			return
		}
		out.WriteString(d.decodeString(pending))
		pending = pending[:0]
	}
	newline := func() {
		flush()
		out.WriteByte('\n')
	}

	i := 0
	for i < len(stream) {
		c := stream[i]
		switch {
		case c == '%':
			for i < len(stream) && stream[i] != '\n' && stream[i] != '\r' {
				i++
			}

		case c == '(':
			var s []byte
			s, i = readLiteralString(stream, i)
			pending = append(pending, s...)

		case c == '<' && i+1 < len(stream) && stream[i+1] == '<':
			// 内联字典（如 BDC 的属性），整体跳过。
			i += 2

		case c == '<':
			var s []byte
			s, i = readHexString(stream, i)
			pending = append(pending, s...)

		case c == '[':
			arrayDepth++
			i++

		case c == ']':
			if arrayDepth > 0 {
				arrayDepth--
			}
			i++

		case c == '\'' || c == '"':
			// ' 与 " 都是「换行后显示字符串」的操作符。
			newline()
			i++

		case c == '-' || c == '.' || (c >= '0' && c <= '9'):
			start := i
			for i < len(stream) && (stream[i] == '-' || stream[i] == '.' ||
				(stream[i] >= '0' && stream[i] <= '9')) {
				i++
			}
			if arrayDepth > 0 {
				if v, err := strconv.ParseFloat(string(stream[start:i]), 64); err == nil && v <= wordGapThreshold {
					// TJ 数组里的大负位移代表词间空格，补一个空格避免单词粘连。
					pending = append(pending, ' ')
				}
			}

		case isRegular(c):
			start := i
			for i < len(stream) && isRegular(stream[i]) {
				i++
			}
			switch string(stream[start:i]) {
			case "Tj", "TJ":
				flush()
			case "T*", "Td", "TD", "Tm", "ET", "BT":
				newline()
			}

		default:
			i++
		}
	}
	flush()
	return out.String()
}

// readLiteralString 解析 (...) 字面量字符串，处理嵌套括号与转义。
// 返回字符串内容与下一个读取位置。
func readLiteralString(s []byte, i int) ([]byte, int) {
	i++ // 跳过 '('
	depth := 1
	var out []byte
	for i < len(s) {
		c := s[i]
		switch c {
		case '\\':
			i++
			if i >= len(s) {
				return out, i
			}
			switch s[i] {
			case 'n':
				out = append(out, '\n')
				i++
			case 'r':
				out = append(out, '\r')
				i++
			case 't':
				out = append(out, '\t')
				i++
			case 'b':
				out = append(out, '\b')
				i++
			case 'f':
				out = append(out, '\f')
				i++
			case '(', ')', '\\':
				out = append(out, s[i])
				i++
			case '\r':
				i++
				if i < len(s) && s[i] == '\n' {
					i++
				}
			case '\n':
				i++
			default:
				if s[i] >= '0' && s[i] <= '7' {
					val, n := 0, 0
					for n < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7' {
						val = val*8 + int(s[i]-'0')
						i++
						n++
					}
					out = append(out, byte(val))
				} else {
					out = append(out, s[i])
					i++
				}
			}
		case '(':
			depth++
			out = append(out, c)
			i++
		case ')':
			depth--
			if depth == 0 {
				return out, i + 1
			}
			out = append(out, c)
			i++
		default:
			out = append(out, c)
			i++
		}
	}
	return out, i
}

// readHexString 解析 <...> 十六进制字符串，返回原始字节。
func readHexString(s []byte, i int) ([]byte, int) {
	i++ // 跳过 '<'
	var digits []byte
	for i < len(s) && s[i] != '>' {
		if !isPDFSpace(s[i]) {
			digits = append(digits, s[i])
		}
		i++
	}
	if i < len(s) {
		i++ // 跳过 '>'
	}
	return decodeHexDigits(digits), i
}

// readHexToken 读取一个 <...> 标记，位置不在 '<' 时返回 ok=false。
func readHexToken(s []byte, i int) ([]byte, int, bool) {
	for i < len(s) && isPDFSpace(s[i]) {
		i++
	}
	if i >= len(s) || s[i] != '<' {
		return nil, i, false
	}
	val, next := readHexString(s, i)
	return val, next, true
}

func decodeHexDigits(digits []byte) []byte {
	out := make([]byte, 0, (len(digits)+1)/2)
	var hi int = -1
	for _, c := range digits {
		v := hexValue(c)
		if v < 0 {
			continue
		}
		if hi < 0 {
			hi = v
			continue
		}
		out = append(out, byte(hi<<4|v))
		hi = -1
	}
	if hi >= 0 {
		out = append(out, byte(hi<<4))
	}
	return out
}

func hexValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

// normalizeWhitespace 统一换行、压缩空白，并剔除不能进入数据库的字符。
//
// NUL 必须在这里清掉：PDF 允许文本串里出现 \000（部分生成器用它表示缺字），
// 而 PostgreSQL 的 text 列不接受 0x00，写入会报 SQLSTATE 22021
// 「无效的 "UTF8" 编码字节顺序: 0x00」。Word 经 LibreOffice 转出的 PDF
// 经常带这种字符，之前表现为「索引文件文本层失败」，全文检索静默失效。
// 其余 C0 控制字符同样无检索价值，一并转成空格。
func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	spaceRun := 0
	newlineRun := 0
	for _, r := range s {
		switch r {
		case ' ', '\t':
			spaceRun++
			continue
		case '\n':
			spaceRun = 0
			newlineRun++
			continue
		case 0:
			// NUL 视作词间分隔，不保留也不吞掉相邻文字。
			spaceRun++
			continue
		}
		if r < 0x20 || r == 0x7f {
			// 其它控制字符：替换为空格，避免整段文本被数据库拒绝。
			spaceRun++
			continue
		}
		if spaceRun > 0 && b.Len() > 0 {
			b.WriteByte(' ')
		}
		if newlineRun > 0 && b.Len() > 0 {
			if newlineRun > 1 {
				b.WriteString("\n\n")
			} else {
				b.WriteByte('\n')
			}
		}
		spaceRun, newlineRun = 0, 0
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func isPDFSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\f', 0:
		return true
	}
	return false
}

// isRegular 判断字符是否属于 PDF 的「常规字符」集合（操作符/关键字）。
func isRegular(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		return true
	case c == '*':
		return true
	}
	return false
}
