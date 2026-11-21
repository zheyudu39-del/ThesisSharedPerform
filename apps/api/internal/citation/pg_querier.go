package citation

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

// pgQuerier 把 sqlc 生成的查询适配成 citation.Querier。
//
// 适配层单独存在而不是让 Service 直接依赖 db.Queries：
// 领域包只认识自己的类型，sqlc 生成物的形状变化不会渗进业务逻辑。
// 这也是 citation 包能在 sqlc 生成之前就完成单元测试的原因。
type pgQuerier struct {
	q *db.Queries
}

// NewPGQuerier 构造适配器。
func NewPGQuerier(pool *pgxpool.Pool) Querier {
	return &pgQuerier{q: db.New(pool)}
}

func (p *pgQuerier) GetDocumentAccessForReference(ctx context.Context, docID, userID uuid.UUID) (AccessRow, error) {
	row, err := p.q.GetDocumentAccessForReference(ctx, db.GetDocumentAccessForReferenceParams{
		DocID:  uuidParam(docID),
		UserID: uuidParam(userID),
	})
	if err != nil {
		return AccessRow{}, err
	}
	return AccessRow{
		DocID:        fromPG(row.DocID),
		GroupID:      fromPG(row.GroupID),
		CreatedBy:    fromPG(row.CreatedBy),
		GroupOwnerID: fromPG(row.GroupOwnerID),
		GroupRole:    anyToString(row.GroupRole),
		DocRole:      anyToString(row.DocRole),
		DefaultRole:  row.DefaultRole,
	}, nil
}

func (p *pgQuerier) UpsertDocReference(ctx context.Context, params UpsertReferenceParams) (Entry, error) {
	authors, err := json.Marshal(params.Authors)
	if err != nil {
		return Entry{}, err
	}
	row, err := p.q.UpsertDocReference(ctx, db.UpsertDocReferenceParams{
		DocID:     uuidParam(params.DocID),
		CiteKey:   params.CiteKey,
		EntryType: params.EntryType,
		Title:     params.Title,
		Authors:   authors,
		Year:      params.Year,
		Container: params.Container,
		Volume:    params.Volume,
		Issue:     params.Issue,
		Pages:     params.Pages,
		Doi:       params.DOI,
		Url:       params.URL,
		Publisher: params.Publisher,
		RawBibtex: params.RawBibTeX,
		CreatedBy: optionalUUID(params.CreatedBy),
	})
	if err != nil {
		return Entry{}, err
	}
	return entryFromUpsertRow(row)
}

func (p *pgQuerier) ListDocReferences(ctx context.Context, docID uuid.UUID) ([]Entry, error) {
	rows, err := p.q.ListDocReferences(ctx, uuidParam(docID))
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		entry, err := entryFromListRow(row)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (p *pgQuerier) DeleteDocReference(ctx context.Context, docID, refID uuid.UUID) (int64, error) {
	return p.q.DeleteDocReference(ctx, db.DeleteDocReferenceParams{
		DocID: uuidParam(docID),
		ID:    uuidParam(refID),
	})
}

// entryFromUpsertRow 与 entryFromListRow 共用字段解码。
// authors 是 jsonb，生成类型给的是 []byte，需要显式反序列化。
func entryFromUpsertRow(row db.UpsertDocReferenceRow) (Entry, error) {
	return buildEntry(row.ID, row.DocID, row.CiteKey, row.EntryType, row.Title, row.Authors,
		row.Year, row.Container, row.Volume, row.Issue, row.Pages, row.Doi, row.Url,
		row.Publisher, row.CreatedAt)
}

func entryFromListRow(row db.ListDocReferencesRow) (Entry, error) {
	return buildEntry(row.ID, row.DocID, row.CiteKey, row.EntryType, row.Title, row.Authors,
		row.Year, row.Container, row.Volume, row.Issue, row.Pages, row.Doi, row.Url,
		row.Publisher, row.CreatedAt)
}

func buildEntry(
	id, docID pgtype.UUID,
	citeKey, entryType, title string,
	authors []byte,
	year int32,
	container, volume, issue, pages, doi, url, publisher string,
	createdAt pgtype.Timestamptz,
) (Entry, error) {
	var parsed []Author
	if len(authors) > 0 {
		if err := json.Unmarshal(authors, &parsed); err != nil {
			return Entry{}, err
		}
	}
	return Entry{
		ID:        fromPG(id),
		DocID:     fromPG(docID),
		CiteKey:   citeKey,
		EntryType: entryType,
		Title:     title,
		Authors:   parsed,
		Year:      year,
		Container: container,
		Volume:    volume,
		Issue:     issue,
		Pages:     pages,
		DOI:       doi,
		URL:       url,
		Publisher: publisher,
		CreatedAt: createdAt.Time,
	}, nil
}

func uuidParam(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return uuidParam(*id)
}

func fromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

// errNoRows 暴露给调用方判断「文档不存在」，避免让业务层 import pgx。
var errNoRows = pgx.ErrNoRows

// IsNotFound 判断错误是否为「查不到行」。
func IsNotFound(err error) bool { return errors.Is(err, errNoRows) }

// anyToString 把 sqlc 对「COALESCE(enum::text, '')」推断出的 interface{}
// 安全地转成字符串。空值按空串处理——业务层用空串表示「没有这个角色」。
func anyToString(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
