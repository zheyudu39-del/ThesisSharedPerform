package search

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/db"
)

/* -------------------------------------------------------------------------- */
/* snippet：提取与转义                                                          */
/* -------------------------------------------------------------------------- */

func TestSnippet_HighlightsMatch(t *testing.T) {
	body := "本文提出了一种新的注意力机制，用于长文本建模。实验表明效果显著。"
	got := snippet(body, "注意力机制")

	if !strings.Contains(got, "<mark>注意力机制</mark>") {
		t.Fatalf("片段未高亮命中词: %q", got)
	}
	// 上下文应保留命中词前后的内容。
	if !strings.Contains(got, "提出了一种新的") {
		t.Fatalf("片段缺少前文: %q", got)
	}
}

func TestSnippet_EscapesHTMLBeforeHighlight(t *testing.T) {
	body := `<script>alert(1)</script> 这是一段包含 <b>标签</b> 的正文，关键词在这里。`

	got := snippet(body, "关键词")

	if strings.Contains(got, "<script>") {
		t.Fatalf("片段未转义 script 标签: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("片段应包含转义后的 script: %q", got)
	}
	// 只有我们自己插入的 <mark> 允许是标签。
	if !strings.Contains(got, "<mark>关键词</mark>") {
		t.Fatalf("片段未高亮命中词: %q", got)
	}
	if strings.Contains(got, "<b>") {
		t.Fatalf("片段未转义 b 标签: %q", got)
	}
}

func TestSnippet_EscapesHighlightedTextItself(t *testing.T) {
	// 命中词本身就带标签字符时，也必须转义，不能拼出可执行的 HTML。
	body := `前缀 <img src=x onerror=alert(1)> 后缀`
	got := snippet(body, "<img src=x onerror=alert(1)>")

	if strings.Contains(got, "<img") {
		t.Fatalf("命中词未被转义: %q", got)
	}
	if !strings.Contains(got, "&lt;img") {
		t.Fatalf("命中词应被转义: %q", got)
	}
	if strings.Count(got, "<mark>") != 1 || strings.Count(got, "</mark>") != 1 {
		t.Fatalf("高亮标记数量异常: %q", got)
	}
}

func TestSnippet_NoMatchReturnsHead(t *testing.T) {
	body := strings.Repeat("无", 300)
	got := snippet(body, "不存在的词")

	if strings.Contains(got, "<mark>") {
		t.Fatalf("无命中时不应出现高亮: %q", got)
	}
	if len([]rune(got)) > snippetMaxRunes {
		t.Fatalf("无命中时片段过长: %d", len([]rune(got)))
	}
}

func TestSnippet_EmptyBody(t *testing.T) {
	if got := snippet("   ", "关键词"); got != "" {
		t.Fatalf("空正文应返回空片段, got %q", got)
	}
}

func TestSnippet_ContextIsBounded(t *testing.T) {
	body := strings.Repeat("前", 200) + "关键词" + strings.Repeat("后", 200)
	got := snippet(body, "关键词")

	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("长正文两侧应带省略号: %q", got)
	}
	// 命中词前后各 60 字 + 标记 + 省略号，总长应远小于原文。
	if n := len([]rune(got)); n > snippetContext*2+20 {
		t.Fatalf("片段过长: %d rune", n)
	}
}

func TestSnippet_EnglishCaseInsensitive(t *testing.T) {
	got := snippet("A Survey of Deep Learning Methods", "deep learning")
	if !strings.Contains(got, "<mark>Deep Learning</mark>") {
		t.Fatalf("英文匹配应忽略大小写并保留原文大小写: %q", got)
	}
}

func TestSnippet_FallsBackToSingleTerm(t *testing.T) {
	body := "这段文字里有检索，但没有前面那个词。"
	got := snippet(body, "完全不存在 检索")

	if !strings.Contains(got, "<mark>检索</mark>") {
		t.Fatalf("整句不匹配时应退化为单词匹配: %q", got)
	}
}

/* -------------------------------------------------------------------------- */
/* LIKE 模式构造                                                                */
/* -------------------------------------------------------------------------- */

func TestBuildLikePattern_EscapesWildcards(t *testing.T) {
	cases := map[string]string{
		"abc": "%abc%",
		"50%": `%50\%%`,
		"a_b": `%a\_b%`,
		`a\b`: `%a\\b%`,
	}
	for input, want := range cases {
		if got := buildLikePattern(input); got != want {
			t.Errorf("buildLikePattern(%q) = %q, want %q", input, got, want)
		}
	}
}

/* -------------------------------------------------------------------------- */
/* 查询参数校验                                                                 */
/* -------------------------------------------------------------------------- */

func TestQuery_RejectsEmptyText(t *testing.T) {
	svc := NewServiceWithQuerier(&stubQuerier{})
	for _, text := range []string{"", "   ", "\t\n"} {
		_, err := svc.Query(context.Background(), QueryInput{Text: text})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("空查询 %q 应返回 ErrValidation, got %v", text, err)
		}
	}
}

func TestQuery_RejectsOverlongText(t *testing.T) {
	svc := NewServiceWithQuerier(&stubQuerier{})
	_, err := svc.Query(context.Background(), QueryInput{Text: strings.Repeat("字", maxQueryRunes+1)})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("超长查询应返回 ErrValidation, got %v", err)
	}
}

func TestQuery_RejectsUnknownType(t *testing.T) {
	svc := NewServiceWithQuerier(&stubQuerier{})
	_, err := svc.Query(context.Background(), QueryInput{Text: "检索", Types: []string{"comment"}})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("未知实体类型应返回 ErrValidation, got %v", err)
	}
}

func TestQuery_EmptyQueryDoesNotHitDatabase(t *testing.T) {
	stub := &stubQuerier{}
	svc := NewServiceWithQuerier(stub)
	if _, err := svc.Query(context.Background(), QueryInput{Text: " "}); err == nil {
		t.Fatal("空查询应报错")
	}
	if stub.called {
		t.Fatal("参数校验失败时不应访问数据库")
	}
}

func TestNormalizeLimit(t *testing.T) {
	cases := map[int]int32{
		0:    DefaultLimit,
		-5:   DefaultLimit,
		1:    1,
		20:   20,
		50:   MaxLimit,
		9999: MaxLimit,
	}
	for in, want := range cases {
		if got := normalizeLimit(in); got != want {
			t.Errorf("normalizeLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestNormalizeTypes_DedupesAndLowercases(t *testing.T) {
	got, err := normalizeTypes([]string{"Paper", " paper ", "", "POST"})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	want := []string{EntityPaper, EntityPost}
	if len(got) != len(want) {
		t.Fatalf("去重结果 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("去重结果 = %v, want %v", got, want)
		}
	}
}

func TestNormalizeTypes_EmptyMeansNoFilter(t *testing.T) {
	got, err := normalizeTypes(nil)
	if err != nil || got != nil {
		t.Fatalf("空类型应返回 nil, got %v, %v", got, err)
	}
}

/* -------------------------------------------------------------------------- */
/* 索引写入参数校验                                                             */
/* -------------------------------------------------------------------------- */

func TestIndex_RejectsInvalidEntity(t *testing.T) {
	svc := NewServiceWithQuerier(&stubQuerier{})
	if err := svc.Index(context.Background(), IndexInput{
		EntityType: "comment", EntityID: uuid.New(), Title: "t",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("未知 entityType 应返回 ErrValidation, got %v", err)
	}
	if err := svc.Index(context.Background(), IndexInput{
		EntityType: EntityPaper, EntityID: uuid.Nil, Title: "t",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("空 entityId 应返回 ErrValidation, got %v", err)
	}
}

func TestIndex_SkipsEmptyDocument(t *testing.T) {
	stub := &stubQuerier{}
	svc := NewServiceWithQuerier(stub)
	if err := svc.Index(context.Background(), IndexInput{
		EntityType: EntityPaper, EntityID: uuid.New(), Title: "  ", Body: "",
	}); err != nil {
		t.Fatalf("空文档应静默跳过, got %v", err)
	}
	if stub.upserts != 0 {
		t.Fatal("空文档不应写入索引")
	}
}

func TestIndex_PassesThrough(t *testing.T) {
	stub := &stubQuerier{}
	svc := NewServiceWithQuerier(stub)
	id := uuid.New()
	if err := svc.Index(context.Background(), IndexInput{
		EntityType: EntityPost, EntityID: id, Title: "标题", Body: "正文",
	}); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if stub.upserts != 1 || stub.lastEntityType != EntityPost {
		t.Fatalf("应写入一次索引, got %d 次, 类型 %q", stub.upserts, stub.lastEntityType)
	}
}

/* -------------------------------------------------------------------------- */
/* 测试替身                                                                     */
/* -------------------------------------------------------------------------- */

type stubQuerier struct {
	called         bool
	upserts        int
	lastEntityType string
}

func (s *stubQuerier) UpsertSearchDocument(_ context.Context, arg db.UpsertSearchDocumentParams) error {
	s.called = true
	s.upserts++
	s.lastEntityType = arg.EntityType
	return nil
}

func (s *stubQuerier) DeleteSearchDocument(context.Context, db.DeleteSearchDocumentParams) error {
	s.called = true
	return nil
}

func (s *stubQuerier) SearchAll(context.Context, db.SearchAllParams) ([]db.SearchAllRow, error) {
	s.called = true
	return nil, nil
}

func (s *stubQuerier) SearchByTypes(context.Context, db.SearchByTypesParams) ([]db.SearchByTypesRow, error) {
	s.called = true
	return nil, nil
}

func TestSnippet_CaseFoldingDoesNotShiftIndex(t *testing.T) {
	// 'İ' 转小写会变成两个 rune，若整体转小写后再按原下标切片会切错位置。
	body := "İİİ 关键词 结尾"
	got := snippet(body, "关键词")
	if !strings.Contains(got, "<mark>关键词</mark>") {
		t.Fatalf("命中词未正确高亮: %q", got)
	}
	if !strings.HasPrefix(got, "İİİ") {
		t.Fatalf("前缀被切坏: %q", got)
	}
}
