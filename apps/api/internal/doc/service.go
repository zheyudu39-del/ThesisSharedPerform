package doc

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
	InsertSnapshot(context.Context, db.InsertSnapshotParams) (db.DocSnapshot, error)
	ListSnapshots(context.Context, db.ListSnapshotsParams) ([]db.ListSnapshotsRow, error)

	CreateAnnotation(context.Context, db.CreateAnnotationParams) (db.Annotation, error)
	GetAnnotation(context.Context, db.GetAnnotationParams) (db.GetAnnotationRow, error)
	ListAnnotations(context.Context, pgtype.UUID) ([]db.ListAnnotationsRow, error)
	UpdateAnnotationContent(context.Context, db.UpdateAnnotationContentParams) (pgtype.UUID, error)
	SetAnnotationResolved(context.Context, db.SetAnnotationResolvedParams) (pgtype.UUID, error)
}

// Service 实现文档领域逻辑。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	now     func() time.Time
}

// NewService 构造服务。pool 为 nil 时只读路径可用（测试替身场景）。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: db.New(pool), now: time.Now}
}

// newServiceWithQuerier 用于测试注入替身。
func newServiceWithQuerier(queries Querier) *Service {
	return &Service{queries: queries, now: time.Now}
}

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
		return nil
	})
	if err != nil {
		return Document{}, err
	}
	updated, _, err := s.GetDocument(ctx, userID, docID)
	return updated, err
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
	return Snapshot{
		Version:   created.Version,
		Label:     stringValue(created.Label),
		CreatedBy: userRefPtr(created.CreatedBy.Bytes, "", ""),
		CreatedAt: created.CreatedAt.Time.UTC(),
		SizeBytes: created.SizeBytes,
	}, nil
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
