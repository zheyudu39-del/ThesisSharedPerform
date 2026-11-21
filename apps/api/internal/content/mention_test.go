package content

import (
	"reflect"
	"testing"
)

func TestExtractMentions(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "单个提及", text: "请 @alice 看一下", want: []string{"alice"}},
		{name: "多个提及保持顺序", text: "@bob 和 @alice 都看看", want: []string{"bob", "alice"}},
		{name: "重复提及去重", text: "@alice @alice @bob", want: []string{"alice", "bob"}},
		{name: "邮箱不算提及", text: "联系 user@example.com", want: nil},
		{name: "行内代码里的不算", text: "执行 `@cmd` 命令", want: nil},
		{name: "围栏代码块里的不算", text: "```\n@alice\n```\n@bob", want: []string{"bob"}},
		{name: "太短不匹配", text: "@a 你好", want: nil},
		{name: "含连字符只取前缀", text: "@alice-bob 你好", want: []string{"alice"}},
		{name: "中文标点后的提及", text: "（@carol）看看", want: []string{"carol"}},
		{name: "无提及", text: "普通文本", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMentions(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExtractMentions(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
