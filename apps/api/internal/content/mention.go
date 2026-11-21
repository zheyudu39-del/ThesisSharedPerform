package content

import (
	"regexp"
	"strings"
)

// mentionPattern 匹配正文里的 @handle。
//
// 为什么前面要带一个「非字母数字/下划线/@」的字符：
//   - 邮箱地址 user@example.com 里的 @ 不应被当作提及；
//   - 连续写 @@foo 时只认第一个有效的 @。
//
// handle 规则与 identity 包的注册校验保持一致（2-32 位字母数字下划线）。
// 这里刻意不匹配连字符：@ 提及是面向中文用户的轻量语法，
// 收紧字符集可以避免把英文句子里的 "name@host" 误判为提及。
var mentionPattern = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_@])@([A-Za-z0-9_]{2,32})`)

// 代码块与行内代码里的 @ 不是提及，先整体剔除再扫描。
// 先处理围栏代码块（```），再处理行内代码（`...`），顺序不能反：
// 否则行内代码规则会先把围栏的 ``` 吃掉一部分，导致内容错位。
var (
	fencedCodePattern = regexp.MustCompile("(?s)```.*?```")
	inlineCodePattern = regexp.MustCompile("`[^`]*`")
)

// ExtractMentions 从纯文本中提取 @handle，去重并保持首次出现的顺序。
//
// 只做「提取」，不负责把 handle 解析成用户 ID：
// 解析需要一次数据库查询，交给服务层在事务提交后批量完成，
// 这样解析失败不会影响发帖/评论本身。
func ExtractMentions(text string) []string {
	if !strings.Contains(text, "@") {
		return nil
	}
	cleaned := fencedCodePattern.ReplaceAllString(text, " ")
	cleaned = inlineCodePattern.ReplaceAllString(cleaned, " ")

	matches := mentionPattern.FindAllStringSubmatch(cleaned, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		handle := match[1]
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		result = append(result, handle)
	}
	return result
}
