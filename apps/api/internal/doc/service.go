package doc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
	"github.com/thesis-shared/api/internal/storage"
)

// UserRef 是 DTO 里出现的最小用户摘要。
type UserRef struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Handle string    `json:"handle"`
}

// Document 是文档对外 DTO，字段与两端契约严格一致。
type Document struct {
	ID             uuid.UUID `json:"id"`
	GroupID        uuid.UUID `json:"groupId"`
	GroupSlug      string    `json:"groupSlug"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	DefaultRole    string    `json:"defaultRole"`
	WordCount      int32     `json:"wordCount"`
	LatestSnapshot int32     `json:"latestSnapshot"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	CreatedBy      UserRef   `json:"createdBy"`
}

// Member 是文档级显式成员的 DTO。
// 只表示 doc_members 中的覆盖角色；群成员的基础权限来自 defaultRole。
type Member struct {
	UserID     uuid.UUID `json:"userId"`
	Name       string    `json:"name"`
	Handle     string    `json:"handle"`
	Role       string    `json:"role"`
	VerifyType string    `json:"verifyType"`
	GrantedAt  time.Time `json:"grantedAt"`
}

// Snapshot 是快照元数据 DTO。正文存放在对象存储，REST 只返回元数据。
type Snapshot struct {
	Version   int32     `json:"version"`
	Label     string    `json:"label"`
	CreatedBy *UserRef  `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	SizeBytes int64     `json:"sizeBytes"`
}

// DocumentsResult 是文档列表的一页。
type DocumentsResult struct {
	Documents  []Document `json:"documents"`
	NextCursor *string    `json:"nextCursor"`
}

// SnapshotsResult 是快照列表的一页。
type SnapshotsResult struct {
	Snapshots  []Snapshot `json:"snapshots"`
	NextCursor *string    `json:"nextCursor"`
}

// MembersResult 是文档成员列表。
type MembersResult struct {
	Members []Member `json:"members"`
}

// UpdateDocumentInput 是局部更新入参：nil 表示请求体未提供该字段。
type UpdateDocumentInput struct {
	Title  *string
	Status *string
}

// Querier 限定文档服务使用的生成查询，便于单元测试注入替身。
type Querier interface {
	CreateDocument(context.Context, db.CreateDocumentParams) (db.Document, error)
	GetDocumentByID(context.Context, pgtype.UUID) (db.GetDocumentByIDRow, error)
	UpdateDocument(context.Context, db.UpdateDocumentParams) (db.Document, error)
	SoftDeleteDocument(context.Context, pgtype.UUID) (int64, error)
	ListDocumentsByGroup(context.Context, db.ListDocumentsByGroupParams) ([]db.ListDocumentsByGroupRow, error)
	GetDocumentAccess(context.Context, db.GetDocumentAccessParams) (db.GetDocumentAccessRow, error)
	GetGroupForDocumentCreate(context.Context, db.GetGroupForDocumentCreateParams) (db.GetGroupForDocumentCreateRow, error)

	ListDocMembers(context.Context, pgtype.UUID) ([]db.ListDocMembersRow, error)
	GetDocMember(context.Context, db.GetDocMemberParams) (db.DocMember, error)
	GetDocMemberView(context.Context, db.GetDocMemberViewParams) (db.GetDocMemberViewRow, error)
	GetGroupMemberRole(context.Context, db.GetGroupMemberRoleParams) (db.GroupRole, error)
	UpsertDocMember(context.Context, db.UpsertDocMemberParams) (db.DocMember, error)
	DeleteDocMember(context.Context, db.DeleteDocMemberParams) (int64, error)

	BumpDocumentSnapshot(context.Context, pgtype.UUID) (int32, error)
	DeleteSnapshot(context.Context, db.DeleteSnapshotParams) error
	InsertSnapshot(context.Context, db.InsertSnapshotParams) (db.DocSnapshot, error)
	ListSnapshots(context.Context, db.ListSnapshotsParams) ([]db.ListSnapshotsRow, error)

	CreateAnnotation(context.Context, db.CreateAnnotationParams) (db.Annotation, error)
	GetAnnotation(context.Context, db.GetAnnotationParams) (db.GetAnnotationRow, error)
	ListAnnotations(context.Context, pgtype.UUID) ([]db.ListAnnotationsRow, error)
	UpdateAnnotationContent(context.Context, db.UpdateAnnotationContentParams) (pgtype.UUID, error)
	SetAnnotationResolved(context.Context, db.SetAnnotationResolvedParams) (pgtype.UUID, error)
}

// NotifyInput 描述一条待生成的通知，字段与 notify.CreateInput 一一对应。
// 与 content / group 的同类声明一致：doc 只认识自己的抽象，
// 由 server 侧的适配器桥接到 notify.Service。
type NotifyInput struct {
	UserID     uuid.UUID
	Type       string
	ActorID    uuid.UUID
	TargetType string
	TargetID   uuid.UUID
	Payload    map[string]any
}

// Notifier 是通知生成的抽象，由 server 注入 notify.Service 的适配器。
// 为 nil 时跳过通知（单元测试或未接入通知的部署），业务写入不受影响。
type Notifier interface {
	Notify(ctx context.Context, input NotifyInput) error
}

// editorQuerier 查询文档编辑通知的收件人：创建者 + 有编辑权限的文档级成员。
//
// 单独抽成接口而不是塞进 Querier：Querier 被单测替身完整实现，加方法会让
// 既有替身编译失败；且该查询只服务于通知。
type editorQuerier interface {
	ListDocumentEditorIDs(ctx context.Context, docID pgtype.UUID) ([]pgtype.UUID, error)
}

// dbEditorQuerier 用生成查询取回收件人。
// 创建者不一定在 doc_members 里（默认权限来自群角色），因此 SQL 用 UNION 并入。
type dbEditorQuerier struct{ queries *db.Queries }

func (q dbEditorQuerier) ListDocumentEditorIDs(ctx context.Context, docID pgtype.UUID) ([]pgtype.UUID, error) {
	return q.queries.ListDocumentEditorIDs(ctx, docID)
}

// snapshotQuerier 查询单条快照记录，供回滚与下载使用。
//
// 单独抽成接口而不是塞进 Querier：Querier 已被单测替身完整实现，
// 新增方法会让既有替身编译失败；而这两条查询只服务于回滚链路。
type snapshotQuerier interface {
	GetSnapshotByVersion(ctx context.Context, docID pgtype.UUID, version int32) (db.DocSnapshot, error)
}

// generatedSnapshotQuerier 用 sqlc 生成的 GetSnapshot 读快照行。
type generatedSnapshotQuerier struct{ queries *db.Queries }

func (q generatedSnapshotQuerier) GetSnapshotByVersion(ctx context.Context, docID pgtype.UUID, version int32) (db.DocSnapshot, error) {
	if q.queries == nil {
		return db.DocSnapshot{}, pgx.ErrNoRows
	}
	return q.queries.GetSnapshot(ctx, db.GetSnapshotParams{DocID: docID, Version: version})
}

// Service 实现文档领域逻辑。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	now     func() time.Time
	// notifier 可选：nil 时跳过通知生成。
	notifier Notifier
	// editors 可选：文档编辑通知的收件人查询。为 nil 时跳过该通知。
	editors editorQuerier
	// snapshots 用于回滚时读取指定版本的存储键。
	snapshots snapshotQuerier
	// reloader 通知协同服务卸载内存中的文档；为 nil 表示未配置，
	// 此时回滚会降级为「需要用户手动刷新」。
	reloader CollabReloader
	// store 是对象存储，导入/读取文档图片时使用；为 nil 时跳过图片。
	store *storage.Client
	// assets 查询 doc_assets。与主 Querier 分开，避免测试替身被迫实现。
	assets assetQuerier
}

// NewService 构造服务。pool 为 nil 时只读路径可用（测试替身场景）。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		pool:      pool,
		queries:   db.New(pool),
		now:       time.Now,
		editors:   dbEditorQuerier{queries: db.New(pool)},
		snapshots: generatedSnapshotQuerier{queries: db.New(pool)},
		assets:    db.New(pool),
	}
}

// SetNotifier 注入通知实现；不注入时文档编辑不产生通知。
func (s *Service) SetNotifier(notifier Notifier) { s.notifier = notifier }

// SetCollabReloader 注入协同服务客户端。
// 由 server 包在组装时调用：doc 包不关心对端是 HTTP 还是别的什么。
func (s *Service) SetCollabReloader(reloader CollabReloader) { s.reloader = reloader }

// newServiceWithQuerier 用于测试注入替身。
func newServiceWithQuerier(queries Querier) *Service {
	return &Service{queries: queries, now: time.Now}
}

// SetSnapshotQuerier 注入快照查询实现（测试用；生产环境由 NewService 自动装配）。
func (s *Service) SetSnapshotQuerier(q snapshotQuerier) { s.snapshots = q }

// withTx 在事务中执行 fn；没有连接池（单元测试替身）时直接执行。
func (s *Service) withTx(ctx context.Context, fn func(Querier) error) error {
	if s.pool == nil {
		return fn(s.queries)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(db.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 文档 CRUD
// ---------------------------------------------------------------------------

// CreateDocument 在群组内创建文档。只有群成员可以创建。
// 新文档默认 draft、defaultRole = editor（群成员默认可编辑）。
func (s *Service) CreateDocument(ctx context.Context, userID uuid.UUID, groupSlug, title string) (Document, error) {
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n < 1 || n > 256 {
		return Document{}, fmt.Errorf("%w: 文档标题长度须为 1 到 256 个字符", ErrValidation)
	}
	group, err := s.queries.GetGroupForDocumentCreate(ctx, db.GetGroupForDocumentCreateParams{
		Slug: &groupSlug, UserID: uuidParam(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrGroupNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("查询群组失败: %w", err)
	}
	// 文档必须属于群组：非群成员（包括匿名）不能创建。
	if !group.MyRole.Valid {
		return Document{}, ErrForbidden
	}
	created, err := s.queries.CreateDocument(ctx, db.CreateDocumentParams{
		GroupID:     group.ID,
		Title:       title,
		Status:      db.DocStatusDraft,
		DefaultRole: db.DocRoleEditor,
		CreatedBy:   uuidParam(userID),
	})
	if err != nil {
		return Document{}, fmt.Errorf("创建文档失败: %w", err)
	}
	document, _, err := s.GetDocument(ctx, userID, created.ID.Bytes)
	return document, err
}

// GetDocument 读取文档并返回当前用户的有效角色。
//
// 无权查看与不存在统一返回 ErrNotFound：非群成员无法通过响应差异
// 探测某个文档是否存在。
func (s *Service) GetDocument(ctx context.Context, userID, docID uuid.UUID) (Document, string, error) {
	facts, err := s.loadAccess(ctx, s.queries, userID, docID)
	if err != nil {
		return Document{}, "", err
	}
	role := EffectiveRole(facts)
	if !CanView(role) {
		return Document{}, "", ErrNotFound
	}
	row, err := s.queries.GetDocumentByID(ctx, uuidParam(docID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, "", ErrNotFound
	}
	if err != nil {
		return Document{}, "", fmt.Errorf("查询文档失败: %w", err)
	}
	return documentFromRow(row), string(role), nil
}

// UpdateDocument 局部更新标题/状态，要求 editor 及以上。
func (s *Service) UpdateDocument(ctx context.Context, userID, docID uuid.UUID, input UpdateDocumentInput) (Document, error) {
	params := db.UpdateDocumentParams{ID: uuidParam(docID)}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if n := utf8.RuneCountInString(title); n < 1 || n > 256 {
			return Document{}, fmt.Errorf("%w: 文档标题长度须为 1 到 256 个字符", ErrValidation)
		}
		params.Title = &title
	}
	if input.Status != nil {
		status, ok := parseStatus(*input.Status)
		if !ok {
			return Document{}, fmt.Errorf("%w: status 只能是 draft、active 或 archived", ErrValidation)
		}
		params.Status = db.NullDocStatus{DocStatus: status, Valid: true}
	}

	var creatorID uuid.UUID
	err := s.withTx(ctx, func(q Querier) error {
		facts, err := s.loadAccess(ctx, q, userID, docID)
		if err != nil {
			return err
		}
		if !CanEdit(EffectiveRole(facts)) {
			return ErrForbidden
		}
		if _, err := q.UpdateDocument(ctx, params); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("更新文档失败: %w", err)
		}
		creatorID = facts.CreatedBy
		return nil
	})
	if err != nil {
		return Document{}, err
	}
	// 只有非创建者编辑才提醒：创建者改自己的文档不必被自己打扰。
	// 通知在提交后发送，失败只记日志。
	if creatorID != uuid.Nil && creatorID != userID {
		s.notifyDocEdit(ctx, creatorID, userID, docID)
	}
	updated, _, err := s.GetDocument(ctx, userID, docID)
	return updated, err
}

// notifyDocEdit 给文档创建者与编辑者发「文档被编辑」通知。
//
// 收件人来自 ListDocumentEditorIDs（创建者 + 文档级 editor），排除编辑者本人；
// 同一文档 5 分钟内的多次编辑由 notify 服务层聚合为一条，这里只需逐人发送。
func (s *Service) notifyDocEdit(ctx context.Context, creatorID, actorID, docID uuid.UUID) {
	if s.notifier == nil || s.editors == nil {
		return
	}
	recipients, err := s.editors.ListDocumentEditorIDs(ctx, uuidParam(docID))
	if err != nil {
		slog.Error("查询文档编辑者失败", "docId", docID, "err", err)
		return
	}
	seen := make(map[uuid.UUID]struct{}, len(recipients)+1)
	// 创建者必收，即使查询因并发删除暂时取不到。
	seen[creatorID] = struct{}{}
	for _, id := range recipients {
		if id.Valid {
			seen[uuid.UUID(id.Bytes)] = struct{}{}
		}
	}
	for recipient := range seen {
		if recipient == actorID || recipient == uuid.Nil {
			continue
		}
		if err := s.notifier.Notify(ctx, NotifyInput{
			UserID:     recipient,
			Type:       "doc_edit",
			ActorID:    actorID,
			TargetType: "document",
			TargetID:   docID,
		}); err != nil {
			slog.Error("生成文档编辑通知失败", "docId", docID, "userId", recipient, "err", err)
		}
	}
}

// DeleteDocument 软删除文档，仅 owner 可调用。
func (s *Service) DeleteDocument(ctx context.Context, userID, docID uuid.UUID) error {
	return s.withTx(ctx, func(q Querier) error {
		facts, err := s.loadAccess(ctx, q, userID, docID)
		if err != nil {
			return err
		}
		if !CanDelete(EffectiveRole(facts)) {
			return ErrForbidden
		}
		affected, err := q.SoftDeleteDocument(ctx, uuidParam(docID))
		if err != nil {
			return fmt.Errorf("删除文档失败: %w", err)
		}
		if affected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ListDocuments 列出群内文档，仅群成员可读。
func (s *Service) ListDocuments(ctx context.Context, userID uuid.UUID, groupSlug, rawCursor string, limit int32) (DocumentsResult, error) {
	limit = normalizeLimit(limit)
	group, err := s.queries.GetGroupForDocumentCreate(ctx, db.GetGroupForDocumentCreateParams{
		Slug: &groupSlug, UserID: uuidParam(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentsResult{}, ErrGroupNotFound
	}
	if err != nil {
		return DocumentsResult{}, fmt.Errorf("查询群组失败: %w", err)
	}
	if !group.MyRole.Valid {
		return DocumentsResult{}, ErrForbidden
	}
	updatedAt, id, err := decodeCursor(rawCursor, streamDocuments)
	if err != nil {
		return DocumentsResult{}, err
	}
	rows, err := s.queries.ListDocumentsByGroup(ctx, db.ListDocumentsByGroupParams{
		GroupID:         group.ID,
		BeforeUpdatedAt: timeCursor(updatedAt, rawCursor),
		BeforeID:        uuidParam(id),
		Limit:           limit + 1,
	})
	if err != nil {
		return DocumentsResult{}, fmt.Errorf("查询文档列表失败: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	documents := make([]Document, 0, len(rows))
	for _, row := range rows {
		documents = append(documents, documentFromListRow(row))
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor(streamDocuments, last.UpdatedAt.Time, last.ID.Bytes)
		if err != nil {
			return DocumentsResult{}, err
		}
		next = &encoded
	}
	return DocumentsResult{Documents: documents, NextCursor: next}, nil
}

// ---------------------------------------------------------------------------
// 文档成员（doc_members 覆盖）
// ---------------------------------------------------------------------------

// ListMembers 返回文档级显式成员，要求至少 viewer。
func (s *Service) ListMembers(ctx context.Context, userID, docID uuid.UUID) (MembersResult, error) {
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanView); err != nil {
		return MembersResult{}, err
	}
	rows, err := s.queries.ListDocMembers(ctx, uuidParam(docID))
	if err != nil {
		return MembersResult{}, fmt.Errorf("查询文档成员失败: %w", err)
	}
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, Member{
			UserID: row.UserID.Bytes, Name: row.Name, Handle: row.Handle,
			Role: string(row.Role), VerifyType: string(row.VerifyType), GrantedAt: row.CreatedAt.Time.UTC(),
		})
	}
	return MembersResult{Members: members}, nil
}

// UpdateMemberRole 授予/覆盖某位群成员的文档角色，仅 owner 可调用。
// 目标必须是文档所属群组的成员；创建者与群主的角色不可修改。
func (s *Service) UpdateMemberRole(ctx context.Context, actorID, docID, targetID uuid.UUID, newRole string) (Member, error) {
	role, ok := ParseAssignableRole(newRole)
	if !ok {
		return Member{}, fmt.Errorf("%w: 文档角色只能是 editor 或 viewer", ErrValidation)
	}
	var updated db.GetDocMemberViewRow
	err := s.withTx(ctx, func(q Querier) error {
		facts, err := s.loadAccess(ctx, q, actorID, docID)
		if err != nil {
			return err
		}
		if !CanManageMembers(EffectiveRole(facts)) {
			return ErrForbidden
		}
		if err := ensureNotImmutable(facts, targetID); err != nil {
			return err
		}
		// 目标必须是群成员：doc_members 只在群成员范围内生效，
		// 给群外用户授权既无效也容易让管理员误以为已开放访问。
		if err := s.ensureGroupMember(ctx, q, facts.GroupID, targetID); err != nil {
			return err
		}
		if _, err := q.UpsertDocMember(ctx, db.UpsertDocMemberParams{
			DocID: uuidParam(docID), UserID: uuidParam(targetID),
			Role: db.DocRole(role), GrantedBy: uuidParam(actorID),
		}); err != nil {
			return fmt.Errorf("设置文档成员失败: %w", err)
		}
		view, err := q.GetDocMemberView(ctx, db.GetDocMemberViewParams{DocID: uuidParam(docID), UserID: uuidParam(targetID)})
		if err != nil {
			return fmt.Errorf("查询文档成员失败: %w", err)
		}
		updated = view
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	return Member{
		UserID: updated.UserID.Bytes, Name: updated.Name, Handle: updated.Handle,
		Role: string(updated.Role), VerifyType: string(updated.VerifyType), GrantedAt: updated.CreatedAt.Time.UTC(),
	}, nil
}

// RemoveMember 移除文档级覆盖角色，目标回落到群组的 defaultRole。
// 仅 owner 可调用；创建者与群主不可移除。
func (s *Service) RemoveMember(ctx context.Context, actorID, docID, targetID uuid.UUID) error {
	return s.withTx(ctx, func(q Querier) error {
		facts, err := s.loadAccess(ctx, q, actorID, docID)
		if err != nil {
			return err
		}
		if !CanManageMembers(EffectiveRole(facts)) {
			return ErrForbidden
		}
		if err := ensureNotImmutable(facts, targetID); err != nil {
			return err
		}
		affected, err := q.DeleteDocMember(ctx, db.DeleteDocMemberParams{
			DocID: uuidParam(docID), UserID: uuidParam(targetID),
		})
		if err != nil {
			return fmt.Errorf("移除文档成员失败: %w", err)
		}
		if affected == 0 {
			return ErrMemberNotFound
		}
		return nil
	})
}

// ensureGroupMember 确认目标用户是群组成员，否则返回 ErrMemberNotFound。
func (s *Service) ensureGroupMember(ctx context.Context, q Querier, groupID uuid.UUID, targetID uuid.UUID) error {
	_, err := q.GetGroupMemberRole(ctx, db.GetGroupMemberRoleParams{
		GroupID: uuidParam(groupID), UserID: uuidParam(targetID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberNotFound
	}
	if err != nil {
		return fmt.Errorf("查询群组成员失败: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 快照
// ---------------------------------------------------------------------------

// ListSnapshots 列出文档快照元数据，要求至少 viewer。
func (s *Service) ListSnapshots(ctx context.Context, userID, docID uuid.UUID, rawCursor string, limit int32) (SnapshotsResult, error) {
	limit = normalizeLimit(limit)
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanView); err != nil {
		return SnapshotsResult{}, err
	}
	before, err := decodeVersionCursor(rawCursor, streamSnapshots)
	if err != nil {
		return SnapshotsResult{}, err
	}
	rows, err := s.queries.ListSnapshots(ctx, db.ListSnapshotsParams{
		DocID: uuidParam(docID), BeforeVersion: before, Limit: limit + 1,
	})
	if err != nil {
		return SnapshotsResult{}, fmt.Errorf("查询快照列表失败: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	snapshots := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshots = append(snapshots, snapshotFromRow(row))
	}
	var next *string
	if hasMore && len(rows) > 0 {
		encoded, err := encodeVersionCursor(streamSnapshots, rows[len(rows)-1].Version)
		if err != nil {
			return SnapshotsResult{}, err
		}
		next = &encoded
	}
	return SnapshotsResult{Snapshots: snapshots, NextCursor: next}, nil
}

// CreateSnapshot 创建一个带标签的快照元数据，要求 editor 及以上。
//
// 正文由协同服务写入对象存储；REST 侧无法读取 Y.Doc，因此这里只登记
// 元数据，storage_key 按架构文档 §5.1 的规范预置，size_bytes 暂为 0。
func (s *Service) CreateSnapshot(ctx context.Context, userID, docID uuid.UUID, label string) (Snapshot, error) {
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) > 64 {
		return Snapshot{}, fmt.Errorf("%w: 快照标签不能超过 64 个字符", ErrValidation)
	}
	var created db.DocSnapshot
	err := s.withTx(ctx, func(q Querier) error {
		facts, err := s.loadAccess(ctx, q, userID, docID)
		if err != nil {
			return err
		}
		if !CanEdit(EffectiveRole(facts)) {
			return ErrForbidden
		}
		version, err := q.BumpDocumentSnapshot(ctx, uuidParam(docID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("递增快照版本失败: %w", err)
		}
		snapshot, err := q.InsertSnapshot(ctx, db.InsertSnapshotParams{
			DocID:      uuidParam(docID),
			Version:    version,
			StorageKey: snapshotStorageKey(docID, version),
			SizeBytes:  0,
			Label:      nullableString(label),
			CreatedBy:  uuidParam(userID),
		})
		if err != nil {
			return fmt.Errorf("创建快照失败: %w", err)
		}
		created = snapshot
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}

	// 元数据行已就位，现在让协同服务把真实正文写进这个版本的 storage_key。
	//
	// 这一步不能省：正文是 Yjs 二进制，只有协同服务知道当前内容。
	// 早期实现只写元数据行（size_bytes=0、storage_key 指向不存在的对象），
	// 「回滚」回去自然是空文档。
	//
	// 失败时把刚插入的元数据行删掉——留下一条指向空对象的记录，
	// 会让用户在列表里看到一份永远打不开的快照。
	if s.reloader != nil {
		size, err := s.reloader.SnapshotDocument(ctx, docID, created.Version)
		if err != nil {
			if delErr := s.withTx(ctx, func(q Querier) error {
				return q.DeleteSnapshot(ctx, db.DeleteSnapshotParams{
					DocID: uuidParam(docID), Version: created.Version,
				})
			}); delErr != nil {
				slog.Error("回滚快照元数据失败", "docId", docID, "version", created.Version, "err", delErr)
			}
			return Snapshot{}, fmt.Errorf("快照内容写入失败: %w", err)
		}
		created.SizeBytes = size
	}

	return Snapshot{
		Version:   created.Version,
		Label:     stringValue(created.Label),
		CreatedBy: userRefPtr(created.CreatedBy.Bytes, "", ""),
		CreatedAt: created.CreatedAt.Time.UTC(),
		SizeBytes: created.SizeBytes,
	}, nil
}

// RestoreResult 是回滚操作的结果。
//
// RequiresReconnect 恒为 true：正文是 Yjs 二进制，协同服务（apps/ws）把
// Y.Doc 放在内存里，REST 侧改数据库指针无法影响已经加载的文档。
// 正确做法是前端断开 provider → 丢弃本地 Y.Doc → 重新连接，
// 由协同服务按新的 latest_snapshot 重新加载。这里诚实地告诉前端这一点，
// 而不是假装「回滚即时生效」。
type RestoreResult struct {
	RestoredFrom int32 `json:"restoredFrom"`
	NewVersion   int32 `json:"newVersion"`
	// RequiresReconnect 为 true 表示协同服务里的内存副本没能立刻失效，
	// 用户需要手动刷新才能看到回滚后的内容。
	// 为 false 表示服务端已主动卸载文档，在线用户会在客户端自动重连后
	// 拿到新版本——这是正常路径。
	RequiresReconnect bool `json:"requiresReconnect"`
	// ReloadError 在自动重载失败时给出原因，便于前端展示具体提示。
	ReloadError string `json:"reloadError,omitempty"`
}

// SnapshotContent 是下载快照正文的结果（Yjs 全量状态）。
type SnapshotContent struct {
	Version int32
	Key     string
}

// SnapshotForDownload 校验权限并返回指定版本的存储键，正文由 handler 从对象存储读取。
//
// 权限要求 editor 及以上：快照是整篇文档的完整副本，viewer 只需要阅读
// 当前正文，不应该能批量导出历史版本。
func (s *Service) SnapshotForDownload(ctx context.Context, userID, docID uuid.UUID, version int32) (SnapshotContent, error) {
	if version <= 0 {
		return SnapshotContent{}, fmt.Errorf("%w: 快照版本号必须大于 0", ErrValidation)
	}
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanEdit); err != nil {
		return SnapshotContent{}, err
	}
	snapshot, err := s.loadSnapshot(ctx, docID, version)
	if err != nil {
		return SnapshotContent{}, err
	}
	return SnapshotContent{Version: snapshot.Version, Key: snapshot.StorageKey}, nil
}

// RestoreSnapshot 把文档回滚到指定快照版本（PRD D-04「版本历史与回滚」）。
//
// 实现方式：不覆盖旧快照、也不复制二进制，而是新增一条快照记录，
// storage_key 指向被回滚版本的对象。这样做的好处：
//   - 对象存储只增不减，回滚本身不会失败在半途（没有跨系统复制）；
//   - 版本号仍然单调递增，「回滚到 v3」产生的 v7 可以再次被回滚或下载；
//   - 审计上能看出某次回滚发生在什么时候、由谁发起。
//
// 代价是同一个 storage_key 会出现在多条记录里：清理旧快照时必须先确认
// 没有其他记录引用它（见 db/queries/documents.sql 的说明）。
func (s *Service) RestoreSnapshot(ctx context.Context, userID, docID uuid.UUID, version int32) (RestoreResult, error) {
	if version <= 0 {
		return RestoreResult{}, fmt.Errorf("%w: 快照版本号必须大于 0", ErrValidation)
	}
	// 先判权限再查版本：否则无权用户能通过 ErrNotFound / ErrForbidden 的
	// 差异探测「某个版本是否存在」。
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanEdit); err != nil {
		return RestoreResult{}, err
	}

	target, err := s.loadSnapshot(ctx, docID, version)
	if err != nil {
		return RestoreResult{}, err
	}

	var created db.DocSnapshot
	err = s.withTx(ctx, func(q Querier) error {
		facts, err := s.loadAccess(ctx, q, userID, docID)
		if err != nil {
			return err
		}
		if !CanEdit(EffectiveRole(facts)) {
			return ErrForbidden
		}
		newVersion, err := q.BumpDocumentSnapshot(ctx, uuidParam(docID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("递增快照版本失败: %w", err)
		}
		label := fmt.Sprintf("回滚到 v%d", version)
		created, err = q.InsertSnapshot(ctx, db.InsertSnapshotParams{
			DocID:      uuidParam(docID),
			Version:    newVersion,
			StorageKey: target.StorageKey,
			SizeBytes:  target.SizeBytes,
			Label:      &label,
			CreatedBy:  uuidParam(userID),
		})
		if err != nil {
			return fmt.Errorf("记录回滚快照失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return RestoreResult{}, err
	}

	// 数据库与对象存储已经指向旧版本，但协同服务内存里的 Y.Doc 还是新的。
	// 主动让对端卸载它：在线客户端会收到可恢复的断开，随后自动重连，
	// 重连时从最新快照重建 —— 这正是我们要的「回滚立即生效」。
	//
	// 失败不视为回滚失败：数据层面已经回滚，只是在线用户需要手动刷新。
	// 如实返回原因，而不是假装成功。
	result := RestoreResult{
		RestoredFrom:      version,
		NewVersion:        created.Version,
		RequiresReconnect: false,
	}
	if s.reloader == nil {
		result.RequiresReconnect = true
		result.ReloadError = "协同服务未配置，请刷新页面以加载该版本"
		return result, nil
	}
	if err := s.reloader.ReloadDocument(ctx, docID); err != nil {
		result.RequiresReconnect = true
		result.ReloadError = err.Error()
	}
	return result, nil
}

// loadSnapshot 读取指定版本的快照记录；不存在时返回 ErrNotFound。
func (s *Service) loadSnapshot(ctx context.Context, docID uuid.UUID, version int32) (db.DocSnapshot, error) {
	if s.snapshots == nil {
		return db.DocSnapshot{}, fmt.Errorf("快照存储未配置")
	}
	snapshot, err := s.snapshots.GetSnapshotByVersion(ctx, uuidParam(docID), version)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.DocSnapshot{}, ErrNotFound
	}
	if err != nil {
		return db.DocSnapshot{}, fmt.Errorf("查询快照失败: %w", err)
	}
	return snapshot, nil
}

// snapshotStorageKey 返回架构文档 §5.1 约定的快照对象键。
func snapshotStorageKey(docID uuid.UUID, version int32) string {
	return fmt.Sprintf("doc-snapshot/%s/%d.bin", docID, version)
}

// ---------------------------------------------------------------------------
// 内部辅助
// ---------------------------------------------------------------------------

// loadAccess 读取权限事实，文档不存在/已删除/群已删除时返回 ErrNotFound。
func (s *Service) loadAccess(ctx context.Context, q Querier, userID, docID uuid.UUID) (AccessFacts, error) {
	row, err := q.GetDocumentAccess(ctx, db.GetDocumentAccessParams{
		ID: uuidParam(docID), UserID: uuidParam(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessFacts{}, ErrNotFound
	}
	if err != nil {
		return AccessFacts{}, fmt.Errorf("查询文档权限失败: %w", err)
	}
	groupRole := ""
	if row.GroupRole.Valid {
		groupRole = string(row.GroupRole.GroupRole)
	}
	docRole := ""
	if row.DocRole.Valid {
		docRole = string(row.DocRole.DocRole)
	}
	return AccessFacts{
		UserID:       userID,
		GroupID:      row.GroupID.Bytes,
		CreatedBy:    row.CreatedBy.Bytes,
		GroupOwnerID: row.GroupOwnerID.Bytes,
		GroupRole:    groupRole,
		DocRole:      Role(docRole),
		DefaultRole:  Role(row.DefaultRole),
	}, nil
}

// requireRole 读取权限并断言满足 predicate，返回有效角色。
func (s *Service) requireRole(ctx context.Context, q Querier, userID, docID uuid.UUID, allowed func(Role) bool) (Role, error) {
	facts, err := s.loadAccess(ctx, q, userID, docID)
	if err != nil {
		return "", err
	}
	role := EffectiveRole(facts)
	if !allowed(role) {
		// 完全无权查看时返回 404，避免泄露文档存在性；
		// 能看但不能写时返回 403，让前端给出准确提示。
		if !CanView(role) {
			return "", ErrNotFound
		}
		return "", ErrForbidden
	}
	return role, nil
}

// ensureNotImmutable 拒绝修改创建者/群主的文档角色。
func ensureNotImmutable(facts AccessFacts, targetID uuid.UUID) error {
	if targetID == facts.CreatedBy || targetID == facts.GroupOwnerID {
		return ErrOwnerImmutable
	}
	return nil
}

func parseStatus(raw string) (db.DocStatus, bool) {
	switch db.DocStatus(raw) {
	case db.DocStatusDraft, db.DocStatusActive, db.DocStatusArchived:
		return db.DocStatus(raw), true
	}
	return "", false
}

func documentFromRow(row db.GetDocumentByIDRow) Document {
	return Document{
		ID:             row.ID.Bytes,
		GroupID:        row.GroupID.Bytes,
		GroupSlug:      stringValue(row.GroupSlug),
		Title:          row.Title,
		Status:         string(row.Status),
		DefaultRole:    string(row.DefaultRole),
		WordCount:      row.WordCount,
		LatestSnapshot: row.LatestSnapshot,
		CreatedAt:      row.CreatedAt.Time.UTC(),
		UpdatedAt:      row.UpdatedAt.Time.UTC(),
		CreatedBy:      UserRef{ID: row.CreatedBy.Bytes, Name: row.CreatorName, Handle: row.CreatorHandle},
	}
}

func documentFromListRow(row db.ListDocumentsByGroupRow) Document {
	return Document{
		ID:             row.ID.Bytes,
		GroupID:        row.GroupID.Bytes,
		GroupSlug:      stringValue(row.GroupSlug),
		Title:          row.Title,
		Status:         string(row.Status),
		DefaultRole:    string(row.DefaultRole),
		WordCount:      row.WordCount,
		LatestSnapshot: row.LatestSnapshot,
		CreatedAt:      row.CreatedAt.Time.UTC(),
		UpdatedAt:      row.UpdatedAt.Time.UTC(),
		CreatedBy:      UserRef{ID: row.CreatedBy.Bytes, Name: row.CreatorName, Handle: row.CreatorHandle},
	}
}

func snapshotFromRow(row db.ListSnapshotsRow) Snapshot {
	var creator *UserRef
	if row.CreatedBy.Valid {
		name, handle := "", ""
		if row.CreatorName != nil {
			name = *row.CreatorName
		}
		if row.CreatorHandle != nil {
			handle = *row.CreatorHandle
		}
		creator = &UserRef{ID: row.CreatedBy.Bytes, Name: name, Handle: handle}
	}
	return Snapshot{
		Version:   row.Version,
		Label:     stringValue(row.Label),
		CreatedBy: creator,
		CreatedAt: row.CreatedAt.Time.UTC(),
		SizeBytes: row.SizeBytes,
	}
}

func userRefPtr(id uuid.UUID, name, handle string) *UserRef {
	if id == uuid.Nil {
		return nil
	}
	return &UserRef{ID: id, Name: name, Handle: handle}
}

func normalizeLimit(limit int32) int32 {
	if limit <= 0 {
		return 20
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func timeCursor(value time.Time, raw string) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: raw != ""}
}

func uuidParam(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
