package group

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

// pgGroupFilesQuerier 把 sqlc 生成的查询适配成 GroupFilesQuerier。
//
// 适配层单独存在而不是让 FileService 直接依赖 db.Queries：
// 领域包只认识自己的类型，sqlc 生成物的形状变化不会渗进业务逻辑。
type pgGroupFilesQuerier struct {
	q *db.Queries
}

// NewPGGroupFilesQuerier 构造群文件库的查询适配器。
func NewPGGroupFilesQuerier(pool *pgxpool.Pool) GroupFilesQuerier {
	return &pgGroupFilesQuerier{q: db.New(pool)}
}

func (p *pgGroupFilesQuerier) GetGroupFileContext(ctx context.Context, slug string, userID uuid.UUID) (GroupFileContext, error) {
	row, err := p.q.GetGroupFileContext(ctx, db.GetGroupFileContextParams{
		UserID: uuidParam(userID),
		Slug:   &slug,
	})
	if err != nil {
		return GroupFileContext{}, err
	}
	return GroupFileContext{
		GroupID: fromPG(row.GroupID),
		OwnerID: fromPG(row.OwnerID),
		MyRole:  row.MyRole,
	}, nil
}

func (p *pgGroupFilesQuerier) GetFileForGroupFile(ctx context.Context, groupID, fileID uuid.UUID) (SourceFile, error) {
	row, err := p.q.GetFileForGroupFile(ctx, db.GetFileForGroupFileParams{
		GroupID: uuidParam(groupID),
		FileID:  uuidParam(fileID),
	})
	if err != nil {
		return SourceFile{}, err
	}
	return SourceFile{
		ID:           fromPG(row.ID),
		OwnerID:      fromPG(row.OwnerID),
		OriginalName: row.OriginalName,
		Mime:         row.Mime,
		Kind:         row.Kind,
		SizeBytes:    row.SizeBytes,
		PageCount:    row.PageCount,
		PostID:       ptrFromPG(row.PostID),
	}, nil
}

func (p *pgGroupFilesQuerier) InsertGroupFile(
	ctx context.Context,
	groupID, addedBy uuid.UUID,
	input SaveGroupFileInput,
	postID *uuid.UUID,
) (uuid.UUID, error) {
	description := input.Description
	row, err := p.q.InsertGroupFile(ctx, db.InsertGroupFileParams{
		GroupID:     uuidParam(groupID),
		FileID:      uuidParam(input.FileID),
		PostID:      optionalUUID(postID),
		Title:       input.Title,
		Description: optionalString(description),
		Tags:        input.Tags,
		AddedBy:     uuidParam(addedBy),
	})
	if err != nil {
		// ON CONFLICT DO NOTHING 时 sqlc 的 :one 返回 pgx.ErrNoRows。
		// 对业务层来说这就是「已存在」，转换成领域错误而不是泄露 SQL 细节。
		if isNoRows(err) {
			return uuid.Nil, ErrGroupFileExists
		}
		return uuid.Nil, err
	}
	return fromPG(row), nil
}

func (p *pgGroupFilesQuerier) GetGroupFile(ctx context.Context, groupID, id uuid.UUID) (GroupFile, error) {
	row, err := p.q.GetGroupFile(ctx, db.GetGroupFileParams{
		GroupID: uuidParam(groupID),
		ID:      uuidParam(id),
	})
	if err != nil {
		return GroupFile{}, err
	}
	return groupFileFromGetRow(row), nil
}

func (p *pgGroupFilesQuerier) ListGroupFiles(ctx context.Context, params ListGroupFilesParams) ([]GroupFile, error) {
	var tag *string
	if params.Tag != "" {
		tag = &params.Tag
	}
	rows, err := p.q.ListGroupFiles(ctx, db.ListGroupFilesParams{
		GroupID:         uuidParam(params.GroupID),
		Tag:             tag,
		BeforeCreatedAt: optionalTime(params.BeforeCreated),
		BeforeID:        uuidParam(params.BeforeID),
		Limit:           params.Limit,
	})
	if err != nil {
		return nil, err
	}
	files := make([]GroupFile, 0, len(rows))
	for _, row := range rows {
		files = append(files, GroupFile{
			ID:      fromPG(row.ID),
			GroupID: fromPG(row.GroupID),
			FileID:  fromPG(row.FileID),
			PostID:  ptrFromPG(row.PostID),
			Title:   row.Title, Description: row.Description, Tags: row.Tags,
			OriginalName: row.OriginalName, Mime: row.Mime, Kind: row.Kind,
			SizeBytes: row.SizeBytes, PageCount: row.PageCount,
			AddedBy: GroupFileAuthor{
				ID:     fromPG(row.AddedBy),
				Name:   row.AddedByName,
				Handle: row.AddedByHandle,
			},
			CreatedAt: row.CreatedAt.Time,
		})
	}
	return files, nil
}

func (p *pgGroupFilesQuerier) DeleteGroupFile(ctx context.Context, groupID, id uuid.UUID) (int64, error) {
	return p.q.DeleteGroupFile(ctx, db.DeleteGroupFileParams{
		GroupID: uuidParam(groupID),
		ID:      uuidParam(id),
	})
}

// groupFileFromGetRow 把「单条查询行」转成 DTO。
func groupFileFromGetRow(row db.GetGroupFileRow) GroupFile {
	return GroupFile{
		ID:      fromPG(row.ID),
		GroupID: fromPG(row.GroupID),
		FileID:  fromPG(row.FileID),
		PostID:  ptrFromPG(row.PostID),
		Title:   row.Title, Description: row.Description, Tags: row.Tags,
		OriginalName: row.OriginalName, Mime: row.Mime, Kind: row.Kind,
		SizeBytes: row.SizeBytes, PageCount: row.PageCount,
		AddedBy: GroupFileAuthor{
			ID:     fromPG(row.AddedBy),
			Name:   row.AddedByName,
			Handle: row.AddedByHandle,
		},
		CreatedAt: row.CreatedAt.Time,
	}
}

func optionalTime(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func ptrFromPG(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	value := uuid.UUID(id.Bytes)
	return &value
}

// ---------------------------------------------------------------------------
// 本文件私有的转换辅助
//
// group 包已有的 uuidParam 在 service.go 里；下面几个是文件库适配器
// 独有的（pgtype → 原生、指针 ↔ 可空列），放在这里避免污染 service.go。
// ---------------------------------------------------------------------------

func fromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return uuidParam(*id)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// isNoRows 判断错误是否为「查不到行」。
// ON CONFLICT DO NOTHING 命中冲突时 sqlc 的 :one 返回 pgx.ErrNoRows，
// 对业务层来说就是「已存在」。
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
