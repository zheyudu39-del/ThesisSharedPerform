package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
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
}

// Item 是单条检索结果。
type Item struct {
	EntityType string    `json:"entityType"`
	EntityID   uuid.UUID `json:"entityId"`
	Title      string    `json:"title"`
	Snippet    string    `json:"snippet"`
	Rank       float32   `json:"rank"`
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

// Service 负责索引维护与统一检索。
//
// 索引同步方式（技术方案 §4.6 原计划用队列）：MVP 阶段由写入方在完成业务写库后
// 直接调用 Index / Remove，不引入异步队列；失败只记日志、不回滚业务，
// 索引缺失可以通过重建任务补齐。
type Service struct {
	querier Querier
}

// NewService 创建检索服务。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{querier: db.New(pool)}
}

// NewServiceWithQuerier 用于测试注入替身。
func NewServiceWithQuerier(q Querier) *Service {
	return &Service{querier: q}
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

	rows, err := s.fetch(ctx, text, types, limit)
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
		})
	}
	return out, nil
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
