package paper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

// Paper 是论文实体对外暴露的形状。
type Paper struct {
	ID            uuid.UUID `json:"id"`
	DOI           string    `json:"doi"`
	ArxivID       string    `json:"arxivId"`
	OpenreviewID  string    `json:"openreviewId"`
	Title         string    `json:"title"`
	Authors       []Author  `json:"authors"`
	Abstract      string    `json:"abstract"`
	Venue         string    `json:"venue"`
	PublishedYear int       `json:"publishedYear"`
	CitationCount int       `json:"citationCount"`
	MetaSource    string    `json:"metaSource"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ResolveResult 表示一次「识别 + 抓取 + 入库」的结果。
type ResolveResult struct {
	Paper Paper `json:"paper"`
	// Existing 为 true 表示命中了已有论文（按 DOI / arXiv / 去重哈希），未新建。
	Existing bool `json:"existing"`
}

// Querier 限定论文服务使用的查询，便于单元测试替身。
type Querier interface {
	CreatePaper(context.Context, db.CreatePaperParams) (db.Paper, error)
	GetPaperByDOI(context.Context, *string) (db.Paper, error)
	GetPaperByArxivID(context.Context, *string) (db.Paper, error)
	GetPaperByDedupeHash(context.Context, *string) (db.Paper, error)
	GetPaperByID(context.Context, pgtype.UUID) (db.Paper, error)
	ListPapers(context.Context, db.ListPapersParams) ([]db.Paper, error)
	CountPapers(context.Context) (int64, error)
}

// MetadataFetcher 抽象外部抓取，便于测试。
type MetadataFetcher interface {
	Fetch(context.Context, Identifier) (Metadata, error)
}

// Service 处理论文识别、抓取与入库。
type Service struct {
	pool    *pgxpool.Pool
	querier Querier
	fetcher MetadataFetcher
}

// NewService 创建论文服务。
func NewService(pool *pgxpool.Pool, fetcher MetadataFetcher) *Service {
	return &Service{pool: pool, querier: db.New(pool), fetcher: fetcher}
}

// NewServiceWithQuerier 用于测试注入替身。
func NewServiceWithQuerier(q Querier, fetcher MetadataFetcher) *Service {
	return &Service{querier: q, fetcher: fetcher}
}

// Resolve 识别用户输入，抓取元数据并入库（幂等：已存在则直接返回）。
func (s *Service) Resolve(ctx context.Context, rawInput string, createdBy uuid.UUID) (ResolveResult, error) {
	id, ok := Identify(rawInput)
	if !ok {
		return ResolveResult{}, fmt.Errorf("%w: 无法识别 DOI 或 arXiv 编号", ErrInvalidInput)
	}

	// 先查库，避免不必要的网络请求。
	if existing, found, err := s.findExisting(ctx, id); err != nil {
		return ResolveResult{}, err
	} else if found {
		return ResolveResult{Paper: toPaper(existing), Existing: true}, nil
	}

	meta, err := s.fetcher.Fetch(ctx, id)
	if err != nil {
		return ResolveResult{}, err
	}

	created, err := s.insert(ctx, meta, createdBy)
	if err != nil {
		// 并发创建时可能撞唯一索引：重新查一次，返回已有记录。
		if isUniqueViolation(err) {
			if existing, found, findErr := s.findExisting(ctx, id); findErr == nil && found {
				return ResolveResult{Paper: toPaper(existing), Existing: true}, nil
			}
		}
		return ResolveResult{}, err
	}
	return ResolveResult{Paper: toPaper(created), Existing: false}, nil
}

// GetByID 按 ID 查询论文。
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (Paper, error) {
	record, err := s.querier.GetPaperByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Paper{}, ErrNotFound
		}
		return Paper{}, err
	}
	return toPaper(record), nil
}

// List 分页列出论文。
func (s *Service) List(ctx context.Context, limit, offset int32) ([]Paper, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	records, err := s.querier.ListPapers(ctx, db.ListPapersParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.querier.CountPapers(ctx)
	if err != nil {
		return nil, 0, err
	}
	papers := make([]Paper, 0, len(records))
	for _, r := range records {
		papers = append(papers, toPaper(r))
	}
	return papers, total, nil
}

// findExisting 按 DOI → arXiv → 去重哈希 的顺序查找已有论文。
func (s *Service) findExisting(ctx context.Context, id Identifier) (db.Paper, bool, error) {
	switch id.Kind {
	case SourceDOI:
		record, err := s.querier.GetPaperByDOI(ctx, &id.Value)
		if err == nil {
			return record, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return db.Paper{}, false, err
		}
	case SourceArxiv:
		record, err := s.querier.GetPaperByArxivID(ctx, &id.Value)
		if err == nil {
			return record, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return db.Paper{}, false, err
		}
	}
	return db.Paper{}, false, nil
}

func (s *Service) insert(ctx context.Context, meta Metadata, createdBy uuid.UUID) (db.Paper, error) {
	if strings.TrimSpace(meta.Title) == "" {
		return db.Paper{}, fmt.Errorf("%w: 抓取到的元数据缺少标题", ErrInvalidInput)
	}

	authorsJSON, err := json.Marshal(meta.Authors)
	if err != nil {
		return db.Paper{}, fmt.Errorf("序列化作者失败: %w", err)
	}

	params := db.CreatePaperParams{
		Doi:           nullableString(meta.DOI),
		ArxivID:       nullableString(meta.ArxivID),
		OpenreviewID:  nullableString(meta.OpenreviewID),
		Title:         meta.Title,
		TitleNorm:     normalizeTitle(meta.Title),
		Authors:       authorsJSON,
		Abstract:      nullableString(meta.Abstract),
		Venue:         nullableString(meta.Venue),
		PublishedAt:   dateFromYear(meta.PublishedYear),
		CitationCount: int32(meta.CitationCount),
		MetaSource:    nullableString(string(meta.Source)),
		MetaFetchedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		DedupeHash:    nullableString(dedupeHash(meta)),
		CreatedBy:     pgtype.UUID{Bytes: createdBy, Valid: createdBy != uuid.Nil},
	}
	return s.querier.CreatePaper(ctx, params)
}

/* -------------------------------------------------------------------------- */
/* 映射与工具                                                                  */
/* -------------------------------------------------------------------------- */

func toPaper(r db.Paper) Paper {
	var authors []Author
	if len(r.Authors) > 0 {
		_ = json.Unmarshal(r.Authors, &authors)
	}
	if authors == nil {
		authors = []Author{}
	}
	return Paper{
		ID:            uuid.UUID(r.ID.Bytes),
		DOI:           deref(r.Doi),
		ArxivID:       deref(r.ArxivID),
		OpenreviewID:  deref(r.OpenreviewID),
		Title:         r.Title,
		Authors:       authors,
		Abstract:      deref(r.Abstract),
		Venue:         deref(r.Venue),
		PublishedYear: yearOf(r.PublishedAt),
		CitationCount: int(r.CitationCount),
		MetaSource:    deref(r.MetaSource),
		CreatedAt:     r.CreatedAt.Time,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullableString(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func yearOf(d pgtype.Date) int {
	if !d.Valid {
		return 0
	}
	return d.Time.Year()
}

func dateFromYear(year int) pgtype.Date {
	if year <= 0 || year > 9999 {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
}

// normalizeTitle 归一化标题用于软去重：小写、去标点、压缩空白。
func normalizeTitle(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 0x4e00 && r <= 0x9fff: // 中文
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// dedupeHash 在无 DOI 时提供软去重键（title_norm + 首位作者 + 年份 的 sha256）。
func dedupeHash(meta Metadata) string {
	if meta.DOI != "" || meta.ArxivID != "" {
		return "" // 有强标识时不需要软去重
	}
	firstAuthor := ""
	if len(meta.Authors) > 0 {
		firstAuthor = strings.ToLower(strings.TrimSpace(meta.Authors[0].Name))
	}
	key := fmt.Sprintf("%s|%s|%d", normalizeTitle(meta.Title), firstAuthor, meta.PublishedYear)
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
