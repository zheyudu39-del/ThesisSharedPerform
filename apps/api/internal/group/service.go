package group

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/content"
	"github.com/thesis-shared/api/internal/db"
)

// Group 是群组对外 DTO，字段与两端契约严格一致。
type Group struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	// CoverURL 目前恒为 null：封面对象存储链路尚未接入，先保留字段占位。
	CoverURL    *string   `json:"coverUrl"`
	OwnerID     uuid.UUID `json:"ownerId"`
	OwnerName   string    `json:"ownerName"`
	JoinPolicy  string    `json:"joinPolicy"`
	MemberCount int32     `json:"memberCount"`
	MyRole      *string   `json:"myRole"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Member 是成员 DTO。
type Member struct {
	UserID     uuid.UUID `json:"userId"`
	Name       string    `json:"name"`
	Handle     string    `json:"handle"`
	Role       string    `json:"role"`
	JoinedAt   time.Time `json:"joinedAt"`
	VerifyType string    `json:"verifyType"`
}

// Invite 是邀请码 DTO。ExpiresAt 为 nil 表示永不过期；
// MaxUses 为 nil 表示不限次数。
type Invite struct {
	Code      string     `json:"code"`
	ExpiresAt *time.Time `json:"expiresAt"`
	MaxUses   *int32     `json:"maxUses"`
}

// RequestUser 是入群申请里的申请人摘要。
type RequestUser struct {
	Name   string `json:"name"`
	Handle string `json:"handle"`
}

// JoinRequest 是入群申请 DTO。
// User 在「本人提交申请」的响应里省略（此时无需回显自己的资料），
// 管理端列表与审批响应里始终带出申请人摘要。
type JoinRequest struct {
	ID        uuid.UUID    `json:"id"`
	User      *RequestUser `json:"user,omitempty"`
	Message   string       `json:"message"`
	Status    string       `json:"status"`
	CreatedAt time.Time    `json:"createdAt"`
}

// GroupsResult 是群组列表的一页。
type GroupsResult struct {
	Groups     []Group `json:"groups"`
	NextCursor *string `json:"nextCursor"`
}

// MembersResult 是成员列表的一页。
type MembersResult struct {
	Members    []Member `json:"members"`
	NextCursor *string  `json:"nextCursor"`
}

// RequestsResult 是入群申请列表。
type RequestsResult struct {
	Requests []JoinRequest `json:"requests"`
}

// FeedResult 是群内动态的一页，复用 content 模块的 PostDTO。
type FeedResult struct {
	Posts      []content.Post `json:"posts"`
	NextCursor *string        `json:"nextCursor"`
}

// CreateGroupInput 是建群入参。
type CreateGroupInput struct {
	Name        string
	Description string
	JoinPolicy  string
}

// UpdateGroupInput 是局部更新入参：nil 表示请求体未提供该字段。
type UpdateGroupInput struct {
	Name        *string
	Description *string
	JoinPolicy  *string
}

// CreateInviteInput 是创建邀请码入参。
type CreateInviteInput struct {
	ExpiresInHours *int32
	MaxUses        *int32
}

// Querier 限定群组服务使用的生成查询，便于单元测试注入替身。
type Querier interface {
	CreateGroup(context.Context, db.CreateGroupParams) (db.Group, error)
	GroupSlugExists(context.Context, *string) (bool, error)
	GetGroupBySlug(context.Context, db.GetGroupBySlugParams) (db.GetGroupBySlugRow, error)
	GetGroupByID(context.Context, db.GetGroupByIDParams) (db.GetGroupByIDRow, error)
	UpdateGroup(context.Context, db.UpdateGroupParams) (db.Group, error)
	ListPublicGroups(context.Context, db.ListPublicGroupsParams) ([]db.ListPublicGroupsRow, error)

	AddGroupMember(context.Context, db.AddGroupMemberParams) (int64, error)
	GetGroupMember(context.Context, db.GetGroupMemberParams) (db.GroupMember, error)
	GetGroupMemberView(context.Context, db.GetGroupMemberViewParams) (db.GetGroupMemberViewRow, error)
	ListGroupMembers(context.Context, db.ListGroupMembersParams) ([]db.ListGroupMembersRow, error)
	UpdateGroupMemberRole(context.Context, db.UpdateGroupMemberRoleParams) (db.GroupMember, error)
	RemoveGroupMember(context.Context, db.RemoveGroupMemberParams) (int64, error)
	RecomputeGroupMemberCount(context.Context, pgtype.UUID) (int32, error)

	CreateGroupInvite(context.Context, db.CreateGroupInviteParams) (db.GroupInvite, error)
	GetGroupInviteByCode(context.Context, string) (db.GroupInvite, error)
	ConsumeGroupInvite(context.Context, pgtype.UUID) (db.GroupInvite, error)

	GetJoinRequestByUser(context.Context, db.GetJoinRequestByUserParams) (db.JoinRequest, error)
	CreateJoinRequest(context.Context, db.CreateJoinRequestParams) (db.JoinRequest, error)
	GetJoinRequestByID(context.Context, db.GetJoinRequestByIDParams) (db.GetJoinRequestByIDRow, error)
	ListJoinRequests(context.Context, pgtype.UUID) ([]db.ListJoinRequestsRow, error)
	UpdateJoinRequestStatus(context.Context, db.UpdateJoinRequestStatusParams) (db.JoinRequest, error)

	ListGroupFeed(context.Context, db.ListGroupFeedParams) ([]db.ListGroupFeedRow, error)
}

// SearchDocument 是要写入检索索引的最小字段集合。
// 与 content.SearchDocument 形状一致，但各自声明，避免两个业务包互相依赖。
type SearchDocument struct {
	EntityType string
	EntityID   uuid.UUID
	Title      string
	Body       string
}

// Indexer 是检索索引的抽象，由 server 在组装时注入 search.Service 的适配器。
// 为 nil 时所有索引调用都被跳过，业务写入不受影响。
type Indexer interface {
	Index(ctx context.Context, doc SearchDocument) error
}

// NotifyInput 描述一条待生成的通知，字段与 notify.CreateInput 一一对应。
// 与 SearchDocument 同理，这里重新声明一份而不直接引用 notify 包。
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

// managerQuerier 查询「谁应该收到管理类通知」。
//
// 单独抽成接口而不是塞进 Querier：Querier 被单测替身完整实现，
// 往里加方法会让既有替身编译失败；且该查询只服务于通知，不是群组业务本身。
type managerQuerier interface {
	ListGroupManagerIDs(ctx context.Context, groupID pgtype.UUID) ([]pgtype.UUID, error)
}

// dbManagerQuerier 用生成查询取回群主与管理员。
// 群主在 group_members 里也有一行 role='owner'，因此无需再查 groups.owner_id。
type dbManagerQuerier struct{ queries *db.Queries }

func (q dbManagerQuerier) ListGroupManagerIDs(ctx context.Context, groupID pgtype.UUID) ([]pgtype.UUID, error) {
	return q.queries.ListGroupManagerIDs(ctx, groupID)
}

// Service 实现群组领域逻辑。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	now     func() time.Time
	// indexer 可选：nil 时跳过索引。
	indexer Indexer
	// notifier 可选：nil 时跳过通知生成。
	notifier Notifier
	// managers 可选：入群申请通知的收件人查询。为 nil 时跳过该通知。
	managers managerQuerier
}

// NewService 构造服务。pool 为 nil 时只读路径可用（测试替身场景）。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: db.New(pool), now: time.Now, managers: dbManagerQuerier{queries: db.New(pool)}}
}

// SetIndexer 注入检索索引实现；不注入时索引被静默跳过。
func (s *Service) SetIndexer(indexer Indexer) { s.indexer = indexer }

// SetNotifier 注入通知实现；不注入时入群申请等事件不产生通知。
func (s *Service) SetNotifier(notifier Notifier) { s.notifier = notifier }

// newServiceWithQuerier 用于测试注入替身；写路径在没有连接池时直接使用替身。
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
// 群组 CRUD
// ---------------------------------------------------------------------------

// CreateGroup 在事务中创建群组、写入 owner 成员行并重算成员数。
//
// member_count 不在应用层直接加一，而是在插入成员后从 group_members
// 实际行数重算：即使后续插入逻辑变化，计数也不会与真实成员数脱节。
func (s *Service) CreateGroup(ctx context.Context, ownerID uuid.UUID, input CreateGroupInput) (Group, error) {
	name, description, policy, err := validateCreate(input)
	if err != nil {
		return Group{}, err
	}
	// 两个同名建群请求并发时，双方都可能在存在性检查时看到「未占用」，
	// 随后其中一个撞上 slug 唯一索引。这不是客户端错误，重新生成后缀再试即可。
	const maxSlugRetries = 3
	var created db.Group
	for attempt := 0; attempt < maxSlugRetries; attempt++ {
		created, err = s.createGroupOnce(ctx, ownerID, name, description, policy)
		if !errors.Is(err, errSlugConflict) {
			break
		}
	}
	if err != nil {
		if errors.Is(err, errSlugConflict) {
			// 连续多次撞唯一索引，说明同名群组创建过于密集；让客户端换名重试。
			return Group{}, fmt.Errorf("%w: 同名群组过多，请换一个群名", ErrValidation)
		}
		return Group{}, err
	}
	// 建群已提交，索引失败只记日志，不影响建群结果。
	s.indexGroup(ctx, created.ID.Bytes, policy, name, description)
	return s.GetGroup(ctx, ownerID, stringValue(created.Slug))
}

// indexGroup 把群名与简介写入检索索引。
//
// 只索引 joinPolicy = public 的群：检索接口匿名可读，而「发现群组」列表
// 也只暴露公开群；索引仅邀请/需审批的群会泄漏其存在与简介。
// 标题用群名，正文用简介；简介为空时只索引标题。
func (s *Service) indexGroup(ctx context.Context, groupID uuid.UUID, policy, name, description string) {
	if s.indexer == nil || policy != string(db.GroupJoinPolicyPublic) {
		return
	}
	if err := s.indexer.Index(ctx, SearchDocument{
		EntityType: "group",
		EntityID:   groupID,
		Title:      name,
		Body:       strings.TrimSpace(description),
	}); err != nil {
		slog.Error("索引群组失败", "groupId", groupID, "err", err)
	}
}

func (s *Service) createGroupOnce(ctx context.Context, ownerID uuid.UUID, name, description, policy string) (db.Group, error) {
	var created db.Group
	err := s.withTx(ctx, func(q Querier) error {
		slug, err := uniqueSlug(Slugify(name), func(candidate string) (bool, error) {
			return q.GroupSlugExists(ctx, &candidate)
		}, randomSuffix)
		if err != nil {
			return err
		}
		group, err := q.CreateGroup(ctx, db.CreateGroupParams{
			Name:        name,
			Slug:        &slug,
			Description: nullableString(description),
			OwnerID:     uuidParam(ownerID),
			JoinPolicy:  db.GroupJoinPolicy(policy),
		})
		if isSlugConflict(err) {
			return errSlugConflict
		}
		if err != nil {
			return fmt.Errorf("创建群组失败: %w", err)
		}
		if _, err := q.AddGroupMember(ctx, db.AddGroupMemberParams{
			GroupID: group.ID, UserID: uuidParam(ownerID), Role: db.GroupRoleOwner,
		}); err != nil {
			return fmt.Errorf("写入群主成员失败: %w", err)
		}
		if _, err := q.RecomputeGroupMemberCount(ctx, group.ID); err != nil {
			return fmt.Errorf("重算成员数失败: %w", err)
		}
		created = group
		return nil
	})
	return created, err
}

// errSlugConflict 是内部信号：slug 唯一索引冲突，调用方应换一个后缀重试。
var errSlugConflict = errors.New("slug 冲突")

// isSlugConflict 识别 groups_slug_key 唯一约束冲突。
// 只认这一条约束，其他唯一冲突（例如未来新增的约束）仍按普通错误处理。
func isSlugConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return pgErr.ConstraintName == "groups_slug_key"
}

// GetGroup 按 slug 读取群组。viewerID 为 uuid.Nil 表示匿名，
// 此时 MyRole 恒为 nil。
func (s *Service) GetGroup(ctx context.Context, viewerID uuid.UUID, slug string) (Group, error) {
	row, err := s.queries.GetGroupBySlug(ctx, db.GetGroupBySlugParams{Slug: &slug, UserID: uuidParam(viewerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrGroupNotFound
	}
	if err != nil {
		return Group{}, fmt.Errorf("查询群组失败: %w", err)
	}
	return groupFromRow(row), nil
}

// UpdateGroup 局部更新群资料，仅 owner/admin 可调用。
func (s *Service) UpdateGroup(ctx context.Context, userID uuid.UUID, slug string, input UpdateGroupInput) (Group, error) {
	params := db.UpdateGroupParams{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
			return Group{}, fmt.Errorf("%w: 群组名称长度须为 1 到 64 个字符", ErrValidation)
		}
		params.Name = &name
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(description) > 500 {
			return Group{}, fmt.Errorf("%w: 群组简介不能超过 500 个字符", ErrValidation)
		}
		params.Description = &description
	}
	if input.JoinPolicy != nil {
		policy, ok := parseJoinPolicy(*input.JoinPolicy)
		if !ok {
			return Group{}, fmt.Errorf("%w: joinPolicy 只能是 public、request 或 invite", ErrValidation)
		}
		params.JoinPolicy = db.NullGroupJoinPolicy{GroupJoinPolicy: policy, Valid: true}
	}

	var updated db.Group
	err := s.withTx(ctx, func(q Querier) error {
		group, actorRole, err := loadGroupForActor(ctx, q, userID, slug)
		if err != nil {
			return err
		}
		if !CanManageGroup(actorRole) {
			return ErrForbidden
		}
		params.ID = group.ID
		record, err := q.UpdateGroup(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrGroupNotFound
		}
		if err != nil {
			return fmt.Errorf("更新群组失败: %w", err)
		}
		updated = record
		return nil
	})
	if err != nil {
		return Group{}, err
	}
	return s.GetGroup(ctx, userID, stringValue(updated.Slug))
}

// ListGroups 列出公开群组（joinPolicy = public），支持游标翻页。
func (s *Service) ListGroups(ctx context.Context, viewerID uuid.UUID, rawCursor string, limit int32) (GroupsResult, error) {
	limit = normalizeLimit(limit)
	createdAt, id, err := decodeCursor(rawCursor, "groups")
	if err != nil {
		return GroupsResult{}, err
	}
	rows, err := s.queries.ListPublicGroups(ctx, db.ListPublicGroupsParams{
		UserID:  uuidParam(viewerID),
		Column2: timeCursor(createdAt, rawCursor),
		Column3: uuidParam(id),
		Limit:   limit + 1,
	})
	if err != nil {
		return GroupsResult{}, fmt.Errorf("查询群组列表失败: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	groups := make([]Group, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, groupFromRow(db.GetGroupBySlugRow(row)))
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor("groups", last.CreatedAt.Time, last.ID.Bytes)
		if err != nil {
			return GroupsResult{}, err
		}
		next = &encoded
	}
	return GroupsResult{Groups: groups, NextCursor: next}, nil
}

// ---------------------------------------------------------------------------
// 成员
// ---------------------------------------------------------------------------

// ListMembers 返回群成员列表。
//
// 公开群任何登录用户都可查看；非公开群（request / invite）要求查看者是成员，
// 否则群成员名单本身就会泄露给未受邀者。
func (s *Service) ListMembers(ctx context.Context, viewerID uuid.UUID, slug, rawCursor string, limit int32) (MembersResult, error) {
	limit = normalizeLimit(limit)
	group, actorRole, err := loadGroupForActor(ctx, s.queries, viewerID, slug)
	if err != nil {
		return MembersResult{}, err
	}
	if group.JoinPolicy != db.GroupJoinPolicyPublic && actorRole == "" {
		return MembersResult{}, ErrForbidden
	}
	createdAt, id, err := decodeCursor(rawCursor, "members")
	if err != nil {
		return MembersResult{}, err
	}
	rows, err := s.queries.ListGroupMembers(ctx, db.ListGroupMembersParams{
		GroupID: group.ID,
		Column2: timeCursor(createdAt, rawCursor),
		Column3: uuidParam(id),
		Limit:   limit + 1,
	})
	if err != nil {
		return MembersResult{}, fmt.Errorf("查询成员列表失败: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, memberFromRow(row))
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor("members", last.JoinedAt.Time, last.UserID.Bytes)
		if err != nil {
			return MembersResult{}, err
		}
		next = &encoded
	}
	return MembersResult{Members: members, NextCursor: next}, nil
}

// UpdateMemberRole 修改成员角色，仅 owner/admin 可调用。
func (s *Service) UpdateMemberRole(ctx context.Context, actorID uuid.UUID, slug string, targetID uuid.UUID, newRole string) (Member, error) {
	role, ok := ParseRole(newRole)
	if !ok || role == RoleOwner {
		return Member{}, fmt.Errorf("%w: 角色只能是 admin 或 member", ErrValidation)
	}
	var updated db.GetGroupMemberViewRow
	err := s.withTx(ctx, func(q Querier) error {
		group, actorRole, err := loadGroupForActor(ctx, q, actorID, slug)
		if err != nil {
			return err
		}
		target, err := q.GetGroupMember(ctx, db.GetGroupMemberParams{GroupID: group.ID, UserID: uuidParam(targetID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemberNotFound
		}
		if err != nil {
			return fmt.Errorf("查询目标成员失败: %w", err)
		}
		if err := CanChangeRoleTo(actorRole, RoleFromDB(target.Role), role, targetID == actorID); err != nil {
			return err
		}
		if _, err := q.UpdateGroupMemberRole(ctx, db.UpdateGroupMemberRoleParams{
			GroupID: group.ID, UserID: uuidParam(targetID), Role: db.GroupRole(role),
		}); err != nil {
			return fmt.Errorf("修改成员角色失败: %w", err)
		}
		view, err := q.GetGroupMemberView(ctx, db.GetGroupMemberViewParams{GroupID: group.ID, UserID: uuidParam(targetID)})
		if err != nil {
			return fmt.Errorf("查询成员信息失败: %w", err)
		}
		updated = view
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	return Member{
		UserID: updated.UserID.Bytes, Name: updated.Name, Handle: updated.Handle,
		Role: string(updated.Role), JoinedAt: updated.JoinedAt.Time.UTC(), VerifyType: string(updated.VerifyType),
	}, nil
}

// RemoveMember 移除成员，或由本人退出。
//
// 删除成员与重算计数在同一事务：计数若与删除脱节，公开列表会显示错误的规模。
func (s *Service) RemoveMember(ctx context.Context, actorID uuid.UUID, slug string, targetID uuid.UUID) error {
	return s.withTx(ctx, func(q Querier) error {
		group, actorRole, err := loadGroupForActor(ctx, q, actorID, slug)
		if err != nil {
			return err
		}
		target, err := q.GetGroupMember(ctx, db.GetGroupMemberParams{GroupID: group.ID, UserID: uuidParam(targetID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemberNotFound
		}
		if err != nil {
			return fmt.Errorf("查询目标成员失败: %w", err)
		}
		sameUser := actorID == targetID
		if err := CanRemoveMember(actorRole, RoleFromDB(target.Role), sameUser); err != nil {
			return err
		}
		if _, err := q.RemoveGroupMember(ctx, db.RemoveGroupMemberParams{GroupID: group.ID, UserID: uuidParam(targetID)}); err != nil {
			return fmt.Errorf("移除成员失败: %w", err)
		}
		if _, err := q.RecomputeGroupMemberCount(ctx, group.ID); err != nil {
			return fmt.Errorf("重算成员数失败: %w", err)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// 邀请码
// ---------------------------------------------------------------------------

// CreateInvite 创建邀请码，仅 owner/admin 可调用。
// 默认有效期 168 小时；maxUses 为空表示不限次数。
func (s *Service) CreateInvite(ctx context.Context, actorID uuid.UUID, slug string, input CreateInviteInput) (Invite, error) {
	hours := int32(168)
	if input.ExpiresInHours != nil {
		hours = *input.ExpiresInHours
	}
	if hours <= 0 || hours > 24*365 {
		return Invite{}, fmt.Errorf("%w: expiresInHours 须为 1 到 8760", ErrValidation)
	}
	if input.MaxUses != nil && (*input.MaxUses <= 0 || *input.MaxUses > 100000) {
		return Invite{}, fmt.Errorf("%w: maxUses 须为 1 到 100000", ErrValidation)
	}
	var created db.GroupInvite
	err := s.withTx(ctx, func(q Querier) error {
		group, actorRole, err := loadGroupForActor(ctx, q, actorID, slug)
		if err != nil {
			return err
		}
		if !CanManageGroup(actorRole) {
			return ErrForbidden
		}
		code, err := inviteCode()
		if err != nil {
			return err
		}
		expiresAt := s.now().UTC().Add(time.Duration(hours) * time.Hour)
		invite, err := q.CreateGroupInvite(ctx, db.CreateGroupInviteParams{
			GroupID:   group.ID,
			Code:      code,
			CreatedBy: uuidParam(actorID),
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
			MaxUses:   input.MaxUses,
		})
		if err != nil {
			return fmt.Errorf("创建邀请码失败: %w", err)
		}
		created = invite
		return nil
	})
	if err != nil {
		return Invite{}, err
	}
	return inviteFromDB(created), nil
}

// JoinByInvite 通过邀请码加入群组。
//
// 邀请码不存在、已过期、已用尽统一返回 ErrInviteNotFound，
// 使持码人无法通过响应差异判断某个码曾经存在。
func (s *Service) JoinByInvite(ctx context.Context, userID uuid.UUID, code string) (Group, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Group{}, fmt.Errorf("%w: 邀请码不能为空", ErrValidation)
	}
	invite, err := s.queries.GetGroupInviteByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrInviteNotFound
	}
	if err != nil {
		return Group{}, fmt.Errorf("查询邀请码失败: %w", err)
	}
	if !inviteUsable(invite, s.now().UTC()) {
		return Group{}, ErrInviteNotFound
	}
	var group db.GetGroupByIDRow
	err = s.withTx(ctx, func(q Querier) error {
		loaded, err := q.GetGroupByID(ctx, db.GetGroupByIDParams{ID: invite.GroupID, UserID: uuidParam(userID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteNotFound
		}
		if err != nil {
			return fmt.Errorf("查询群组失败: %w", err)
		}
		if loaded.MyRole.Valid {
			return ErrAlreadyMember
		}
		// 先原子地占用一次额度，再写成员行：并发用同一码时只有额度内的请求能成功。
		if _, err := q.ConsumeGroupInvite(ctx, invite.ID); errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteNotFound
		} else if err != nil {
			return fmt.Errorf("消耗邀请码失败: %w", err)
		}
		if _, err := q.AddGroupMember(ctx, db.AddGroupMemberParams{
			GroupID: loaded.ID, UserID: uuidParam(userID), Role: db.GroupRoleMember,
		}); err != nil {
			return fmt.Errorf("加入群组失败: %w", err)
		}
		if _, err := q.RecomputeGroupMemberCount(ctx, loaded.ID); err != nil {
			return fmt.Errorf("重算成员数失败: %w", err)
		}
		group = loaded
		return nil
	})
	if err != nil {
		return Group{}, err
	}
	return s.GetGroup(ctx, userID, stringValue(group.Slug))
}

// JoinPublic 直接加入公开群组。
func (s *Service) JoinPublic(ctx context.Context, userID uuid.UUID, slug string) (Group, error) {
	var group db.GetGroupBySlugRow
	err := s.withTx(ctx, func(q Querier) error {
		loaded, actorRole, err := loadGroupForActor(ctx, q, userID, slug)
		if err != nil {
			return err
		}
		if loaded.JoinPolicy != db.GroupJoinPolicyPublic {
			return ErrJoinPolicy
		}
		if actorRole != "" {
			return ErrAlreadyMember
		}
		if _, err := q.AddGroupMember(ctx, db.AddGroupMemberParams{
			GroupID: loaded.ID, UserID: uuidParam(userID), Role: db.GroupRoleMember,
		}); err != nil {
			return fmt.Errorf("加入群组失败: %w", err)
		}
		if _, err := q.RecomputeGroupMemberCount(ctx, loaded.ID); err != nil {
			return fmt.Errorf("重算成员数失败: %w", err)
		}
		group = loaded
		return nil
	})
	if err != nil {
		return Group{}, err
	}
	return s.GetGroup(ctx, userID, stringValue(group.Slug))
}

// ---------------------------------------------------------------------------
// 入群申请
// ---------------------------------------------------------------------------

// CreateJoinRequest 提交入群申请，仅适用于 joinPolicy = request 的群组。
func (s *Service) CreateJoinRequest(ctx context.Context, userID uuid.UUID, slug, message string) (JoinRequest, error) {
	message = strings.TrimSpace(message)
	if utf8.RuneCountInString(message) > 280 {
		return JoinRequest{}, fmt.Errorf("%w: 申请留言不能超过 280 个字符", ErrValidation)
	}
	var created db.JoinRequest
	var groupID, ownerID uuid.UUID
	var groupSlug string
	err := s.withTx(ctx, func(q Querier) error {
		group, actorRole, err := loadGroupForActor(ctx, q, userID, slug)
		if err != nil {
			return err
		}
		if group.JoinPolicy != db.GroupJoinPolicyRequest {
			return ErrJoinPolicy
		}
		if actorRole != "" {
			return ErrAlreadyMember
		}
		existing, err := q.GetJoinRequestByUser(ctx, db.GetJoinRequestByUserParams{GroupID: group.ID, UserID: uuidParam(userID)})
		if err == nil && existing.Status == db.ReportStatusPending {
			return ErrAlreadyRequested
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("查询入群申请失败: %w", err)
		}
		record, err := q.CreateJoinRequest(ctx, db.CreateJoinRequestParams{
			GroupID: group.ID, UserID: uuidParam(userID), Message: nullableString(message),
		})
		if err != nil {
			return fmt.Errorf("提交入群申请失败: %w", err)
		}
		created = record
		groupID, ownerID = group.ID.Bytes, group.OwnerID.Bytes
		groupSlug = stringValue(group.Slug)
		return nil
	})
	if err != nil {
		return JoinRequest{}, err
	}
	// 通知放在提交之后：群主/管理员收不到提醒只是体验问题，
	// 不能让已经写好的申请因为通知失败而回滚。
	s.notifyJoinRequest(ctx, groupID, ownerID, userID, created.ID.Bytes, groupSlug)
	return JoinRequest{
		ID: created.ID.Bytes, Status: string(created.Status),
		Message: stringValue(created.Message), CreatedAt: created.CreatedAt.Time.UTC(),
	}, nil
}

// notifyJoinRequest 给群主与所有管理员发入群申请提醒。
//
// 申请人自己不会收到（由 notify 服务层的自行为判断兜底，这里提前跳过省一次调用）；
// 群主若同时也是 admin 行只会在查询结果里出现一次，无需额外去重。
// payload 带 slug 与申请人 handle，前端跳转与文案都用得上。
func (s *Service) notifyJoinRequest(ctx context.Context, groupID, ownerID, applicantID, requestID uuid.UUID, groupSlug string) {
	if s.notifier == nil || s.managers == nil || groupID == uuid.Nil {
		return
	}
	managers, err := s.managers.ListGroupManagerIDs(ctx, uuidParam(groupID))
	if err != nil {
		slog.Error("查询群管理员失败", "groupId", groupID, "err", err)
		return
	}
	recipients := make(map[uuid.UUID]struct{}, len(managers)+1)
	for _, id := range managers {
		if id.Valid {
			recipients[uuid.UUID(id.Bytes)] = struct{}{}
		}
	}
	// 兜底：即使成员表暂时缺失群主行，也要提醒群主。
	if ownerID != uuid.Nil {
		recipients[ownerID] = struct{}{}
	}
	for recipient := range recipients {
		if recipient == applicantID {
			continue
		}
		payload := map[string]any{"requestId": requestID.String()}
		// 群组路由按 slug 而不是 ID，带上 slug 前端才能直接拼出可点击的链接。
		if groupSlug != "" {
			payload["slug"] = groupSlug
		}
		if err := s.notifier.Notify(ctx, NotifyInput{
			UserID:     recipient,
			Type:       "join_request",
			ActorID:    applicantID,
			TargetType: "group",
			TargetID:   groupID,
			Payload:    payload,
		}); err != nil {
			slog.Error("生成入群申请通知失败", "groupId", groupID, "userId", recipient, "err", err)
		}
	}
}

// ListJoinRequests 列出待处理的入群申请，仅 owner/admin 可调用。
func (s *Service) ListJoinRequests(ctx context.Context, actorID uuid.UUID, slug string) (RequestsResult, error) {
	group, actorRole, err := loadGroupForActor(ctx, s.queries, actorID, slug)
	if err != nil {
		return RequestsResult{}, err
	}
	if !CanManageGroup(actorRole) {
		return RequestsResult{}, ErrForbidden
	}
	rows, err := s.queries.ListJoinRequests(ctx, group.ID)
	if err != nil {
		return RequestsResult{}, fmt.Errorf("查询入群申请失败: %w", err)
	}
	requests := make([]JoinRequest, 0, len(rows))
	for _, row := range rows {
		requests = append(requests, JoinRequest{
			ID: row.ID.Bytes, User: &RequestUser{Name: row.UserName, Handle: row.UserHandle},
			Message: stringValue(row.Message), Status: string(row.Status), CreatedAt: row.CreatedAt.Time.UTC(),
		})
	}
	return RequestsResult{Requests: requests}, nil
}

// ReviewJoinRequest 审批入群申请。approve 为 true 时通过并加入成员，
// 否则拒绝。两者都要求操作者是 owner/admin。
func (s *Service) ReviewJoinRequest(ctx context.Context, actorID uuid.UUID, slug string, requestID uuid.UUID, approve bool) (JoinRequest, error) {
	status := db.ReportStatusRejected
	if approve {
		status = db.ReportStatusResolved
	}
	var updated db.JoinRequest
	var applicant db.GetJoinRequestByIDRow
	err := s.withTx(ctx, func(q Querier) error {
		group, actorRole, err := loadGroupForActor(ctx, q, actorID, slug)
		if err != nil {
			return err
		}
		if !CanManageGroup(actorRole) {
			return ErrForbidden
		}
		request, err := q.GetJoinRequestByID(ctx, db.GetJoinRequestByIDParams{ID: uuidParam(requestID), GroupID: group.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRequestNotFound
		}
		if err != nil {
			return fmt.Errorf("查询入群申请失败: %w", err)
		}
		record, err := q.UpdateJoinRequestStatus(ctx, db.UpdateJoinRequestStatusParams{
			ID: uuidParam(requestID), GroupID: group.ID, Status: status, ReviewedBy: uuidParam(actorID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// 已被其他管理员处理过（status 不再是 pending）。
			return ErrRequestNotFound
		}
		if err != nil {
			return fmt.Errorf("更新入群申请失败: %w", err)
		}
		if approve {
			if _, err := q.AddGroupMember(ctx, db.AddGroupMemberParams{
				GroupID: group.ID, UserID: request.UserID, Role: db.GroupRoleMember,
			}); err != nil {
				return fmt.Errorf("加入群组失败: %w", err)
			}
			if _, err := q.RecomputeGroupMemberCount(ctx, group.ID); err != nil {
				return fmt.Errorf("重算成员数失败: %w", err)
			}
		}
		updated, applicant = record, request
		return nil
	})
	if err != nil {
		return JoinRequest{}, err
	}
	return JoinRequest{
		ID: updated.ID.Bytes, User: &RequestUser{Name: applicant.UserName, Handle: applicant.UserHandle},
		Message: stringValue(updated.Message), Status: string(updated.Status), CreatedAt: updated.CreatedAt.Time.UTC(),
	}, nil
}

// ---------------------------------------------------------------------------
// 群内动态
// ---------------------------------------------------------------------------

// Feed 返回群内动态，仅成员可读。
func (s *Service) Feed(ctx context.Context, userID uuid.UUID, slug, rawCursor string, limit int32) (FeedResult, error) {
	limit = normalizeLimit(limit)
	group, actorRole, err := loadGroupForActor(ctx, s.queries, userID, slug)
	if err != nil {
		return FeedResult{}, err
	}
	if actorRole == "" {
		return FeedResult{}, ErrForbidden
	}
	createdAt, id, err := decodeCursor(rawCursor, "group-feed")
	if err != nil {
		return FeedResult{}, err
	}
	rows, err := s.queries.ListGroupFeed(ctx, db.ListGroupFeedParams{
		GroupID: group.ID,
		UserID:  uuidParam(userID),
		Column3: timeCursor(createdAt, rawCursor),
		Column4: uuidParam(id),
		Limit:   limit + 1,
	})
	if err != nil {
		return FeedResult{}, fmt.Errorf("查询群内动态失败: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	posts := make([]content.Post, 0, len(rows))
	for _, row := range rows {
		// 列顺序与 GetPostByIDRow 完全一致，直接转换后复用同一份投影逻辑。
		post, err := content.PostFromRow(db.GetPostByIDRow(row))
		if err != nil {
			return FeedResult{}, err
		}
		posts = append(posts, post)
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor("group-feed", last.CreatedAt.Time, last.ID.Bytes)
		if err != nil {
			return FeedResult{}, err
		}
		next = &encoded
	}
	return FeedResult{Posts: posts, NextCursor: next}, nil
}

// ---------------------------------------------------------------------------
// 内部辅助
// ---------------------------------------------------------------------------

// loadGroupForActor 读取群组并返回当前用户在群内的角色（非成员为空）。
func loadGroupForActor(ctx context.Context, q Querier, userID uuid.UUID, slug string) (db.GetGroupBySlugRow, Role, error) {
	group, err := q.GetGroupBySlug(ctx, db.GetGroupBySlugParams{Slug: &slug, UserID: uuidParam(userID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetGroupBySlugRow{}, "", ErrGroupNotFound
	}
	if err != nil {
		return db.GetGroupBySlugRow{}, "", fmt.Errorf("查询群组失败: %w", err)
	}
	role := Role("")
	if group.MyRole.Valid {
		role = Role(group.MyRole.GroupRole)
	}
	return group, role, nil
}

func validateCreate(input CreateGroupInput) (name, description, policy string, err error) {
	name = strings.TrimSpace(input.Name)
	description = strings.TrimSpace(input.Description)
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		return "", "", "", fmt.Errorf("%w: 群组名称长度须为 1 到 64 个字符", ErrValidation)
	}
	if utf8.RuneCountInString(description) > 500 {
		return "", "", "", fmt.Errorf("%w: 群组简介不能超过 500 个字符", ErrValidation)
	}
	parsed, ok := parseJoinPolicy(input.JoinPolicy)
	if !ok {
		return "", "", "", fmt.Errorf("%w: joinPolicy 只能是 public、request 或 invite", ErrValidation)
	}
	return name, description, string(parsed), nil
}

func parseJoinPolicy(raw string) (db.GroupJoinPolicy, bool) {
	switch db.GroupJoinPolicy(raw) {
	case db.GroupJoinPolicyPublic, db.GroupJoinPolicyRequest, db.GroupJoinPolicyInvite:
		return db.GroupJoinPolicy(raw), true
	}
	return "", false
}

// inviteUsable 是邀请码有效性判定：未过期且未用尽。
// 抽成纯函数以便在没有数据库的单元测试里覆盖边界。
func inviteUsable(invite db.GroupInvite, now time.Time) bool {
	if invite.ExpiresAt.Valid && !now.Before(invite.ExpiresAt.Time) {
		return false
	}
	if invite.MaxUses != nil && invite.UsedCount >= *invite.MaxUses {
		return false
	}
	return true
}

func groupFromRow(row db.GetGroupBySlugRow) Group {
	group := Group{
		ID:          row.ID.Bytes,
		Name:        row.Name,
		Slug:        stringValue(row.Slug),
		Description: stringValue(row.Description),
		CoverURL:    nil,
		OwnerID:     row.OwnerID.Bytes,
		OwnerName:   row.OwnerName,
		JoinPolicy:  string(row.JoinPolicy),
		MemberCount: row.MemberCount,
		CreatedAt:   row.CreatedAt.Time.UTC(),
	}
	if row.MyRole.Valid {
		role := string(row.MyRole.GroupRole)
		group.MyRole = &role
	}
	return group
}

func memberFromRow(row db.ListGroupMembersRow) Member {
	return Member{
		UserID: row.UserID.Bytes, Name: row.Name, Handle: row.Handle,
		Role: string(row.Role), JoinedAt: row.JoinedAt.Time.UTC(), VerifyType: string(row.VerifyType),
	}
}

func inviteFromDB(record db.GroupInvite) Invite {
	invite := Invite{Code: record.Code, MaxUses: record.MaxUses}
	if record.ExpiresAt.Valid {
		expires := record.ExpiresAt.Time.UTC()
		invite.ExpiresAt = &expires
	}
	return invite
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
