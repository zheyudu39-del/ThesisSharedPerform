package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

// 实体类型取值，与 search_documents.entity_type 的 varchar(16) 约定一致。
const (
	EntityPaper = "paper"
	EntityPost  = "post"
	EntityUser  = "user"
	EntityGroup = "group"
	// EntityFile 只用于保存 PDF 文本层，不属于可检索实体（见 IndexFileText）。
	EntityFile = "file"
)

// 查询参数边界。
const (
	DefaultLimit = 20
	MaxLimit     = 50
	// maxQueryRunes 限制查询长度：过长的输入既无意义，也会拖慢 trigram 计算。
	maxQueryRunes = 200
)

// 错误值，供 handler 做状态码映射。
var (
	// ErrValidation 表示入参不合法（空查询、未知实体类型、limit 越界等），对应 422。
	ErrValidation = errors.New("检索参数不合法")
)

// allowedEntityTypes 是允许出现在 Types 过滤里的取值集合。
// 刻意不含 EntityFile：PDF 文本层暂不单独参与检索（见 IndexFileText）。
var allowedEntityTypes = map[string]struct{}{
	EntityPaper: {},
	EntityPost:  {},
	EntityUser:  {},
	EntityGroup: {},
}

// IndexInput 描述一条待写入索引的文档。
type IndexInput struct {
	// EntityType 取 EntityPaper / EntityPost / EntityUser / EntityGroup。
	EntityType string
	EntityID   uuid.UUID
	Title      string
	Body       string
}

// QueryInput 是一次检索请求。
type QueryInput struct {
	// Text 为用户输入的关键词，会同时用于全文检索与模糊匹配。
	Text string
	// Types 为空表示不限定实体类型；非空时只返回这些类型的结果。
	Types []string
	// Limit 为 0 时取 DefaultLimit，上限 MaxLimit。
	Limit int

	// From / To 是创建时间范围（闭区间），nil 表示该侧不限。
	// 作用在实体自身的 created_at 上，而不是索引的 updated_at：
	// 用户筛选的是「什么时候发表的」，索引更新时间对用户没有意义。
	From *time.Time
	To   *time.Time
	// Tag 按标签过滤，匹配帖子与论文的标签。
	// 只有 posts / papers 有关联标签；其它实体在带 tag 筛选时被排除，
	// 而不是报错——用户看到「筛完没有结果」比「参数不支持」更自然。
	Tag string
	// Author 按作者 handle 过滤，只有帖子有作者概念；
	// 其它实体在带 author 筛选时同样被排除。
	Author string
}

// Item 是单条检索结果。
type Item struct {
	EntityType string    `json:"entityType"`
	EntityID   uuid.UUID `json:"entityId"`
	Title      string    `json:"title"`
	Snippet    string    `json:"snippet"`
	Rank       float32   `json:"rank"`
	// Slug 是实体的路由段：用户为 handle，群组为 slug，其余为空。
	// 前端据此拼出 /u/{handle} 或 /groups/{slug}；只有 entityId 时
	// 无法定位到可读地址。
	Slug string `json:"slug"`
}

// Result 是一次检索的完整结果。
type Result struct {
	Items []Item `json:"items"`
	// Total 是本次命中的结果条数（受 Limit 截断，最大为 Limit）。
	// MVP 不做总数统计：精确总数需要额外一次全表 count，
	// 而前端首屏只需要知道「还有更多」，由 len(Items) == Limit 判断即可。
	Total int `json:"total"`
}

// Querier 限定检索服务使用的查询，便于单元测试替身。
type Querier interface {
	UpsertSearchDocument(context.Context, db.UpsertSearchDocumentParams) error
	DeleteSearchDocument(context.Context, db.DeleteSearchDocumentParams) error
	SearchAll(context.Context, db.SearchAllParams) ([]db.SearchAllRow, error)
	SearchByTypes(context.Context, db.SearchByTypesParams) ([]db.SearchByTypesRow, error)
}

// filteredQuery 是一次带筛选条件的检索（时间 / 标签 / 作者）。
type filteredQuery struct {
	Query       string
	LikePattern string
	// Types 为空表示不限类型。
	Types []string
	// From / To 为 nil 表示该侧不限。
	From, To *time.Time
	// Tag / Author 为空表示不过滤；服务层已转小写并去掉 @。
	Tag    string
	Author string
	Limit  int32
}

// filteredRow 是带筛选检索的扫描目标。
// 与 searchRow 分开定义是因为 pgx 的按名扫描只认导出字段。
type filteredRow struct {
	EntityType      string      `db:"entity_type"`
	EntityID        pgtype.UUID `db:"entity_id"`
	Title           string      `db:"title"`
	Body            string      `db:"body"`
	Rank            float32     `db:"rank"`
	TitleSimilarity float32     `db:"title_similarity"`
	Slug            string      `db:"slug"`
}

// filteredQuerier 抽象带筛选的检索。
//
// 不把 SearchFiltered 放进 Querier：Querier 由单元测试替身实现，
// 新增方法会让所有替身编译失败；筛选查询也确实需要连接实体表做 JOIN，
// 与纯索引查询是两条路径。sqlc 生成 SearchFiltered 后，把 poolFilteredQuerier
// 换成 db 的薄封装即可，Service 逻辑不变。
type filteredQuerier interface {
	SearchFiltered(context.Context, filteredQuery) ([]filteredRow, error)
}

// Service 负责索引维护与统一检索。
//
// 索引同步方式（技术方案 §4.6 原计划用队列）：MVP 阶段由写入方在完成业务写库后
// 直接调用 Index / Remove，不引入异步队列；失败只记日志、不回滚业务，
// 索引缺失可以通过重建任务补齐。
type Service struct {
	querier Querier
	// filtered 负责带筛选条件的检索；为 nil 时（测试替身未实现）返回错误。
	filtered filteredQuerier
}

// NewService 创建检索服务。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{querier: db.New(pool), filtered: poolFilteredQuerier{pool: pool}}
}

// NewServiceWithQuerier 用于测试注入替身。
// 替身若实现了 filteredQuerier，带筛选的检索也会走替身。
func NewServiceWithQuerier(q Querier) *Service {
	svc := &Service{querier: q}
	if f, ok := q.(filteredQuerier); ok {
		svc.filtered = f
	}
	return svc
}

// Index 写入或更新一条索引（按 entity_type + entity_id 幂等）。
//
// 供 paper / content / identity / group 模块在写入成功后调用。
// 标题权重 A、正文权重 B，由 SQL 中的 setweight 完成。
func (s *Service) Index(ctx context.Context, doc IndexInput) error {
	if err := validateEntityType(doc.EntityType); err != nil {
		return err
	}
	if doc.EntityID == uuid.Nil {
		return fmt.Errorf("%w: entityId 不能为空", ErrValidation)
	}
	if strings.TrimSpace(doc.Title) == "" && strings.TrimSpace(doc.Body) == "" {
		// 没有任何可检索文本，写入也检索不到，直接跳过以免污染索引。
		return nil
	}
	return s.querier.UpsertSearchDocument(ctx, db.UpsertSearchDocumentParams{
		EntityType: doc.EntityType,
		EntityID:   pgtype.UUID{Bytes: doc.EntityID, Valid: true},
		Title:      nullableString(doc.Title),
		Body:       nullableString(doc.Body),
	})
}

// IndexPaperFullText 把论文的摘要与 PDF 文本层合并写入索引（PRD S-04）。
//
// 为什么正文要合并而不是分别写：search_documents 对 (entity_type, entity_id)
// 只有一行，索引正文是整行覆盖的；摘要与全文分开写会互相覆盖。
// 摘要在前、全文在后，既符合阅读顺序，也让 ts_rank 更偏向摘要命中。
//
// pdfText 为空（扫描件、无文本层）时只索引摘要——调用方不需要先判断。
func (s *Service) IndexPaperFullText(ctx context.Context, paperID uuid.UUID, title, abstract, pdfText string) error {
	body := strings.TrimSpace(abstract)
	if text := strings.TrimSpace(pdfText); text != "" {
		if body != "" {
			body += "\n\n"
		}
		body += text
	}
	return s.Index(ctx, IndexInput{
		EntityType: EntityPaper,
		EntityID:   paperID,
		Title:      title,
		Body:       body,
	})
}

// IndexFileText 为「尚未关联论文」的 PDF 建立索引。
//
// 实体类型取 file：检索接口原样返回 entityType，前端目前只渲染
// paper/post/user/group，因此这类结果暂时不会出现在结果列表里。
// 保留写入的意义是数据先就位——论文关联接口补齐后，同一份文本可以
// 直接改挂到 paper 上，不必回头重新解析 PDF。
func (s *Service) IndexFileText(ctx context.Context, fileID uuid.UUID, filename, pdfText string) error {
	if strings.TrimSpace(pdfText) == "" {
		return nil
	}
	if fileID == uuid.Nil {
		return fmt.Errorf("%w: fileId 不能为空", ErrValidation)
	}
	// 绕过 validateEntityType：file 不是可检索实体，但索引表允许任意
	// entity_type 字符串，这里直接写入以保留文本。
	// 用 UpsertSearchDocument 而不是单开一条 SQL：两者写入的列完全一致，
	// 多一条查询只会增加生成代码与维护面。
	return s.querier.UpsertSearchDocument(ctx, db.UpsertSearchDocumentParams{
		EntityType: EntityFile,
		EntityID:   pgtype.UUID{Bytes: fileID, Valid: true},
		Title:      nullableString(filename),
		Body:       nullableString(pdfText),
	})
}

// Remove 删除一条索引。实体被删除、隐藏或转为私有后调用。
// 索引本就不存在时不算错误（幂等）。
func (s *Service) Remove(ctx context.Context, entityType string, entityID uuid.UUID) error {
	if err := validateEntityType(entityType); err != nil {
		return err
	}
	if entityID == uuid.Nil {
		return fmt.Errorf("%w: entityId 不能为空", ErrValidation)
	}
	return s.querier.DeleteSearchDocument(ctx, db.DeleteSearchDocumentParams{
		EntityType: entityType,
		EntityID:   pgtype.UUID{Bytes: entityID, Valid: true},
	})
}

// Query 执行统一检索。
//
// 排序在 SQL 中完成：ts_rank 降序 → 标题相似度降序 → updated_at 降序。
func (s *Service) Query(ctx context.Context, input QueryInput) (Result, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return Result{}, fmt.Errorf("%w: 搜索关键词不能为空", ErrValidation)
	}
	if utf8.RuneCountInString(text) > maxQueryRunes {
		return Result{}, fmt.Errorf("%w: 搜索关键词不能超过 %d 个字符", ErrValidation, maxQueryRunes)
	}

	types, err := normalizeTypes(input.Types)
	if err != nil {
		return Result{}, err
	}
	limit := normalizeLimit(input.Limit)

	// 时间范围校验：两端都给时 from 不能晚于 to，否则必然零结果，
	// 直接报错比让用户对着空列表猜原因更清楚。
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return Result{}, fmt.Errorf("%w: 开始时间不能晚于结束时间", ErrValidation)
	}

	filters := normalizedFilters{tag: normalizeTag(input.Tag), author: normalizeAuthor(input.Author)}
	var rows []searchRow
	if filters.active() || input.From != nil || input.To != nil {
		rows, err = s.fetchFiltered(ctx, text, types, limit, input.From, input.To, filters)
	} else {
		rows, err = s.fetch(ctx, text, types, limit)
	}
	if err != nil {
		return Result{}, err
	}

	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, Item{
			EntityType: row.entityType,
			EntityID:   uuid.UUID(row.entityID.Bytes),
			Title:      row.title,
			Snippet:    snippet(row.body, text),
			// ts_rank 对「仅被 ILIKE / trigram 命中」的行恒为 0，
			// 叠加标题相似度让这类结果也有可比较的分数（权重取 0.5，不改变 SQL 的排序主次）。
			Rank: row.rank + 0.5*row.titleSimilarity,
			Slug: row.slug,
		})
	}
	return Result{Items: items, Total: len(items)}, nil
}

// searchRow 抹平 SearchAllRow / SearchByTypesRow 两个生成结构体的差异。
type searchRow struct {
	entityType      string
	entityID        pgtype.UUID
	title           string
	body            string
	rank            float32
	titleSimilarity float32
	slug            string
}

// fetch 按是否限定类型选择查询。
func (s *Service) fetch(ctx context.Context, text string, types []string, limit int32) ([]searchRow, error) {
	pattern := buildLikePattern(text)

	if len(types) == 0 {
		rows, err := s.querier.SearchAll(ctx, db.SearchAllParams{
			Query:       text,
			LikePattern: pattern,
			Limit:       limit,
		})
		if err != nil {
			return nil, err
		}
		out := make([]searchRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, searchRow{
				entityType: r.EntityType, entityID: r.EntityID, title: r.Title,
				body: r.Body, rank: r.Rank, titleSimilarity: r.TitleSimilarity,
				slug: r.Slug,
			})
		}
		return out, nil
	}

	rows, err := s.querier.SearchByTypes(ctx, db.SearchByTypesParams{
		Query:       text,
		LikePattern: pattern,
		Types:       types,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]searchRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, searchRow{
			entityType: r.EntityType, entityID: r.EntityID, title: r.Title,
			body: r.Body, rank: r.Rank, titleSimilarity: r.TitleSimilarity,
			slug: r.Slug,
		})
	}
	return out, nil
}

// normalizedFilters 是清洗后的标签 / 作者筛选值。
type normalizedFilters struct {
	tag    string
	author string
}

func (f normalizedFilters) active() bool { return f.tag != "" || f.author != "" }

// normalizeTag 统一成小写并去空白：标签 slug 在写入时就是小写，
// 这里保持同一套规则，避免「机器学习」与「机器学习 」被当成两个标签。
func normalizeTag(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// normalizeAuthor 接受 @handle 或 handle 两种写法。
func normalizeAuthor(raw string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
}

// fetchFiltered 走带筛选条件的检索。
//
// 筛选条件需要 JOIN 实体表（posts / papers）才能判断时间、标签与作者，
// 因此不能复用只查 search_documents 的 SearchAll / SearchByTypes。
func (s *Service) fetchFiltered(ctx context.Context, text string, types []string, limit int32, from, to *time.Time, filters normalizedFilters) ([]searchRow, error) {
	if s.filtered == nil {
		return nil, fmt.Errorf("%w: 当前部署不支持筛选检索", ErrValidation)
	}
	rows, err := s.filtered.SearchFiltered(ctx, filteredQuery{
		Query:       text,
		LikePattern: buildLikePattern(text),
		Types:       types,
		From:        from,
		To:          to,
		Tag:         filters.tag,
		Author:      filters.author,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]searchRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, searchRow{
			entityType: r.EntityType, entityID: r.EntityID, title: r.Title,
			body: r.Body, rank: r.Rank, titleSimilarity: r.TitleSimilarity,
			slug: r.Slug,
		})
	}
	return out, nil
}

// searchFilteredSQL 是带筛选的检索查询，与 db/queries/search.sql 的 SearchFiltered 一致。
//
// 时间 / 标签 / 作者条件需要实体自身的列，search_documents 没有这些列，
// 因此按 entity_type 分别 JOIN 回源表：
//   - post  → posts（author_id、tags 经 post_tags 关联）
//   - paper → papers（paper_tags 关联；论文没有作者 handle，只有 JSON 作者名）
//   - user / group → 没有 created_at 之外的筛选列，带 tag / author 时一律排除。
//
// 用 LEFT JOIN + 条件表达式而不是 INNER JOIN：一个条件只对某类实体有意义，
// INNER JOIN 会把不满足该条件的其它实体整类删掉，而不是「按规则排除」。
const searchFilteredSQL = `
WITH q AS (
    SELECT $1::text AS raw, websearch_to_tsquery('simple', $1::text) AS tsq
)
SELECT
    sd.entity_type,
    sd.entity_id,
    COALESCE(sd.title, '')::text AS title,
    COALESCE(sd.body, '')::text  AS body,
    ts_rank(sd.tsv, q.tsq)::real AS rank,
    similarity(COALESCE(sd.title, ''), q.raw)::real AS title_similarity,
    (CASE
      WHEN sd.entity_type = 'user'  THEN (SELECT u.handle FROM users u  WHERE u.id = sd.entity_id AND u.deleted_at IS NULL)
      WHEN sd.entity_type = 'group' THEN (SELECT g.slug   FROM groups g WHERE g.id = sd.entity_id)
      ELSE NULL
    END)::text AS slug
FROM search_documents sd
CROSS JOIN q
LEFT JOIN posts p  ON sd.entity_type = 'post'  AND p.id = sd.entity_id AND p.deleted_at IS NULL
LEFT JOIN papers pa ON sd.entity_type = 'paper' AND pa.id = sd.entity_id
WHERE (
        sd.tsv @@ q.tsq
     OR sd.title ILIKE $2::text
     OR sd.body ILIKE $2::text
     OR similarity(sd.title, q.raw) >= 0.2
)
  AND ($3::text[] IS NULL OR sd.entity_type = ANY($3::text[]))
  AND (
    ($4::timestamptz IS NULL AND $5::timestamptz IS NULL)
    OR (sd.entity_type = 'post' AND p.id IS NOT NULL
        AND ($4::timestamptz IS NULL OR p.created_at >= $4::timestamptz)
        AND ($5::timestamptz IS NULL OR p.created_at <= $5::timestamptz))
    OR (sd.entity_type = 'paper' AND pa.id IS NOT NULL
        AND ($4::timestamptz IS NULL OR pa.created_at >= $4::timestamptz)
        AND ($5::timestamptz IS NULL OR pa.created_at <= $5::timestamptz))
    OR (sd.entity_type NOT IN ('post', 'paper') AND sd.updated_at >= COALESCE($4::timestamptz, sd.updated_at)
        AND sd.updated_at <= COALESCE($5::timestamptz, sd.updated_at))
  )
  AND (
    $6::text IS NULL
    OR (sd.entity_type = 'post' AND p.id IS NOT NULL AND EXISTS(
          SELECT 1 FROM post_tags pt JOIN tags t ON t.id = pt.tag_id
          WHERE pt.post_id = p.id AND t.slug = $6::text))
    OR (sd.entity_type = 'paper' AND pa.id IS NOT NULL AND EXISTS(
          SELECT 1 FROM paper_tags pt JOIN tags t ON t.id = pt.tag_id
          WHERE pt.paper_id = pa.id AND t.slug = $6::text))
  )
  AND (
    $7::text IS NULL
    OR (sd.entity_type = 'post' AND p.id IS NOT NULL AND EXISTS(
          SELECT 1 FROM users u WHERE u.id = p.author_id AND lower(u.handle) = $7::text))
  )
ORDER BY rank DESC, title_similarity DESC, sd.updated_at DESC
LIMIT $8::int
`

type poolFilteredQuerier struct{ pool *pgxpool.Pool }

func (q poolFilteredQuerier) SearchFiltered(ctx context.Context, arg filteredQuery) ([]filteredRow, error) {
	var types []string
	if len(arg.Types) > 0 {
		types = arg.Types
	}
	rows, err := q.pool.Query(ctx, searchFilteredSQL,
		arg.Query, arg.LikePattern, types, nullableTime(arg.From), nullableTime(arg.To),
		nullableFilter(arg.Tag), nullableFilter(arg.Author), arg.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]filteredRow, 0, arg.Limit)
	for rows.Next() {
		item, err := pgx.RowToStructByNameLax[filteredRow](rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func nullableTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func nullableFilter(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// normalizeTypes 校验并去重实体类型；空切片表示不过滤。
func normalizeTypes(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	types := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if err := validateEntityType(t); err != nil {
			return nil, err
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		types = append(types, t)
	}
	return types, nil
}

// normalizeLimit 把 limit 收敛到 [1, MaxLimit]；0 或负数取默认值。
func normalizeLimit(limit int) int32 {
	switch {
	case limit <= 0:
		return DefaultLimit
	case limit > MaxLimit:
		return MaxLimit
	default:
		return int32(limit)
	}
}

func validateEntityType(entityType string) error {
	if _, ok := allowedEntityTypes[entityType]; !ok {
		return fmt.Errorf("%w: entityType 只能是 paper、post、user 或 group", ErrValidation)
	}
	return nil
}

func nullableString(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
