package paper

import (
	"net/url"
	"regexp"
	"strings"
)

// Source 表示元数据的来源渠道，用于记录 meta_source。
type Source string

const (
	SourceDOI     Source = "crossref"
	SourceArxiv   Source = "arxiv"
	SourceManual  Source = "manual"
	SourceOpenRev Source = "openreview"
)

// Identifier 是从用户输入中识别出的论文标识。
type Identifier struct {
	Kind Source
	// Value 对 DOI 是规范化后的 DOI（小写、去掉 URL 前缀）；
	// 对 arXiv 是去掉版本号的编号（如 2405.13412）。
	Value string
}

var (
	// DOI: 10.<4-9 位注册号>/<后缀>，后缀允许常见字符。
	doiPattern = regexp.MustCompile(`(?i)\b(10\.\d{4,9}/[-._;()/:a-z0-9]+)`)
	// arXiv 新式编号：YYMM.NNNNN（4-5 位），可带 vN 版本号。
	arxivNewPattern = regexp.MustCompile(`(?i)\b(\d{4}\.\d{4,5})(v\d+)?\b`)
	// arXiv 旧式编号：archive/YYMMNNN，如 hep-th/9901001。
	arxivOldPattern = regexp.MustCompile(`(?i)\b([a-z-]+(?:\.[A-Z]{2})?/\d{7})(v\d+)?\b`)
)

// Identify 从任意用户输入（DOI、arXiv 链接、URL 或纯编号）中识别论文标识。
// 无法识别时返回 ok=false，调用方应引导用户手动补录（PRD P-04）。
func Identify(raw string) (Identifier, bool) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return Identifier{}, false
	}

	// 1) DOI 优先：DOI 里可能包含数字，先匹配可避免被 arXiv 规则误判。
	if m := doiPattern.FindStringSubmatch(input); m != nil {
		return Identifier{Kind: SourceDOI, Value: normalizeDOI(m[1])}, true
	}

	// 2) 解析 URL，按域名分派。
	if parsed, err := url.Parse(input); err == nil && parsed.Host != "" {
		host := strings.ToLower(parsed.Host)
		switch {
		case strings.HasSuffix(host, "arxiv.org"):
			// /abs/2405.13412 或 /pdf/2405.13412v2
			if id, ok := parseArxivPath(parsed.Path); ok {
				return Identifier{Kind: SourceArxiv, Value: id}, true
			}
		case strings.HasSuffix(host, "openreview.net"):
			if q := parsed.Query().Get("id"); q != "" {
				return Identifier{Kind: SourceOpenRev, Value: q}, true
			}
		case strings.HasSuffix(host, "doi.org"):
			// https://doi.org/10.1145/xxxx
			if id, ok := Identify(strings.TrimPrefix(parsed.Path, "/")); ok && id.Kind == SourceDOI {
				return id, true
			}
		}
	}

	// 3) 纯 arXiv 编号。
	if id, ok := parseArxivPath(input); ok {
		return Identifier{Kind: SourceArxiv, Value: id}, true
	}

	return Identifier{}, false
}

// parseArxivPath 从路径或裸字符串中提取 arXiv 编号，并去掉版本号。
func parseArxivPath(path string) (string, bool) {
	p := strings.TrimSpace(path)
	p = strings.TrimPrefix(p, "/")
	for _, prefix := range []string{"abs/", "pdf/", "html/"} {
		if strings.HasPrefix(strings.ToLower(p), prefix) {
			p = p[len(prefix):]
			break
		}
	}
	// 去掉 .pdf 后缀
	p = strings.TrimSuffix(strings.ToLower(p), ".pdf")

	if m := arxivNewPattern.FindStringSubmatch(p); m != nil {
		return m[1], true
	}
	if m := arxivOldPattern.FindStringSubmatch(p); m != nil {
		return m[1], true
	}
	return "", false
}

// normalizeDOI 统一 DOI 形式：去掉 https://doi.org/ 等前缀，转小写。
func normalizeDOI(doi string) string {
	d := strings.TrimSpace(doi)
	lower := strings.ToLower(d)
	for _, prefix := range []string{
		"https://doi.org/", "http://doi.org/",
		"https://dx.doi.org/", "http://dx.doi.org/",
		"doi:", "doi ",
	} {
		if strings.HasPrefix(lower, prefix) {
			d = d[len(prefix):]
			lower = lower[len(prefix):]
			break
		}
	}
	return strings.ToLower(strings.TrimSpace(d))
}
