package group

// 群文件库（PRD G-05）：群内共享文献，按标签分类，支持从帖子一键存入。
//
// 为什么单独成一个服务而不是挂在 group.Service 上：
// 文件库是「指向已有文件的引用」，与群组/成员/邀请码的领域规则几乎不重叠，
// 唯一共享的是「群成员才能操作」这条权限。拆开后现有 Service 的 Querier
// 与测试替身都不必改动，接线时只需多构造一个 FileService。
// 权限判定本身仍是纯函数（CanViewGroupFiles / CanDeleteGroupFile），
// 可以直接单元测试。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// 群文件库的领域错误。
var (
	// ErrGroupFileNotFound 表示文件库条目不存在或不属于该群。
	ErrGroupFileNotFound = errors.New("文件库条目不存在")
	// ErrGroupFileExists 表示该文件已存入本群文件库（唯一约束冲突）。
	ErrGroupFileExists = errors.New("该文件已在群文件库中")
	// ErrSourceFileNotFound 表示 fileId 指向的文件不存在。
	ErrSourceFileNotFound = errors.New("文件不存在")
)

const (
	// 与迁移 00005 的列宽一致：提前校验能返回清晰的 422，
	// 而不是把数据库层的截断错误暴露给客户端。
	groupFileTitleMax       = 255
	groupFileDescriptionMax = 500
	// 标签长度与数量上限：防止把整段关键词倒进标签字段。
	groupFileTagMax    = 32
	groupFileTagsLimit = 10
	// streamGroupFiles 是文件库列表的游标标识，防止与其它列表的游标混用。
	streamGroupFiles = "group_files"
)

// GroupFileAuthor 是「存入者」摘要。
type GroupFileAuthor struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Handle string    `json:"handle"`
}

// GroupFile 是群文件库条目的对外 DTO。
//
// 同时携带「文件本体元数据」（originalName/mime/sizeBytes/pageCount）
// 与「文件库条目元数据」（title/description/tags/存入者），
// 前端列表不需要再逐条请求文件详情。
type GroupFile struct {
	ID           uuid.UUID       `json:"id"`
	GroupID      uuid.UUID       `json:"groupId"`
	FileID       uuid.UUID       `json:"fileId"`
	PostID       *uuid.UUID      `json:"postId"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Tags         []string        `json:"tags"`
	OriginalName string          `json:"originalName"`
	Mime         string          `json:"mime"`
	Kind         string          `json:"kind"`
	SizeBytes    int64           `json:"sizeBytes"`
	PageCount    int32           `json:"pageCount"`
	AddedBy      GroupFileAuthor `json:"addedBy"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// GroupFilesResult 是文件库列表的一页。
type GroupFilesResult struct {
	Files      []GroupFile `json:"files"`
	NextCursor *string     `json:"nextCursor"`
}

// SaveGroupFileInput 是存入文件库的入参。
// Title 为空时由服务端回退到文件名。
type SaveGroupFileInput struct {
	FileID      uuid.UUID
	Title       string
	Description string
	Tags        []string
}

// GroupFileContext 是权限判定所需的群组事实。
// MyRole 为空串表示非群成员；OwnerID 用于判定群主。
type GroupFileContext struct {
	GroupID uuid.UUID
	OwnerID uuid.UUID
	MyRole  string
}

// SourceFile 是待存入文件的本体信息与来源帖子。
type SourceFile struct {
	ID           uuid.UUID
	OwnerID      uuid.UUID
	OriginalName string
	Mime         string
	Kind         string
	SizeBytes    int64
	PageCount    int32
	// PostID 是本群内引用该文件的最新帖子；没有则为 nil。
	PostID *uuid.UUID
}

// GroupFilesQuerier 是文件库需要的查询子集。
//
// 独立于 group.Querier 声明：Querier 已经很大，把文件库的方法塞进去
// 会让每个现有测试替身都被迫实现一堆无关方法。
type GroupFilesQuerier interface {
	GetGroupFileContext(ctx context.Context, slug string, userID uuid.UUID) (GroupFileContext, error)
	GetFileForGroupFile(ctx context.Context, groupID, fileID uuid.UUID) (SourceFile, error)
	InsertGroupFile(ctx context.Context, groupID, addedBy uuid.UUID, input SaveGroupFileInput, postID *uuid.UUID) (uuid.UUID, error)
	GetGroupFile(ctx context.Context, groupID, id uuid.UUID) (GroupFile, error)
	ListGroupFiles(ctx context.Context, params ListGroupFilesParams) ([]GroupFile, error)
	DeleteGroupFile(ctx context.Context, groupID, id uuid.UUID) (int64, error)
}

// ListGroupFilesParams 是列表查询参数。
type ListGroupFilesParams struct {
	GroupID       uuid.UUID
	Tag           string
	BeforeCreated *time.Time
	BeforeID      uuid.UUID
	Limit         int32
}

// FileService 实现群文件库的领域逻辑。
type FileService struct {
	queries GroupFilesQuerier
}

// NewFileService 用查询实现构造服务。
//
// 接线代理需要提供一个适配器，把 sqlc 生成的 db.Queries 转成
// GroupFilesQuerier（生成类型的字段名见 db/queries/group_files.sql）。
// 不直接接受 *pgxpool.Pool，是为了在 sqlc 生成之前本包也能编译与测试。
func NewFileService(queries GroupFilesQuerier) *FileService {
	return &FileService{queries: queries}
}

// ---------------------------------------------------------------------------
// 权限（纯函数）
// ---------------------------------------------------------------------------

// CanViewGroupFiles 判断能否查看文件库：仅群成员。
// 文件库可能包含未公开的文献，与群内动态保持同样的可见范围。
func CanViewGroupFiles(role Role) bool { return AtLeast(role, RoleMember) }

// CanSaveGroupFile 判断能否向文件库存入文件：仅群成员。
func CanSaveGroupFile(role Role) bool { return AtLeast(role, RoleMember) }

// CanDeleteGroupFile 判断能否删除文件库条目。
//
// 规则（任务契约）：存入者本人，或群 admin/owner。
// 存入者本人可以删除自己加错的条目，管理员负责清理他人的条目；
// 普通成员不能删别人的条目。
func CanDeleteGroupFile(role Role, isAdder bool) bool {
	if isAdder {
		return true
	}
	return CanManageGroup(role)
}

// ---------------------------------------------------------------------------
// 服务方法
// ---------------------------------------------------------------------------

// Save 把一个已有文件存入群文件库。
//
// 文件来源有两种，权限判定不同：
//   - 上传者本人：文件是他的，存进自己加入的群都合理；
//   - 帖子附件：文件已随帖子发布到本群，群成员看到后存入文件库，
//     正是产品描述里的「一键存入」。
//
// 其余情况（别人上传、又没在本群帖子里出现过）拒绝：文件库不该成为
// 绕过文件可见性去扩散他人私有文件的通道。
func (s *FileService) Save(ctx context.Context, userID uuid.UUID, slug string, input SaveGroupFileInput) (GroupFile, error) {
	if input.FileID == uuid.Nil {
		return GroupFile{}, fmt.Errorf("%w: fileId 不能为空", ErrValidation)
	}
	group, err := s.queries.GetGroupFileContext(ctx, slug, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GroupFile{}, ErrGroupNotFound
	}
	if err != nil {
		return GroupFile{}, fmt.Errorf("查询群组失败: %w", err)
	}
	if !CanSaveGroupFile(Role(group.MyRole)) {
		return GroupFile{}, ErrForbidden
	}

	source, err := s.queries.GetFileForGroupFile(ctx, group.GroupID, input.FileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GroupFile{}, ErrSourceFileNotFound
	}
	if err != nil {
		return GroupFile{}, fmt.Errorf("查询文件失败: %w", err)
	}
	if source.OwnerID != userID && source.PostID == nil {
		return GroupFile{}, ErrForbidden
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		// 不填标题时用原始文件名：文件库列表里至少能认出是哪份文件。
		title = source.OriginalName
	}
	if n := utf8.RuneCountInString(title); n < 1 || n > groupFileTitleMax {
		return GroupFile{}, fmt.Errorf("%w: 标题长度须为 1 到 %d 个字符", ErrValidation, groupFileTitleMax)
	}
	description := strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(description) > groupFileDescriptionMax {
		return GroupFile{}, fmt.Errorf("%w: 描述不能超过 %d 个字符", ErrValidation, groupFileDescriptionMax)
	}
	tags, err := normalizeTags(input.Tags)
	if err != nil {
		return GroupFile{}, err
	}

	id, err := s.queries.InsertGroupFile(ctx, group.GroupID, userID, SaveGroupFileInput{
		FileID: input.FileID, Title: title, Description: description, Tags: tags,
	}, source.PostID)
	if err != nil {
		// 重复存入有三种表现，都要归一到 ErrGroupFileExists：
		//   1. 适配器把唯一约束冲突原样抛出（SQLSTATE 23505）；
		//   2. sqlc 的 :one + ON CONFLICT DO NOTHING 在无行返回时给 pgx.ErrNoRows；
		//   3. 适配器自行把 0 行转成 uuid.Nil。
		if isUniqueViolation(err) || errors.Is(err, pgx.ErrNoRows) {
			return GroupFile{}, ErrGroupFileExists
		}
		return GroupFile{}, fmt.Errorf("存入文件库失败: %w", err)
	}
	if id == uuid.Nil {
		return GroupFile{}, ErrGroupFileExists
	}
	return s.queries.GetGroupFile(ctx, group.GroupID, id)
}

// List 列出文件库，仅群成员可读。
func (s *FileService) List(ctx context.Context, userID uuid.UUID, slug, tag, rawCursor string, limit int32) (GroupFilesResult, error) {
	group, err := s.queries.GetGroupFileContext(ctx, slug, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GroupFilesResult{}, ErrGroupNotFound
	}
	if err != nil {
		return GroupFilesResult{}, fmt.Errorf("查询群组失败: %w", err)
	}
	if !CanViewGroupFiles(Role(group.MyRole)) {
		return GroupFilesResult{}, ErrForbidden
	}
	limit = normalizeLimit(limit)
	createdAt, id, err := decodeCursor(rawCursor, streamGroupFiles)
	if err != nil {
		return GroupFilesResult{}, err
	}
	params := ListGroupFilesParams{
		GroupID: group.GroupID,
		Tag:     strings.TrimSpace(tag),
		Limit:   limit + 1,
	}
	if rawCursor != "" {
		params.BeforeCreated = &createdAt
		params.BeforeID = id
	}
	files, err := s.queries.ListGroupFiles(ctx, params)
	if err != nil {
		return GroupFilesResult{}, fmt.Errorf("查询文件库失败: %w", err)
	}
	hasMore := len(files) > int(limit)
	if hasMore {
		files = files[:limit]
	}
	if files == nil {
		files = []GroupFile{}
	}
	var next *string
	if hasMore && len(files) > 0 {
		last := files[len(files)-1]
		encoded, err := encodeCursor(streamGroupFiles, last.CreatedAt, last.ID)
		if err != nil {
			return GroupFilesResult{}, err
		}
		next = &encoded
	}
	return GroupFilesResult{Files: files, NextCursor: next}, nil
}

// Delete 删除文件库条目：存入者本人或 admin/owner。
func (s *FileService) Delete(ctx context.Context, userID uuid.UUID, slug string, entryID uuid.UUID) error {
	group, err := s.queries.GetGroupFileContext(ctx, slug, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGroupNotFound
	}
	if err != nil {
		return fmt.Errorf("查询群组失败: %w", err)
	}
	if !AtLeast(Role(group.MyRole), RoleMember) {
		// 非成员一律拒绝，避免探测群内文件库。
		return ErrForbidden
	}
	entry, err := s.queries.GetGroupFile(ctx, group.GroupID, entryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGroupFileNotFound
	}
	if err != nil {
		return fmt.Errorf("查询文件库条目失败: %w", err)
	}
	if !CanDeleteGroupFile(Role(group.MyRole), entry.AddedBy.ID == userID) {
		return ErrForbidden
	}
	affected, err := s.queries.DeleteGroupFile(ctx, group.GroupID, entryID)
	if err != nil {
		return fmt.Errorf("删除文件库条目失败: %w", err)
	}
	if affected == 0 {
		return ErrGroupFileNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// normalizeTags 清洗标签：去空白、去重、限制长度与数量。
// 去重是必要的：标签用于过滤，重复标签会让前端展示出现重复项，
// 也会让「按标签计数」这类后续功能失真。
func normalizeTags(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(raw))
	tags := make([]string, 0, len(raw))
	for _, tag := range raw {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > groupFileTagMax {
			return nil, fmt.Errorf("%w: 单个标签不能超过 %d 个字符", ErrValidation, groupFileTagMax)
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) > groupFileTagsLimit {
		return nil, fmt.Errorf("%w: 标签最多 %d 个", ErrValidation, groupFileTagsLimit)
	}
	return tags, nil
}

// isUniqueViolation 判断是否唯一约束冲突。
// 用 SQLSTATE 而不是字符串匹配：错误文案会随 PostgreSQL 版本变化。
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
