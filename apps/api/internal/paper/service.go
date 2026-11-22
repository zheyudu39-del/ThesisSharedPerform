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
	"unicode/utf8"

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
	// PDFFileID 是挂在这篇论文上的全文文件（papers.pdf_file_id）。
	// 为空表示还没有上传全文，前端降级为纯文本预览。
	PDFFileID *uuid.UUID `json:"pdfFileId"`
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
	SetPaperPDFFile(context.Context, db.SetPaperPDFFileParams) (db.Paper, error)
	GetFileByID(context.Context, pgtype.UUID) (db.File, error)
	GetUserByID(context.Context, pgtype.UUID) (db.User, error)
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

// CreateManualInput 是手动补录论文的入参（PRD P-04）。
//
// 适用场景：没有 DOI / arXiv 编号的论文——专著、技术报告、内部文档、
// 尚未上线的预印本。PRD 要求「必填标题 + 至少一位作者」。
type CreateManualInput struct {
	Title         string
	Authors       []Author
	Abstract      string
	Venue         string
	PublishedYear int
	DOI           string
	ArxivID       string
}

// CreateManual 手动创建一篇论文。
//
// 与 Resolve 的区别：不访问任何外部源，元数据完全来自用户。
// meta_source 记为 "manual"，便于运营区分「抓来的」和「人填的」。
//
// 去重：填了 DOI / arXiv 时先按强标识查重；都没填则按
// (标题归一化, 第一作者, 年份) 算软去重哈希，命中已有记录直接返回，
// 避免同一篇论文被反复补录成多条。
func (s *Service) CreateManual(ctx context.Context, createdBy uuid.UUID, input CreateManualInput) (ResolveResult, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return ResolveResult{}, fmt.Errorf("%w: 标题不能为空", ErrInvalidInput)
	}
	if utf8.RuneCountInString(title) > 512 {
		return ResolveResult{}, fmt.Errorf("%w: 标题不能超过 512 个字符", ErrInvalidInput)
	}

	// 作者：去掉空白项，至少留一位。
	authors := make([]Author, 0, len(input.Authors))
	for _, a := range input.Authors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		authors = append(authors, Author{
			Name:        name,
			Affiliation: strings.TrimSpace(a.Affiliation),
			ORCID:       strings.TrimSpace(a.ORCID),
		})
	}
	if len(authors) == 0 {
		return ResolveResult{}, fmt.Errorf("%w: 至少需要一位作者", ErrInvalidInput)
	}
	if len(authors) > 50 {
		return ResolveResult{}, fmt.Errorf("%w: 作者不能超过 50 位", ErrInvalidInput)
	}

	meta := Metadata{
		Title:         title,
		Authors:       authors,
		Abstract:      strings.TrimSpace(input.Abstract),
		Venue:         strings.TrimSpace(input.Venue),
		PublishedYear: input.PublishedYear,
		DOI:           normalizeDOI(strings.TrimSpace(input.DOI)),
		ArxivID:       normalizeArxiv(strings.TrimSpace(input.ArxivID)),
		Source:        SourceManual,
	}

	// 有强标识时先按它查重，命中直接返回。
	if meta.DOI != "" || meta.ArxivID != "" {
		if existing, found, err := s.findExisting(ctx, Identifier{
			Kind:  pickKind(meta.DOI, meta.ArxivID),
			Value: pickValue(meta.DOI, meta.ArxivID),
		}); err != nil {
			return ResolveResult{}, err
		} else if found {
			return ResolveResult{Paper: toPaper(existing), Existing: true}, nil
		}
	} else {
		// 无强标识：按软去重哈希查重。
		hash := dedupeHash(meta)
		if existing, err := s.querier.GetPaperByDedupeHash(ctx, &hash); err == nil {
			return ResolveResult{Paper: toPaper(existing), Existing: true}, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ResolveResult{}, err
		}
	}

	created, err := s.insert(ctx, meta, createdBy)
	if err != nil {
		if isUniqueViolation(err) {
			// 并发补录同一篇：重新查一次，返回已有记录。
			if meta.DOI != "" || meta.ArxivID != "" {
				if existing, found, findErr := s.findExisting(ctx, Identifier{
					Kind:  pickKind(meta.DOI, meta.ArxivID),
					Value: pickValue(meta.DOI, meta.ArxivID),
				}); findErr == nil && found {
					return ResolveResult{Paper: toPaper(existing), Existing: true}, nil
				}
			} else if h := dedupeHash(meta); h != "" {
				if existing, findErr := s.querier.GetPaperByDedupeHash(ctx, &h); findErr == nil {
					return ResolveResult{Paper: toPaper(existing), Existing: true}, nil
				}
			}
		}
		return ResolveResult{}, err
	}
	return ResolveResult{Paper: toPaper(created), Existing: false}, nil
}

func pickKind(doi, arxiv string) Source {
	if doi != "" {
		return SourceDOI
	}
	return SourceArxiv
}

func pickValue(doi, arxiv string) string {
	if doi != "" {
		return doi
	}
	return arxiv
}

// SetPDFFile 把一份已上传的文件设为这篇论文的全文（papers.pdf_file_id）。
//
// 权限规则：
//   - 论文的创建者可以挂载；管理员也可以（运营补录全文）。
//   - 文件必须属于调用者本人（管理员除外），否则等于把别人的文件
//     悄悄挪到自己的论文上。
//   - 传空 fileID 表示卸载全文。
//
// 为什么不校验「文件是不是 PDF」：Word / LaTeX 源文件同样可以作为
// 论文全文保存，前端按 kind 降级展示。类型判断交给前端与转码流水线。
func (s *Service) SetPDFFile(ctx context.Context, userID, paperID uuid.UUID, fileID *uuid.UUID) (Paper, error) {
	record, err := s.querier.GetPaperByID(ctx, pgtype.UUID{Bytes: paperID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Paper{}, ErrNotFound
		}
		return Paper{}, err
	}

	// 创建者之外的人需要管理员身份。created_by 为空（系统导入的论文）
	// 时只有管理员能挂全文。
	isCreator := record.CreatedBy.Valid && uuid.UUID(record.CreatedBy.Bytes) == userID
	if !isCreator {
		user, err := s.querier.GetUserByID(ctx, pgtype.UUID{Bytes: userID, Valid: true})
		if err != nil {
			return Paper{}, ErrForbidden
		}
		if user.Role != db.UserRoleAdmin {
			return Paper{}, ErrForbidden
		}
	}

	var target pgtype.UUID
	if fileID != nil {
		if *fileID == uuid.Nil {
			return Paper{}, fmt.Errorf("%w: 文件 ID 不能为 uuid.Nil", ErrInvalidInput)
		}
		file, err := s.querier.GetFileByID(ctx, pgtype.UUID{Bytes: *fileID, Valid: true})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Paper{}, fmt.Errorf("%w: 文件不存在", ErrInvalidInput)
			}
			return Paper{}, err
		}
		// 只有管理员能挂别人上传的文件。
		if uuid.UUID(file.OwnerID.Bytes) != userID {
			user, err := s.querier.GetUserByID(ctx, pgtype.UUID{Bytes: userID, Valid: true})
			if err != nil || user.Role != db.UserRoleAdmin {
				return Paper{}, ErrForbidden
			}
		}
		// 被拒绝或转码失败的文件不挂：挂上去也打不开，等于给用户一个坏链接。
		if file.Status == db.FileStatusRejected || file.Status == db.FileStatusFailed {
			return Paper{}, fmt.Errorf("%w: 该文件未通过校验或处理失败", ErrInvalidInput)
		}
		target = pgtype.UUID{Bytes: *fileID, Valid: true}
	}

	updated, err := s.querier.SetPaperPDFFile(ctx, db.SetPaperPDFFileParams{
		PdfFileID: target,
		ID:        pgtype.UUID{Bytes: paperID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Paper{}, ErrNotFound
		}
		return Paper{}, err
	}
	return toPaper(updated), nil
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
		PDFFileID:     uuidPtr(r.PdfFileID),
	}
}

// uuidPtr 把可空的 uuid 列转成指针；NULL 转成 nil。
func uuidPtr(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := uuid.UUID(v.Bytes)
	return &id
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
