package paper

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Metadata 是从外部源抓取到的论文元数据，已映射到内部 schema。
type Metadata struct {
	DOI           string
	ArxivID       string
	OpenreviewID  string
	Title         string
	Authors       []Author
	Abstract      string
	Venue         string
	PublishedYear int
	CitationCount int
	Source        Source
}

// Author 对应 papers.authors JSONB 中的一项。
type Author struct {
	Name        string `json:"name"`
	Affiliation string `json:"affiliation,omitempty"`
	ORCID       string `json:"orcid,omitempty"`
}

// Fetcher 从外部数据源抓取元数据。
type Fetcher struct {
	client  *http.Client
	baseURL string // 仅测试用；为空时使用真实端点
}

// NewFetcher 创建抓取器。超时按 PRD 要求控制在合理范围，避免拖慢用户请求。
func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{Timeout: 12 * time.Second},
	}
}

// Fetch 按标识抓取元数据。优先 Crossref，失败后降级到 arXiv
// （见 technical-architecture.md §4.5「抓取源优先级」）。
func (f *Fetcher) Fetch(ctx context.Context, id Identifier) (Metadata, error) {
	switch id.Kind {
	case SourceDOI:
		meta, err := f.fetchCrossref(ctx, id.Value)
		if err == nil {
			return meta, nil
		}
		// Crossref 无结果时尝试 arXiv（有些论文 DOI 指向 arXiv 预印本）。
		if arxivMeta, arxivErr := f.fetchArxiv(ctx, id.Value); arxivErr == nil {
			return arxivMeta, nil
		}
		return Metadata{}, err

	case SourceArxiv:
		return f.fetchArxiv(ctx, id.Value)

	case SourceOpenRev:
		return Metadata{}, fmt.Errorf("%w: OpenReview 解析暂未实现", ErrNotFound)

	default:
		return Metadata{}, fmt.Errorf("%w: 不支持的标识类型", ErrNotFound)
	}
}

/* -------------------------------------------------------------------------- */
/* Crossref                                                                    */
/* -------------------------------------------------------------------------- */

type crossrefResponse struct {
	Message struct {
		DOI       string   `json:"DOI"`
		Title     []string `json:"title"`
		Abstract  string   `json:"abstract"`
		Container []string `json:"container-title"`
		Published struct {
			DateParts [][]int `json:"date-parts"`
		} `json:"published"`
		Author []struct {
			Given       string `json:"given"`
			Family      string `json:"family"`
			ORCID       string `json:"ORCID"`
			Affiliation []struct {
				Name string `json:"name"`
			} `json:"affiliation"`
		} `json:"author"`
		IsReferencedByCount int `json:"is-referenced-by-count"`
	} `json:"message"`
}

func (f *Fetcher) fetchCrossref(ctx context.Context, doi string) (Metadata, error) {
	endpoint := f.baseURL
	if endpoint == "" {
		endpoint = "https://api.crossref.org/works/" + url.PathEscape(doi)
	} else {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/works/" + url.PathEscape(doi)
	}

	body, status, err := f.get(ctx, endpoint, "ThesisShared/0.1 (mailto:dev@thesis.local)")
	if err != nil {
		return Metadata{}, err
	}
	if status == http.StatusNotFound {
		return Metadata{}, ErrNotFound
	}
	if status != http.StatusOK {
		return Metadata{}, fmt.Errorf("crossref 返回状态 %d", status)
	}

	var resp crossrefResponse
	if err := jsonUnmarshal(body, &resp); err != nil {
		return Metadata{}, fmt.Errorf("解析 crossref 响应失败: %w", err)
	}

	msg := resp.Message
	if strings.TrimSpace(firstNonEmpty(msg.Title)) == "" {
		return Metadata{}, ErrNotFound
	}

	meta := Metadata{
		DOI:           normalizeDOI(msg.DOI),
		Title:         stripJats(firstNonEmpty(msg.Title)),
		Abstract:      stripJats(msg.Abstract),
		Venue:         firstNonEmpty(msg.Container),
		CitationCount: msg.IsReferencedByCount,
		Source:        SourceDOI,
	}
	if len(msg.Published.DateParts) > 0 && len(msg.Published.DateParts[0]) > 0 {
		meta.PublishedYear = msg.Published.DateParts[0][0]
	}
	for _, a := range msg.Author {
		name := strings.TrimSpace(a.Given + " " + a.Family)
		if name == "" {
			continue
		}
		author := Author{Name: name, ORCID: strings.TrimPrefix(a.ORCID, "https://orcid.org/")}
		if len(a.Affiliation) > 0 {
			author.Affiliation = a.Affiliation[0].Name
		}
		meta.Authors = append(meta.Authors, author)
	}
	return meta, nil
}

/* -------------------------------------------------------------------------- */
/* arXiv                                                                       */
/* -------------------------------------------------------------------------- */

type arxivFeed struct {
	XMLName xml.Name `xml:"feed"`
	Entries []struct {
		ID        string `xml:"id"`
		Title     string `xml:"title"`
		Summary   string `xml:"summary"`
		Published string `xml:"published"`
		Authors   []struct {
			Name string `xml:"name"`
		} `xml:"author"`
		DOI             string `xml:"doi"`
		JournalRef      string `xml:"journal_ref"`
		PrimaryCategory struct {
			Term string `xml:"term"`
		} `xml:"primary_category"`
	} `xml:"entry"`
}

func (f *Fetcher) fetchArxiv(ctx context.Context, arxivID string) (Metadata, error) {
	endpoint := f.baseURL
	if endpoint == "" {
		endpoint = "https://export.arxiv.org/api/query?id_list=" + url.QueryEscape(arxivID)
	} else {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/api/query?id_list=" + url.QueryEscape(arxivID)
	}

	body, status, err := f.get(ctx, endpoint, "")
	if err != nil {
		return Metadata{}, err
	}
	if status != http.StatusOK {
		return Metadata{}, fmt.Errorf("arxiv 返回状态 %d", status)
	}

	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return Metadata{}, fmt.Errorf("解析 arxiv 响应失败: %w", err)
	}
	if len(feed.Entries) == 0 {
		return Metadata{}, ErrNotFound
	}

	entry := feed.Entries[0]
	// arXiv 对不存在的编号会返回一条标题为 "Error" 的 entry。
	if strings.EqualFold(strings.TrimSpace(entry.Title), "Error") {
		return Metadata{}, ErrNotFound
	}

	meta := Metadata{
		ArxivID:  arxivID,
		Title:    collapseSpace(entry.Title),
		Abstract: collapseSpace(entry.Summary),
		Venue:    strings.TrimSpace(entry.JournalRef),
		DOI:      normalizeDOI(entry.DOI),
		Source:   SourceArxiv,
	}
	if meta.Venue == "" {
		meta.Venue = "arXiv " + entry.PrimaryCategory.Term
	}
	if t, err := time.Parse(time.RFC3339, entry.Published); err == nil {
		meta.PublishedYear = t.Year()
	}
	for _, a := range entry.Authors {
		if name := collapseSpace(a.Name); name != "" {
			meta.Authors = append(meta.Authors, Author{Name: name})
		}
	}
	return meta, nil
}

/* -------------------------------------------------------------------------- */
/* 工具                                                                        */
/* -------------------------------------------------------------------------- */

func (f *Fetcher) get(ctx context.Context, endpoint, userAgent string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("构造请求失败: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	req.Header.Set("Accept", "application/json, application/atom+xml, */*")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求外部源失败: %w", err)
	}
	defer resp.Body.Close()

	// 限制读取大小，防止异常响应耗尽内存。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应失败: %w", err)
	}
	return body, resp.StatusCode, nil
}

// stripJats 去掉 Crossref 摘要里常见的 JATS XML 标签。
func stripJats(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return collapseSpace(b.String())
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func firstNonEmpty(items []string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}
