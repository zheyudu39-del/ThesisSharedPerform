package citation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/thesis-shared/api/internal/doc"
)

// Querier 限定引用服务使用的查询。
//
// 这里刻意不用 sqlc 生成的具体参数/返回类型，而是用本包定义的
// UpsertReferenceParams / Entry 等形状：sqlc 尚未生成时本包也能编译与
// 单元测试，接线时只需写一个薄适配器把生成类型与本包类型互转。
//
// 所有方法都只做单条/单次查询。导入不做事务是刻意的：每条 upsert 本身
// 幂等，中途失败后重新导入同一份 .bib 会收敛到相同结果，而事务反而会
// 让一个大文件的失败代价变成「全部回滚、一条都没进」。
type Querier interface {
	// GetDocumentAccessForReference 返回权限判定所需的事实；
	// 文档不存在/已删除时返回 pgx.ErrNoRows。
	GetDocumentAccessForReference(ctx context.Context, docID, userID uuid.UUID) (AccessRow, error)
	// UpsertDocReference 幂等写入一条引用（cite_key 冲突时更新）。
	UpsertDocReference(ctx context.Context, params UpsertReferenceParams) (Entry, error)
	// ListDocReferences 返回文档的全部引用，按创建顺序。
	ListDocReferences(ctx context.Context, docID uuid.UUID) ([]Entry, error)
	// DeleteDocReference 删除引用，返回受影响行数。
	DeleteDocReference(ctx context.Context, docID, refID uuid.UUID) (int64, error)
}

// Service 实现引用领域逻辑。
type Service struct {
	queries Querier
}

// NewService 用查询实现构造服务。
//
// 不接受 *pgxpool.Pool：接线代理需要提供一个把 sqlc 生成类型转换成本包
// 类型的适配器（见 package 文档与返回说明）。这样引用模块在 sqlc 生成前
// 也能独立编译与测试。
func NewService(queries Querier) *Service {
	return &Service{queries: queries}
}

// ---------------------------------------------------------------------------
// 权限
// ---------------------------------------------------------------------------

// requireRole 读取文档权限事实并断言满足 predicate。
//
// 权限规则直接复用 doc 包的 EffectiveRole 纯函数：引用与文档是同一份
// 访问模型，各写一套只会让两者逐渐不一致（例如某天文档权限调整了，
// 引用接口却还按旧规则放行）。
//
// 完全无权查看返回 ErrNotFound（避免泄露存在性），能看不能写返回 ErrForbidden。
func (s *Service) requireRole(ctx context.Context, userID, docID uuid.UUID, allowed func(doc.Role) bool) error {
	row, err := s.queries.GetDocumentAccessForReference(ctx, docID, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("查询文档权限失败: %w", err)
	}
	role := doc.EffectiveRole(doc.AccessFacts{
		UserID:       userID,
		GroupID:      row.GroupID,
		CreatedBy:    row.CreatedBy,
		GroupOwnerID: row.GroupOwnerID,
		GroupRole:    row.GroupRole,
		DocRole:      doc.Role(row.DocRole),
		DefaultRole:  doc.Role(row.DefaultRole),
	})
	if allowed(role) {
		return nil
	}
	if !doc.CanView(role) {
		return ErrNotFound
	}
	return ErrForbidden
}

// canRead 与 canWrite 是引用接口对文档权限的映射：
//   - 查看引用 = 能查看文档；
//   - 增删引用 = 能编辑文档（editor 及以上）。
func canRead(role doc.Role) bool  { return doc.CanView(role) }
func canWrite(role doc.Role) bool { return doc.CanEdit(role) }

// ---------------------------------------------------------------------------
// 导入与添加
// ---------------------------------------------------------------------------

// ImportBibTeX 解析并幂等导入 BibTeX 文本。
//
// 「幂等」的含义：同一份 .bib 反复导入不会产生重复条目，第二次导入
// 只会把字段更新成最新值。因此返回值里的 Imported 是本次处理的条目数，
// 而不是新增数——调用方不需要区分新增与更新。
func (s *Service) ImportBibTeX(ctx context.Context, userID, docID uuid.UUID, raw string) (ImportResult, error) {
	if strings.TrimSpace(raw) == "" {
		return ImportResult{}, fmt.Errorf("%w: BibTeX 文本不能为空", ErrValidation)
	}
	if utf8.RuneCountInString(raw) > 1_000_000 {
		// 单篇文档的参考文献不可能有 1MB；限制只用于防止误粘贴整个数据库导出。
		return ImportResult{}, fmt.Errorf("%w: BibTeX 文本过大（上限 1MB）", ErrValidation)
	}
	if err := s.requireRole(ctx, userID, docID, canWrite); err != nil {
		return ImportResult{}, err
	}
	parsed, err := ParseBibTeX(raw)
	if err != nil {
		return ImportResult{}, err
	}
	if len(parsed) == 0 {
		return ImportResult{}, fmt.Errorf("%w: 未找到可导入的 BibTeX 条目", ErrValidation)
	}

	entries := make([]Entry, 0, len(parsed))
	for _, bib := range parsed {
		params, err := paramsFromBib(docID, userID, bib)
		if err != nil {
			return ImportResult{}, err
		}
		stored, err := s.queries.UpsertDocReference(ctx, params)
		if err != nil {
			return ImportResult{}, fmt.Errorf("写入引用失败: %w", err)
		}
		entries = append(entries, stored)
	}
	return ImportResult{Imported: len(entries), Entries: entries}, nil
}

// paramsFromBib 把解析结果转换为写入参数，并做字段级校验。
func paramsFromBib(docID, userID uuid.UUID, bib BibEntry) (UpsertReferenceParams, error) {
	key := strings.TrimSpace(bib.Key)
	if key == "" {
		return UpsertReferenceParams{}, fmt.Errorf("%w: 缺少 cite key", ErrValidation)
	}
	if utf8.RuneCountInString(key) > 64 {
		return UpsertReferenceParams{}, fmt.Errorf("%w: cite key 不能超过 64 个字符", ErrValidation)
	}
	title := strings.TrimSpace(bib.Fields["title"])
	if title == "" {
		// 没有题名的条目在参考文献表里无法辨认，直接拒绝并指出是哪一条。
		return UpsertReferenceParams{}, fmt.Errorf("%w: 条目 %s 缺少 title", ErrValidation, key)
	}
	authors := ParseAuthors(bib.Fields["author"])
	raw := bib.Fields
	entryType := bib.Type
	if entryType == "" {
		entryType = "misc"
	}
	var year *int32
	if y := parseYear(raw["year"]); y > 0 {
		year = &y
	}
	// 期刊名与会议名在 BibTeX 里是两个字段，统一落到 container：
	// 参考文献格式对「期刊还是会议」的处理相同（都是容器），分开存
	// 只会让渲染层多一次判空。
	container := firstNonEmpty(raw["journal"], raw["booktitle"], raw["container"], raw["publisher"])
	params := UpsertReferenceParams{
		DocID:     docID,
		CiteKey:   key,
		EntryType: entryType,
		Title:     title,
		Authors:   authors,
		Year:      year,
		Container: optString(container),
		Volume:    optString(raw["volume"]),
		Issue:     optString(firstNonEmpty(raw["number"], raw["issue"])),
		Pages:     optString(normalizePages(raw["pages"])),
		DOI:       optString(raw["doi"]),
		URL:       optString(raw["url"]),
		Publisher: optString(raw["publisher"]),
		CreatedBy: &userID,
	}
	// raw_bibtex 保留一份紧凑原文，便于用户回看与再次导出。
	params.RawBibTeX = optString(renderRawBib(bib))
	return params, nil
}

// AddReference 手工添加一条引用。
func (s *Service) AddReference(ctx context.Context, userID, docID uuid.UUID, input AddReferenceInput) (Entry, error) {
	if err := s.requireRole(ctx, userID, docID, canWrite); err != nil {
		return Entry{}, err
	}
	key := strings.TrimSpace(input.CiteKey)
	if key == "" {
		return Entry{}, fmt.Errorf("%w: cite key 不能为空", ErrValidation)
	}
	if utf8.RuneCountInString(key) > 64 {
		return Entry{}, fmt.Errorf("%w: cite key 不能超过 64 个字符", ErrValidation)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Entry{}, fmt.Errorf("%w: 题名不能为空", ErrValidation)
	}
	if utf8.RuneCountInString(title) > 2000 {
		return Entry{}, fmt.Errorf("%w: 题名过长", ErrValidation)
	}
	entryType := strings.TrimSpace(input.EntryType)
	if entryType == "" {
		entryType = "article"
	}
	normalized, ok := bibEntryTypes[entryType]
	if !ok {
		return Entry{}, fmt.Errorf("%w: 条目类型只能是 article、inproceedings、book 或 misc", ErrValidation)
	}
	entryType = normalized

	authors := make([]Author, 0, len(input.Authors))
	for _, a := range input.Authors {
		if a.Family == "" && a.Given == "" && a.Literal == "" {
			continue
		}
		authors = append(authors, Author{
			Family: strings.TrimSpace(a.Family), Given: strings.TrimSpace(a.Given),
			Literal: strings.TrimSpace(a.Literal),
		})
	}
	var year *int32
	if input.Year != nil && *input.Year > 0 {
		y := int32(*input.Year)
		year = &y
	}
	params := UpsertReferenceParams{
		DocID:     docID,
		CiteKey:   key,
		EntryType: entryType,
		Title:     title,
		Authors:   authors,
		Year:      year,
		Container: optString(input.Container),
		Volume:    optString(input.Volume),
		Issue:     optString(input.Issue),
		Pages:     optString(input.Pages),
		DOI:       optString(input.DOI),
		URL:       optString(input.URL),
		Publisher: optString(input.Publisher),
		CreatedBy: &userID,
	}
	stored, err := s.queries.UpsertDocReference(ctx, params)
	if err != nil {
		return Entry{}, fmt.Errorf("写入引用失败: %w", err)
	}
	return stored, nil
}

// ---------------------------------------------------------------------------
// 列表与删除
// ---------------------------------------------------------------------------

// ListReferences 返回文档的全部引用，要求能查看文档。
func (s *Service) ListReferences(ctx context.Context, userID, docID uuid.UUID) (ReferencesResult, error) {
	if err := s.requireRole(ctx, userID, docID, canRead); err != nil {
		return ReferencesResult{}, err
	}
	entries, err := s.queries.ListDocReferences(ctx, docID)
	if err != nil {
		return ReferencesResult{}, fmt.Errorf("查询引用失败: %w", err)
	}
	if entries == nil {
		entries = []Entry{}
	}
	return ReferencesResult{References: entries}, nil
}

// DeleteReference 删除一条引用，要求能编辑文档。
func (s *Service) DeleteReference(ctx context.Context, userID, docID, refID uuid.UUID) error {
	if err := s.requireRole(ctx, userID, docID, canWrite); err != nil {
		return err
	}
	affected, err := s.queries.DeleteDocReference(ctx, docID, refID)
	if err != nil {
		return fmt.Errorf("删除引用失败: %w", err)
	}
	if affected == 0 {
		return ErrReferenceNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// 参考文献渲染
// ---------------------------------------------------------------------------

// Bibliography 渲染指定格式的参考文献表。
//
// 查看引用只需要读权限：参考文献是正文的一部分，viewer 也要能看到。
func (s *Service) Bibliography(ctx context.Context, userID, docID uuid.UUID, rawFormat string) (Bibliography, error) {
	format, ok := ParseFormat(rawFormat)
	if !ok {
		return Bibliography{}, fmt.Errorf("%w: format 只能是 gbt7714、apa 或 ieee", ErrUnsupportedFormat)
	}
	if err := s.requireRole(ctx, userID, docID, canRead); err != nil {
		return Bibliography{}, err
	}
	entries, err := s.queries.ListDocReferences(ctx, docID)
	if err != nil {
		return Bibliography{}, fmt.Errorf("查询引用失败: %w", err)
	}
	rendered, err := RenderBibliography(format, entries)
	if err != nil {
		return Bibliography{}, err
	}
	return Bibliography{Format: string(format), Entries: rendered}, nil
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// parseYear 从可能带杂质的年份字段里提取四位年份。
// BibTeX 的 year 有时是 "2020"、"{2020}"，偶尔是 "2020a"（同作者同年多篇）。
func parseYear(raw string) int32 {
	raw = strings.TrimSpace(raw)
	digits := make([]rune, 0, 4)
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
			if len(digits) == 4 {
				break
			}
		} else if len(digits) > 0 {
			break
		}
	}
	if len(digits) != 4 {
		return 0
	}
	year := int32(0)
	for _, r := range digits {
		year = year*10 + (r - '0')
	}
	// 明显不合理的年份（早于印刷术或晚于 100 年后）视为未知，
	// 避免脏数据把排序键推到极端位置。
	if year < 1000 || year > 3000 {
		return 0
	}
	return year
}

// normalizePages 把 BibTeX 的 "--" 页码范围转成 "–"（单破折号）。
func normalizePages(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "---", "–")
	raw = strings.ReplaceAll(raw, "--", "–")
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func optString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// renderRawBib 重建一份紧凑的 BibTeX 原文，用于 raw_bibtex 字段。
// 不追求与原文件逐字节一致，只求保留足以回看的字段。
func renderRawBib(bib BibEntry) string {
	keys := []string{"author", "title", "journal", "booktitle", "year", "volume", "number", "pages", "doi", "url", "publisher"}
	var b strings.Builder
	fmt.Fprintf(&b, "@%s{%s", bib.Type, bib.Key)
	for _, key := range keys {
		value, ok := bib.Fields[key]
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		fmt.Fprintf(&b, ",\n  %s = {%s}", key, value)
	}
	b.WriteString("\n}")
	return b.String()
}
