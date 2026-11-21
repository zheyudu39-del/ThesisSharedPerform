package citation

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Author 是一条文献的作者。
//
// 两种形态互斥：
//   - 个人作者：Family（姓）+ Given（名/缩写）；
//   - 机构作者：Literal（如「World Health Organization」），整段原样输出。
//
// 之所以不用「一个字符串」表示作者，是因为三种引用格式对姓与名的
// 排列顺序、缩写方式要求完全不同（APA 是 Family, G.，IEEE 是 G. Family），
// 拆成结构化字段后渲染层才不需要反向解析字符串。
type Author struct {
	Family  string `json:"family,omitempty"`
	Given   string `json:"given,omitempty"`
	Literal string `json:"literal,omitempty"`
}

// UnmarshalJSON 允许作者以纯字符串形式提交（视为机构作者或整段姓名）。
// 手工添加接口的调用方未必愿意构造 {family, given} 对象，放宽输入
// 比返回 422 更友好；解析失败时回退到 Literal。
func (a *Author) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var literal string
		if err := json.Unmarshal(data, &literal); err != nil {
			return err
		}
		*a = Author{Literal: strings.TrimSpace(literal)}
		return nil
	}
	type alias Author
	var parsed alias
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*a = Author(parsed)
	return nil
}

// DisplayName 返回最适合展示的完整姓名，供前端列表使用。
func (a Author) DisplayName() string {
	if a.Literal != "" {
		return a.Literal
	}
	switch {
	case a.Family != "" && a.Given != "":
		return a.Family + ", " + a.Given
	case a.Family != "":
		return a.Family
	default:
		return a.Given
	}
}

// Entry 是对外的引用条目 DTO，字段与前端契约严格一致。
// Year 为 0 表示未知年份（数据库里存 NULL）。
type Entry struct {
	ID        uuid.UUID `json:"id"`
	DocID     uuid.UUID `json:"docId"`
	CiteKey   string    `json:"citeKey"`
	EntryType string    `json:"entryType"`
	Title     string    `json:"title"`
	Authors   []Author  `json:"authors"`
	Year      int32     `json:"year"`
	Container string    `json:"container"`
	Volume    string    `json:"volume"`
	Issue     string    `json:"issue"`
	Pages     string    `json:"pages"`
	DOI       string    `json:"doi"`
	URL       string    `json:"url"`
	Publisher string    `json:"publisher"`
	CreatedAt time.Time `json:"createdAt"`
}

// ImportResult 是 BibTeX 导入的返回体：导入条数与最终落库的条目列表。
// 重复 cite_key 走更新而非报错，因此 Imported 统计的是「处理条数」。
type ImportResult struct {
	Imported int     `json:"imported"`
	Entries  []Entry `json:"entries"`
}

// ReferencesResult 是引用列表的返回体。
type ReferencesResult struct {
	References []Entry `json:"references"`
}

// AddReferenceInput 是手工添加引用的入参。
type AddReferenceInput struct {
	CiteKey   string
	EntryType string
	Title     string
	Authors   []Author
	Year      *int
	Container string
	Volume    string
	Issue     string
	Pages     string
	DOI       string
	URL       string
	Publisher string
}

// BibliographyEntry 是渲染后的一条参考文献。
//
// SortKey 暴露给前端做本地二次排序或调试；Text 是最终展示文本。
type BibliographyEntry struct {
	CiteKey string `json:"citeKey"`
	Text    string `json:"text"`
	SortKey string `json:"sortKey"`
}

// Bibliography 是某个格式下的完整参考文献表。
type Bibliography struct {
	Format  string              `json:"format"`
	Entries []BibliographyEntry `json:"entries"`
}

// AccessRow 是权限判定所需的事实，由 Querier 从文档、群组与成员表取回。
// 用本包的本地类型而不是 sqlc 生成类型，是为了让本包在 sqlc 生成之前
// 也能编译与测试；接线时只需实现一个很薄的适配器。
type AccessRow struct {
	DocID        uuid.UUID
	GroupID      uuid.UUID
	CreatedBy    uuid.UUID
	GroupOwnerID uuid.UUID
	// GroupRole 是群内角色文本，空串表示非群成员。
	GroupRole string
	// DocRole 是 doc_members 的覆盖角色文本，空串表示无覆盖。
	DocRole string
	// DefaultRole 是 documents.default_role 文本。
	DefaultRole string
}

// UpsertReferenceParams 是幂等写入一条引用所需的字段。
// 可空字段用指针表示，nil 表示「本次没有提供」。
type UpsertReferenceParams struct {
	DocID     uuid.UUID
	CiteKey   string
	EntryType string
	Title     string
	Authors   []Author
	Year      *int32
	Container *string
	Volume    *string
	Issue     *string
	Pages     *string
	DOI       *string
	URL       *string
	Publisher *string
	RawBibTeX *string
	CreatedBy *uuid.UUID
}
