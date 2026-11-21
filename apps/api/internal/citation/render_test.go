package citation

import (
	"errors"
	"strings"
	"testing"
)

// entry 构造一条测试条目，未指定的字段保持零值。
func entry(key, title string, authors []Author, year int32, container string) Entry {
	return Entry{
		CiteKey: key, Title: title, Authors: authors, Year: year,
		Container: container, EntryType: "article",
	}
}

func TestRenderGBSingleAuthor(t *testing.T) {
	e := entry("knuth1997", "The Art of Computer Programming",
		[]Author{{Family: "Knuth", Given: "Donald E."}}, 1997, "Addison-Wesley")
	e.Volume = "1"
	e.Pages = "1–650"

	got, err := RenderBibliography(FormatGBT7714, []Entry{e})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "KNUTH D E. The Art of Computer Programming[J]. Addison-Wesley, 1997, 1: 1–650."
	if got[0].Text != want {
		t.Fatalf("got %q\nwant %q", got[0].Text, want)
	}
}

func TestRenderGBMoreThanThreeAuthorsUsesEtAl(t *testing.T) {
	authors := []Author{
		{Family: "Vaswani", Given: "Ashish"},
		{Family: "Shazeer", Given: "Noam"},
		{Family: "Parmar", Given: "Niki"},
		{Family: "Uszkoreit", Given: "Jakob"},
	}
	got, _ := RenderBibliography(FormatGBT7714, []Entry{
		entry("vaswani2017", "Attention is All You Need", authors, 2017, "NeurIPS"),
	})
	if !strings.Contains(got[0].Text, "et al.") {
		t.Fatalf("多作者西文条目应使用 et al.: %q", got[0].Text)
	}
	if strings.Contains(got[0].Text, "Uszkoreit") {
		t.Fatalf("第四位作者不应出现: %q", got[0].Text)
	}
}

func TestRenderGBChineseUsesDeng(t *testing.T) {
	authors := []Author{
		{Family: "王", Given: "小明"},
		{Family: "李", Given: "华"},
		{Family: "张", Given: "三"},
		{Family: "赵", Given: "四"},
	}
	e := entry("wang2020", "中文文献测试", authors, 2020, "计算机学报")
	e.EntryType = "article"
	got, _ := RenderBibliography(FormatGBT7714, []Entry{e})
	if !strings.Contains(got[0].Text, "等") {
		t.Fatalf("中文条目超过 3 人应使用「等」: %q", got[0].Text)
	}
	if !strings.Contains(got[0].Text, "[J]") {
		t.Fatalf("期刊条目应带 [J]: %q", got[0].Text)
	}
}

func TestRenderGBBookTypeCode(t *testing.T) {
	e := entry("knuth1997", "TAOCP", []Author{{Family: "Knuth", Given: "D."}}, 1997, "")
	e.EntryType = "book"
	e.Publisher = "Addison-Wesley"
	got, _ := RenderBibliography(FormatGBT7714, []Entry{e})
	if !strings.Contains(got[0].Text, "[M]") {
		t.Fatalf("书籍条目应带 [M]: %q", got[0].Text)
	}
}

func TestRenderGBNoYearNoJournal(t *testing.T) {
	e := entry("anon", "Untitled Work", []Author{{Family: "Anon"}}, 0, "")
	e.EntryType = "misc"
	got, err := RenderBibliography(FormatGBT7714, []Entry{e})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := got[0].Text
	if strings.Contains(text, ", ,") || strings.Contains(text, ", .") {
		t.Fatalf("缺失字段不应留下空槽: %q", text)
	}
	if !strings.HasPrefix(text, "ANON. Untitled Work[Z]") {
		t.Fatalf("got %q", text)
	}
}

func TestRenderAPASingleAuthor(t *testing.T) {
	e := entry("vaswani2017", "Attention is all you need",
		[]Author{{Family: "Vaswani", Given: "Ashish"}}, 2017, "Advances in Neural Information Processing Systems")
	e.Volume = "30"
	e.Pages = "5998–6008"

	got, _ := RenderBibliography(FormatAPA, []Entry{e})
	want := `Vaswani, A. (2017). Attention is all you need. Advances in Neural Information Processing Systems, 30, 5998–6008.`
	if got[0].Text != want {
		t.Fatalf("got %q\nwant %q", got[0].Text, want)
	}
}

func TestRenderAPATwoAuthorsUsesAmpersand(t *testing.T) {
	e := entry("x", "Title", []Author{
		{Family: "Smith", Given: "John"},
		{Family: "Doe", Given: "Jane"},
	}, 2020, "Journal")
	got, _ := RenderBibliography(FormatAPA, []Entry{e})
	if !strings.Contains(got[0].Text, "Smith, J., & Doe, J.") {
		t.Fatalf("got %q", got[0].Text)
	}
}

func TestRenderAPANoYear(t *testing.T) {
	e := entry("x", "Title", []Author{{Family: "Smith", Given: "J."}}, 0, "Journal")
	got, _ := RenderBibliography(FormatAPA, []Entry{e})
	if !strings.Contains(got[0].Text, "(n.d.)") {
		t.Fatalf("无年份应显示 (n.d.): %q", got[0].Text)
	}
}

func TestRenderIEEEKeepsOrderAndNumbers(t *testing.T) {
	// 故意让字母序与传入顺序相反，验证 IEEE 不做字母排序。
	entries := []Entry{
		entry("z2020", "Zeta Paper", []Author{{Family: "Zulu", Given: "A."}}, 2020, "Journal Z"),
		entry("a2021", "Alpha Paper", []Author{{Family: "Alpha", Given: "B."}}, 2021, "Journal A"),
	}
	entries[0].Volume = "1"
	entries[0].Issue = "2"
	entries[0].Pages = "3–4"

	got, err := RenderBibliography(FormatIEEE, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got[0].Text, "[1] A. Zulu") {
		t.Fatalf("第一条应保持传入顺序并编号 [1]: %q", got[0].Text)
	}
	if !strings.HasPrefix(got[1].Text, "[2] B. Alpha") {
		t.Fatalf("第二条应为 [2]: %q", got[1].Text)
	}
	want := `[1] A. Zulu, "Zeta Paper," Journal Z, vol. 1, no. 2, pp. 3–4, 2020.`
	if got[0].Text != want {
		t.Fatalf("got %q\nwant %q", got[0].Text, want)
	}
}

func TestRenderSortsByFirstAuthorFamily(t *testing.T) {
	entries := []Entry{
		entry("z", "Z", []Author{{Family: "Zulu"}}, 2020, ""),
		entry("a", "A", []Author{{Family: "Alpha"}}, 2021, ""),
		entry("m", "M", []Author{{Family: "Mike"}}, 2019, ""),
	}
	for _, format := range []Format{FormatGBT7714, FormatAPA} {
		got, _ := RenderBibliography(format, entries)
		keys := []string{got[0].CiteKey, got[1].CiteKey, got[2].CiteKey}
		if keys[0] != "a" || keys[1] != "m" || keys[2] != "z" {
			t.Fatalf("%s 排序 = %v，期望 a,m,z", format, keys)
		}
	}
}

func TestRenderDoesNotMutateInput(t *testing.T) {
	entries := []Entry{
		entry("z", "Z", []Author{{Family: "Zulu"}}, 2020, ""),
		entry("a", "A", []Author{{Family: "Alpha"}}, 2021, ""),
	}
	_, _ = RenderBibliography(FormatAPA, entries)
	if entries[0].CiteKey != "z" {
		t.Fatal("渲染不应修改调用方的切片顺序")
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	_, err := RenderBibliography(Format("mla"), nil)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v，期望 ErrUnsupportedFormat", err)
	}
}

func TestParseFormat(t *testing.T) {
	for _, raw := range []string{"gbt7714", "GB/T7714", "APA", "ieee"} {
		if _, ok := ParseFormat(raw); !ok {
			t.Fatalf("ParseFormat(%q) 应成功", raw)
		}
	}
	if _, ok := ParseFormat("mla"); ok {
		t.Fatal("未知格式应失败")
	}
}

func TestRenderEmpty(t *testing.T) {
	got, err := RenderBibliography(FormatAPA, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空输入应返回空列表，got %d", len(got))
	}
}

func TestRenderIEEEBook(t *testing.T) {
	e := entry("knuth", "The Art of Computer Programming", []Author{{Family: "Knuth", Given: "Donald E."}}, 1997, "")
	e.EntryType = "book"
	e.Publisher = "Addison-Wesley"
	got, _ := RenderBibliography(FormatIEEE, []Entry{e})
	if strings.Contains(got[0].Text, `"`) {
		t.Fatalf("书籍标题不应加引号: %q", got[0].Text)
	}
	if !strings.Contains(got[0].Text, "Addison-Wesley") || !strings.Contains(got[0].Text, "1997") {
		t.Fatalf("got %q", got[0].Text)
	}
}
