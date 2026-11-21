package citation

import (
	"errors"
	"testing"
)

func TestParseBibTeXBasic(t *testing.T) {
	input := `
@article{vaswani2017,
  author = {Vaswani, Ashish and Shazeer, Noam and Parmar, Niki},
  title = {Attention is All You Need},
  journal = {Advances in Neural Information Processing Systems},
  year = {2017},
  volume = {30},
  pages = {5998--6008}
}
`
	entries, err := ParseBibTeX(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("条目数 = %d，期望 1", len(entries))
	}
	e := entries[0]
	if e.Key != "vaswani2017" || e.Type != "article" {
		t.Fatalf("key/type = %q/%q", e.Key, e.Type)
	}
	if got := e.Fields["title"]; got != "Attention is All You Need" {
		t.Fatalf("title = %q", got)
	}
	if got := e.Fields["journal"]; got != "Advances in Neural Information Processing Systems" {
		t.Fatalf("journal = %q", got)
	}
}

func TestParseBibTeXMultipleEntriesAndTypes(t *testing.T) {
	input := `
@inproceedings{lecun1998,
  author = {LeCun, Yann and Bottou, Léon},
  title = {Gradient-based learning applied to document recognition},
  booktitle = {Proceedings of the IEEE},
  year = 1998
}
@book{knuth1997,
  author = {Knuth, Donald E.},
  title = {The Art of Computer Programming},
  publisher = {Addison-Wesley},
  year = {1997}
}
@misc{web2020,
  title = {Some Web Resource},
  url = {https://example.com},
  year = {2020}
}
@phdthesis{thesis2019,
  author = {Doe, Jane},
  title = {A Thesis},
  year = {2019}
}
`
	entries, err := ParseBibTeX(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("条目数 = %d，期望 4", len(entries))
	}
	wantTypes := []string{"inproceedings", "book", "misc", "misc"}
	for i, want := range wantTypes {
		if entries[i].Type != want {
			t.Fatalf("条目 %d 类型 = %q，期望 %q", i, entries[i].Type, want)
		}
	}
}

func TestParseBibTeXSkipsCommentAndString(t *testing.T) {
	input := `
@comment{this is ignored}
@string{acm = "ACM"}
@article{real2020,
  title = {Real Entry},
  year = {2020}
}
`
	entries, err := ParseBibTeX(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "real2020" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestParseBibTeXMissingKey(t *testing.T) {
	_, err := ParseBibTeX(`@article{, title = {No Key}}`)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v，期望 ErrValidation", err)
	}
}

func TestParseBibTeXUnclosed(t *testing.T) {
	_, err := ParseBibTeX(`@article{x, title = {Broken}`)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v，期望 ErrValidation", err)
	}
}

func TestParseBibTeXDuplicateKeyLastWins(t *testing.T) {
	input := `
@article{k, title = {First}, year = {2001}}
@article{k, title = {Second}, year = {2002}}
`
	entries, err := ParseBibTeX(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("条目数 = %d，期望 1（同 key 去重）", len(entries))
	}
	if entries[0].Fields["title"] != "Second" {
		t.Fatalf("title = %q，期望后出现的条目胜出", entries[0].Fields["title"])
	}
}

func TestParseBibTeXQuotedValuesAndConcatenation(t *testing.T) {
	input := `@article{q, title = "Quoted Title", year = "20" # "20"}`
	entries, err := ParseBibTeX(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Fields["title"] != "Quoted Title" {
		t.Fatalf("title = %q", entries[0].Fields["title"])
	}
	if entries[0].Fields["year"] != "2020" {
		t.Fatalf("year = %q，期望拼接为 2020", entries[0].Fields["year"])
	}
}

func TestParseAuthors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Author
	}{
		{
			name: "姓, 名 形式",
			in:   "Vaswani, Ashish and Shazeer, Noam",
			want: []Author{{Family: "Vaswani", Given: "Ashish"}, {Family: "Shazeer", Given: "Noam"}},
		},
		{
			name: "名 姓 形式",
			in:   "Ashish Vaswani and Noam Shazeer",
			want: []Author{{Family: "Vaswani", Given: "Ashish"}, {Family: "Shazeer", Given: "Noam"}},
		},
		{
			name: "单作者",
			in:   "Knuth, Donald E.",
			want: []Author{{Family: "Knuth", Given: "Donald E."}},
		},
		{
			name: "机构作者",
			in:   "{World Health Organization}",
			want: []Author{{Literal: "World Health Organization"}},
		},
		{
			name: "后缀",
			in:   "King, Jr, Martin Luther",
			want: []Author{{Family: "King", Given: "Martin Luther"}},
		},
		{
			name: "空",
			in:   "",
			want: []Author{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAuthors(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("作者数 = %d，期望 %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("作者 %d = %+v，期望 %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseAuthorsKeepsAndInsideBraces(t *testing.T) {
	// 机构名里的 "and" 不能被当作作者分隔符。
	got := ParseAuthors("{Research and Development Lab} and Smith, John")
	if len(got) != 2 {
		t.Fatalf("作者数 = %d，期望 2: %+v", len(got), got)
	}
	if got[0].Literal != "Research and Development Lab" {
		t.Fatalf("got[0] = %+v", got[0])
	}
	if got[1].Family != "Smith" {
		t.Fatalf("got[1] = %+v", got[1])
	}
}

func TestCleanBibTeXTextAccents(t *testing.T) {
	tests := map[string]string{
		`G{\"o}del`:      "Gödel",
		`Erd\H{o}s`:      "Erdos", // 未知重音命令退化为基字母
		`L{\'e}on`:       "Léon",
		`{Attention}`:    "Attention",
		`a~b`:            "a b",
		`Smith \& Jones`: "Smith & Jones",
		`\c{c}a va`:      "ça va",
	}
	for in, want := range tests {
		if got := cleanBibTeXText(in); got != want {
			t.Fatalf("cleanBibTeXText(%q) = %q，期望 %q", in, got, want)
		}
	}
}
