package group

import (
	"errors"
	"strings"
	"testing"
)

func TestCanViewGroupFiles(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleMember, true},
		{"", false},
	}
	for _, tt := range tests {
		if got := CanViewGroupFiles(tt.role); got != tt.want {
			t.Fatalf("CanViewGroupFiles(%q) = %v，期望 %v", tt.role, got, tt.want)
		}
	}
}

func TestCanSaveGroupFile(t *testing.T) {
	if !CanSaveGroupFile(RoleMember) {
		t.Fatal("普通成员应能存入文件")
	}
	if CanSaveGroupFile("") {
		t.Fatal("非成员不应能存入文件")
	}
}

func TestCanDeleteGroupFile(t *testing.T) {
	tests := []struct {
		name    string
		role    Role
		isAdder bool
		want    bool
	}{
		{"存入者本人是普通成员", RoleMember, true, true},
		{"存入者本人是管理员", RoleAdmin, true, true},
		{"普通成员删他人条目", RoleMember, false, false},
		{"管理员删他人条目", RoleAdmin, false, true},
		{"群主删他人条目", RoleOwner, false, true},
		{"非成员删自己条目（已退群）", "", true, true},
		{"非成员删他人条目", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanDeleteGroupFile(tt.role, tt.isAdder); got != tt.want {
				t.Fatalf("got %v，期望 %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeTags(t *testing.T) {
	t.Run("去空白与去重", func(t *testing.T) {
		got, err := normalizeTags([]string{" NLP ", "nlp", "", "  ", "Graph"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"NLP", "Graph"}
		if len(got) != len(want) {
			t.Fatalf("got %v，期望 %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v，期望 %v", got, want)
			}
		}
	})

	t.Run("空输入返回空切片而非 nil", func(t *testing.T) {
		got, err := normalizeTags(nil)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got %v, err %v", got, err)
		}
	})

	t.Run("标签过长", func(t *testing.T) {
		_, err := normalizeTags([]string{strings.Repeat("字", groupFileTagMax+1)})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v，期望 ErrValidation", err)
		}
	})

	t.Run("标签过多", func(t *testing.T) {
		tags := make([]string, 0, groupFileTagsLimit+1)
		for i := 0; i <= groupFileTagsLimit; i++ {
			tags = append(tags, string(rune('a'+i)))
		}
		_, err := normalizeTags(tags)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v，期望 ErrValidation", err)
		}
	})
}
