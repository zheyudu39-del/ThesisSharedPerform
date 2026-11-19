package paper

import "testing"

func TestIdentify(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		kind   Source
		value  string
		wantOK bool
	}{
		{"裸 DOI", "10.1145/3292500.3330701", SourceDOI, "10.1145/3292500.3330701", true},
		{"DOI 大写归一", "10.1145/ABC123", SourceDOI, "10.1145/abc123", true},
		{"DOI URL", "https://doi.org/10.1038/nature12373", SourceDOI, "10.1038/nature12373", true},
		{"DOI dx URL", "http://dx.doi.org/10.1000/xyz", SourceDOI, "10.1000/xyz", true},
		{"DOI 前缀", "doi:10.1145/1234.5678", SourceDOI, "10.1145/1234.5678", true},

		{"arXiv 新式裸编号", "2405.13412", SourceArxiv, "2405.13412", true},
		{"arXiv 带版本", "2405.13412v3", SourceArxiv, "2405.13412", true},
		{"arXiv abs URL", "https://arxiv.org/abs/2405.13412", SourceArxiv, "2405.13412", true},
		{"arXiv pdf URL", "https://arxiv.org/pdf/2405.13412v2.pdf", SourceArxiv, "2405.13412", true},
		{"arXiv 旧式编号", "hep-th/9901001", SourceArxiv, "hep-th/9901001", true},

		{"OpenReview URL", "https://openreview.net/forum?id=abc123XYZ", SourceOpenRev, "abc123XYZ", true},

		{"无效输入", "随便写点东西", "", "", false},
		{"空字符串", "", "", "", false},
		{"普通 URL", "https://example.com/paper", "", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, ok := Identify(c.input)
			if ok != c.wantOK {
				t.Fatalf("Identify(%q) ok=%v, want %v", c.input, ok, c.wantOK)
			}
			if !ok {
				return
			}
			if id.Kind != c.kind {
				t.Errorf("kind=%q, want %q", id.Kind, c.kind)
			}
			if id.Value != c.value {
				t.Errorf("value=%q, want %q", id.Value, c.value)
			}
		})
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := map[string]string{
		"Attention Is All You Need":   "attention is all you need",
		"Attention: Is All You Need!": "attention is all you need",
		"  多余   空格  ":                 "多余 空格",
		"BERT-based Models":           "bert based models",
	}
	for input, want := range cases {
		if got := normalizeTitle(input); got != want {
			t.Errorf("normalizeTitle(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestStripJats(t *testing.T) {
	input := `<jats:p>This is <jats:italic>important</jats:italic> text.</jats:p>`
	want := "This is important text."
	if got := stripJats(input); got != want {
		t.Errorf("stripJats=%q, want %q", got, want)
	}
}

func TestDedupeHashSkipsStrongIdentifiers(t *testing.T) {
	meta := Metadata{DOI: "10.1000/xyz", Title: "Some Paper"}
	if h := dedupeHash(meta); h != "" {
		t.Errorf("有 DOI 时不应生成软去重哈希, got %q", h)
	}

	meta2 := Metadata{Title: "Some Paper", Authors: []Author{{Name: "Alice"}}, PublishedYear: 2024}
	if h := dedupeHash(meta2); h == "" {
		t.Error("无强标识时应生成软去重哈希")
	}
}
