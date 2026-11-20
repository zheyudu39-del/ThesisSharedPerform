package group

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// fakeQuerier 是一个只覆盖服务层关键路径的内存替身。
// 它刻意保持「同一份状态」：成员表与群行共享，因此可以断言
// member_count 是否与 group_members 实际行数一致。
type fakeQuerier struct {
	groups    map[uuid.UUID]*db.Group
	members   map[uuid.UUID]map[uuid.UUID]db.GroupRole
	users     map[uuid.UUID]struct{ name, handle string }
	invites   map[string]*db.GroupInvite
	requests  map[uuid.UUID]*db.JoinRequest
	slugIndex map[string]uuid.UUID
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		groups:    make(map[uuid.UUID]*db.Group),
		members:   make(map[uuid.UUID]map[uuid.UUID]db.GroupRole),
		users:     make(map[uuid.UUID]struct{ name, handle string }),
		invites:   make(map[string]*db.GroupInvite),
		requests:  make(map[uuid.UUID]*db.JoinRequest),
		slugIndex: make(map[string]uuid.UUID),
	}
}

func (f *fakeQuerier) addUser(name, handle string) uuid.UUID {
	id := uuid.New()
	f.users[id] = struct{ name, handle string }{name, handle}
	return id
}

func (f *fakeQuerier) CreateGroup(_ context.Context, arg db.CreateGroupParams) (db.Group, error) {
	group := db.Group{
		ID: uuidParam(uuid.New()), Name: arg.Name, Slug: arg.Slug, Description: arg.Description,
		OwnerID: arg.OwnerID, JoinPolicy: arg.JoinPolicy, MemberCount: 1,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	f.groups[group.ID.Bytes] = &group
	f.members[group.ID.Bytes] = make(map[uuid.UUID]db.GroupRole)
	if arg.Slug != nil {
		f.slugIndex[*arg.Slug] = group.ID.Bytes
	}
	return group, nil
}

func (f *fakeQuerier) GroupSlugExists(_ context.Context, slug *string) (bool, error) {
	if slug == nil {
		return false, nil
	}
	_, ok := f.slugIndex[*slug]
	return ok, nil
}

func (f *fakeQuerier) groupRow(id uuid.UUID, viewer uuid.UUID) db.GetGroupBySlugRow {
	group := f.groups[id]
	row := db.GetGroupBySlugRow{
		ID: group.ID, Name: group.Name, Slug: group.Slug, Description: group.Description,
		OwnerID: group.OwnerID, JoinPolicy: group.JoinPolicy, MemberCount: group.MemberCount,
		CreatedAt: group.CreatedAt, OwnerName: "owner",
	}
	if role, ok := f.members[id][viewer]; ok {
		row.MyRole = db.NullGroupRole{GroupRole: role, Valid: true}
	}
	return row
}

func (f *fakeQuerier) GetGroupBySlug(_ context.Context, arg db.GetGroupBySlugParams) (db.GetGroupBySlugRow, error) {
	if arg.Slug == nil {
		return db.GetGroupBySlugRow{}, pgx.ErrNoRows
	}
	id, ok := f.slugIndex[*arg.Slug]
	if !ok {
		return db.GetGroupBySlugRow{}, pgx.ErrNoRows
	}
	return f.groupRow(id, arg.UserID.Bytes), nil
}

func (f *fakeQuerier) GetGroupByID(_ context.Context, arg db.GetGroupByIDParams) (db.GetGroupByIDRow, error) {
	row, ok := f.groups[arg.ID.Bytes]
	if !ok {
		return db.GetGroupByIDRow{}, pgx.ErrNoRows
	}
	result := db.GetGroupByIDRow{
		ID: row.ID, Name: row.Name, Slug: row.Slug, Description: row.Description,
		OwnerID: row.OwnerID, JoinPolicy: row.JoinPolicy, MemberCount: row.MemberCount,
		CreatedAt: row.CreatedAt, OwnerName: "owner",
	}
	if role, ok := f.members[arg.ID.Bytes][arg.UserID.Bytes]; ok {
		result.MyRole = db.NullGroupRole{GroupRole: role, Valid: true}
	}
	return result, nil
}

func (f *fakeQuerier) UpdateGroup(_ context.Context, arg db.UpdateGroupParams) (db.Group, error) {
	group, ok := f.groups[arg.ID.Bytes]
	if !ok {
		return db.Group{}, pgx.ErrNoRows
	}
	if arg.Name != nil {
		group.Name = *arg.Name
	}
	if arg.Description != nil {
		group.Description = arg.Description
	}
	if arg.JoinPolicy.Valid {
		group.JoinPolicy = arg.JoinPolicy.GroupJoinPolicy
	}
	return *group, nil
}

func (f *fakeQuerier) ListPublicGroups(_ context.Context, arg db.ListPublicGroupsParams) ([]db.ListPublicGroupsRow, error) {
	rows := make([]db.ListPublicGroupsRow, 0)
	for id, group := range f.groups {
		if group.JoinPolicy != db.GroupJoinPolicyPublic {
			continue
		}
		rows = append(rows, db.ListPublicGroupsRow(f.groupRow(id, arg.UserID.Bytes)))
	}
	return rows, nil
}

func (f *fakeQuerier) AddGroupMember(_ context.Context, arg db.AddGroupMemberParams) (int64, error) {
	group := f.members[arg.GroupID.Bytes]
	if _, exists := group[arg.UserID.Bytes]; exists {
		return 0, nil
	}
	group[arg.UserID.Bytes] = arg.Role
	return 1, nil
}

func (f *fakeQuerier) GetGroupMember(_ context.Context, arg db.GetGroupMemberParams) (db.GroupMember, error) {
	role, ok := f.members[arg.GroupID.Bytes][arg.UserID.Bytes]
	if !ok {
		return db.GroupMember{}, pgx.ErrNoRows
	}
	return db.GroupMember{GroupID: arg.GroupID, UserID: arg.UserID, Role: role, JoinedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}, nil
}

func (f *fakeQuerier) GetGroupMemberView(_ context.Context, arg db.GetGroupMemberViewParams) (db.GetGroupMemberViewRow, error) {
	role, ok := f.members[arg.GroupID.Bytes][arg.UserID.Bytes]
	if !ok {
		return db.GetGroupMemberViewRow{}, pgx.ErrNoRows
	}
	user := f.users[arg.UserID.Bytes]
	return db.GetGroupMemberViewRow{
		GroupID: arg.GroupID, UserID: arg.UserID, Role: role,
		JoinedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		Name:     user.name, Handle: user.handle,
	}, nil
}

func (f *fakeQuerier) ListGroupMembers(_ context.Context, arg db.ListGroupMembersParams) ([]db.ListGroupMembersRow, error) {
	rows := make([]db.ListGroupMembersRow, 0)
	for userID, role := range f.members[arg.GroupID.Bytes] {
		user := f.users[userID]
		rows = append(rows, db.ListGroupMembersRow{
			GroupID: arg.GroupID, UserID: uuidParam(userID), Role: role,
			JoinedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			Name:     user.name, Handle: user.handle,
		})
	}
	return rows, nil
}

func (f *fakeQuerier) UpdateGroupMemberRole(_ context.Context, arg db.UpdateGroupMemberRoleParams) (db.GroupMember, error) {
	group, ok := f.members[arg.GroupID.Bytes]
	if !ok {
		return db.GroupMember{}, pgx.ErrNoRows
	}
	if _, exists := group[arg.UserID.Bytes]; !exists {
		return db.GroupMember{}, pgx.ErrNoRows
	}
	group[arg.UserID.Bytes] = arg.Role
	return db.GroupMember{GroupID: arg.GroupID, UserID: arg.UserID, Role: arg.Role}, nil
}

func (f *fakeQuerier) RemoveGroupMember(_ context.Context, arg db.RemoveGroupMemberParams) (int64, error) {
	group := f.members[arg.GroupID.Bytes]
	if _, exists := group[arg.UserID.Bytes]; !exists {
		return 0, nil
	}
	delete(group, arg.UserID.Bytes)
	return 1, nil
}

func (f *fakeQuerier) RecomputeGroupMemberCount(_ context.Context, groupID pgtype.UUID) (int32, error) {
	group, ok := f.groups[groupID.Bytes]
	if !ok {
		return 0, pgx.ErrNoRows
	}
	group.MemberCount = int32(len(f.members[groupID.Bytes]))
	return group.MemberCount, nil
}

func (f *fakeQuerier) CreateGroupInvite(_ context.Context, arg db.CreateGroupInviteParams) (db.GroupInvite, error) {
	invite := db.GroupInvite{
		ID: uuidParam(uuid.New()), GroupID: arg.GroupID, Code: arg.Code, CreatedBy: arg.CreatedBy,
		ExpiresAt: arg.ExpiresAt, MaxUses: arg.MaxUses,
	}
	f.invites[arg.Code] = &invite
	return invite, nil
}

func (f *fakeQuerier) GetGroupInviteByCode(_ context.Context, code string) (db.GroupInvite, error) {
	invite, ok := f.invites[code]
	if !ok {
		return db.GroupInvite{}, pgx.ErrNoRows
	}
	return *invite, nil
}

func (f *fakeQuerier) ConsumeGroupInvite(_ context.Context, id pgtype.UUID) (db.GroupInvite, error) {
	for _, invite := range f.invites {
		if invite.ID.Bytes != id.Bytes {
			continue
		}
		if invite.MaxUses != nil && invite.UsedCount >= *invite.MaxUses {
			return db.GroupInvite{}, pgx.ErrNoRows
		}
		invite.UsedCount++
		return *invite, nil
	}
	return db.GroupInvite{}, pgx.ErrNoRows
}

func (f *fakeQuerier) GetJoinRequestByUser(_ context.Context, arg db.GetJoinRequestByUserParams) (db.JoinRequest, error) {
	for _, request := range f.requests {
		if request.GroupID.Bytes == arg.GroupID.Bytes && request.UserID.Bytes == arg.UserID.Bytes {
			return *request, nil
		}
	}
	return db.JoinRequest{}, pgx.ErrNoRows
}

func (f *fakeQuerier) CreateJoinRequest(_ context.Context, arg db.CreateJoinRequestParams) (db.JoinRequest, error) {
	for id, request := range f.requests {
		if request.GroupID.Bytes == arg.GroupID.Bytes && request.UserID.Bytes == arg.UserID.Bytes {
			request.Message, request.Status = arg.Message, db.ReportStatusPending
			request.CreatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			_ = id
			return *request, nil
		}
	}
	request := &db.JoinRequest{
		ID: uuidParam(uuid.New()), GroupID: arg.GroupID, UserID: arg.UserID,
		Message: arg.Message, Status: db.ReportStatusPending,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	f.requests[request.ID.Bytes] = request
	return *request, nil
}

func (f *fakeQuerier) GetJoinRequestByID(_ context.Context, arg db.GetJoinRequestByIDParams) (db.GetJoinRequestByIDRow, error) {
	request, ok := f.requests[arg.ID.Bytes]
	if !ok || request.GroupID.Bytes != arg.GroupID.Bytes {
		return db.GetJoinRequestByIDRow{}, pgx.ErrNoRows
	}
	user := f.users[request.UserID.Bytes]
	return db.GetJoinRequestByIDRow{
		ID: request.ID, GroupID: request.GroupID, UserID: request.UserID, Message: request.Message,
		Status: request.Status, CreatedAt: request.CreatedAt, UserName: user.name, UserHandle: user.handle,
	}, nil
}

func (f *fakeQuerier) ListJoinRequests(_ context.Context, groupID pgtype.UUID) ([]db.ListJoinRequestsRow, error) {
	rows := make([]db.ListJoinRequestsRow, 0)
	for _, request := range f.requests {
		if request.GroupID.Bytes != groupID.Bytes || request.Status != db.ReportStatusPending {
			continue
		}
		user := f.users[request.UserID.Bytes]
		rows = append(rows, db.ListJoinRequestsRow{
			ID: request.ID, GroupID: request.GroupID, UserID: request.UserID, Message: request.Message,
			Status: request.Status, CreatedAt: request.CreatedAt, UserName: user.name, UserHandle: user.handle,
		})
	}
	return rows, nil
}

func (f *fakeQuerier) UpdateJoinRequestStatus(_ context.Context, arg db.UpdateJoinRequestStatusParams) (db.JoinRequest, error) {
	request, ok := f.requests[arg.ID.Bytes]
	if !ok || request.GroupID.Bytes != arg.GroupID.Bytes || request.Status != db.ReportStatusPending {
		return db.JoinRequest{}, pgx.ErrNoRows
	}
	request.Status = arg.Status
	return *request, nil
}

func (f *fakeQuerier) ListGroupFeed(context.Context, db.ListGroupFeedParams) ([]db.ListGroupFeedRow, error) {
	return nil, nil
}

// memberCountMatches 断言冗余计数与实际成员行数一致。
func (f *fakeQuerier) memberCountMatches(t *testing.T, groupID uuid.UUID) {
	t.Helper()
	if got, want := f.groups[groupID].MemberCount, int32(len(f.members[groupID])); got != want {
		t.Fatalf("member_count = %d，实际成员行数 = %d", got, want)
	}
}

func TestCreateGroupSeedsOwnerAndCount(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	service := newServiceWithQuerier(querier)

	created, err := service.CreateGroup(context.Background(), owner, CreateGroupInput{
		Name: "机器学习讨论组", JoinPolicy: "public",
	})
	if err != nil {
		t.Fatalf("建群失败: %v", err)
	}
	if created.Slug == "" {
		t.Fatal("建群后 slug 不能为空（中文名应退化为随机 slug）")
	}
	if created.MyRole == nil || *created.MyRole != string(RoleOwner) {
		t.Fatalf("建群者的 myRole = %v，期望 owner", created.MyRole)
	}
	querier.memberCountMatches(t, created.ID)
	if role := querier.members[created.ID][owner]; role != db.GroupRoleOwner {
		t.Fatalf("群主成员行角色 = %q", role)
	}
}

func TestCreateGroupSlugConflictGetsSuffix(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	service := newServiceWithQuerier(querier)

	first, err := service.CreateGroup(context.Background(), owner, CreateGroupInput{Name: "Lab", JoinPolicy: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateGroup(context.Background(), owner, CreateGroupInput{Name: "Lab", JoinPolicy: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Slug == second.Slug {
		t.Fatalf("同名群组 slug 冲突: %q", first.Slug)
	}
	if second.Slug == "lab" {
		t.Fatalf("冲突时应追加后缀，得到 %q", second.Slug)
	}
}

func TestJoinPublicFlow(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	joiner := querier.addUser("成员", "joiner")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Open Group", JoinPolicy: "public"})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := service.JoinPublic(ctx, joiner, created.Slug)
	if err != nil {
		t.Fatalf("加入公开群失败: %v", err)
	}
	if joined.MemberCount != 2 {
		t.Fatalf("memberCount = %d，期望 2", joined.MemberCount)
	}
	querier.memberCountMatches(t, created.ID)

	if _, err := service.JoinPublic(ctx, joiner, created.Slug); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("重复加入 err = %v，期望 ErrAlreadyMember", err)
	}
}

func TestJoinPublicRejectsNonPublic(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	joiner := querier.addUser("成员", "joiner")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Invite Only", JoinPolicy: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.JoinPublic(ctx, joiner, created.Slug); !errors.Is(err, ErrJoinPolicy) {
		t.Fatalf("err = %v，期望 ErrJoinPolicy", err)
	}
}

func TestJoinByInviteRespectsMaxUses(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	first := querier.addUser("甲", "first")
	second := querier.addUser("乙", "second")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Invite Group", JoinPolicy: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	maxUses := int32(1)
	invite, err := service.CreateInvite(ctx, owner, created.Slug, CreateInviteInput{MaxUses: &maxUses})
	if err != nil {
		t.Fatalf("创建邀请码失败: %v", err)
	}
	if _, err := service.JoinByInvite(ctx, first, invite.Code); err != nil {
		t.Fatalf("首次使用邀请码失败: %v", err)
	}
	if _, err := service.JoinByInvite(ctx, second, invite.Code); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("用尽后 err = %v，期望 ErrInviteNotFound", err)
	}
	querier.memberCountMatches(t, created.ID)
}

func TestJoinByInviteRejectsExpired(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	joiner := querier.addUser("成员", "joiner")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Expired Group", JoinPolicy: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	// 直接注入一个已过期的邀请码。
	querier.invites["expiredcode12345"] = &db.GroupInvite{
		ID: uuidParam(uuid.New()), GroupID: uuidParam(created.ID), Code: "expiredcode12345",
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	}
	if _, err := service.JoinByInvite(ctx, joiner, "expiredcode12345"); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("过期邀请码 err = %v，期望 ErrInviteNotFound", err)
	}
	if _, err := service.JoinByInvite(ctx, joiner, "does-not-exist"); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("不存在邀请码 err = %v，期望 ErrInviteNotFound", err)
	}
}

func TestOwnerCannotLeaveOrBeDemoted(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Owned", JoinPolicy: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveMember(ctx, owner, created.Slug, owner); !errors.Is(err, ErrOwnerImmutable) {
		t.Fatalf("群主退出 err = %v，期望 ErrOwnerImmutable", err)
	}
	// 群主不可被改角色；HTTP 层把 ErrOwnerImmutable 映射为 422 validation_error。
	if _, err := service.UpdateMemberRole(ctx, owner, created.Slug, owner, "admin"); !errors.Is(err, ErrOwnerImmutable) {
		t.Fatalf("降级群主 err = %v，期望 ErrOwnerImmutable", err)
	}
	querier.memberCountMatches(t, created.ID)
}

func TestAdminCannotDemoteOtherAdmin(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	adminA := querier.addUser("管理员甲", "admina")
	adminB := querier.addUser("管理员乙", "adminb")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Admins", JoinPolicy: "public"})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []uuid.UUID{adminA, adminB} {
		if _, err := service.JoinPublic(ctx, user, created.Slug); err != nil {
			t.Fatal(err)
		}
		if _, err := service.UpdateMemberRole(ctx, owner, created.Slug, user, "admin"); err != nil {
			t.Fatalf("群主提升管理员失败: %v", err)
		}
	}
	if _, err := service.UpdateMemberRole(ctx, adminA, created.Slug, adminB, "member"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("管理员降级另一管理员 err = %v，期望 ErrForbidden", err)
	}
	if _, err := service.UpdateMemberRole(ctx, owner, created.Slug, adminB, "member"); err != nil {
		t.Fatalf("群主应能降级管理员: %v", err)
	}
}

func TestJoinRequestLifecycle(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	applicant := querier.addUser("申请人", "applicant")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Approval", JoinPolicy: "request"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := service.CreateJoinRequest(ctx, applicant, created.Slug, "我想加入")
	if err != nil {
		t.Fatalf("提交申请失败: %v", err)
	}
	if request.Status != "pending" {
		t.Fatalf("申请状态 = %q，期望 pending", request.Status)
	}
	if _, err := service.CreateJoinRequest(ctx, applicant, created.Slug, "再来一次"); !errors.Is(err, ErrAlreadyRequested) {
		t.Fatalf("重复申请 err = %v，期望 ErrAlreadyRequested", err)
	}
	if _, err := service.ListJoinRequests(ctx, applicant, created.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非管理员查看申请 err = %v，期望 ErrForbidden", err)
	}
	approved, err := service.ReviewJoinRequest(ctx, owner, created.Slug, request.ID, true)
	if err != nil {
		t.Fatalf("审批通过失败: %v", err)
	}
	if approved.Status != "resolved" {
		t.Fatalf("审批后状态 = %q，期望 resolved", approved.Status)
	}
	querier.memberCountMatches(t, created.ID)
	if _, err := service.ReviewJoinRequest(ctx, owner, created.Slug, request.ID, true); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("重复审批 err = %v，期望 ErrRequestNotFound", err)
	}
}

func TestFeedRequiresMembership(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	outsider := querier.addUser("路人", "outsider")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Private Feed", JoinPolicy: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Feed(ctx, outsider, created.Slug, "", 20); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非成员读动态 err = %v，期望 ErrForbidden", err)
	}
	if _, err := service.Feed(ctx, owner, created.Slug, "", 20); err != nil {
		t.Fatalf("成员读动态失败: %v", err)
	}
}

func TestUpdateGroupRequiresManageRole(t *testing.T) {
	querier := newFakeQuerier()
	owner := querier.addUser("群主", "owner")
	member := querier.addUser("成员", "member")
	service := newServiceWithQuerier(querier)
	ctx := context.Background()

	created, err := service.CreateGroup(ctx, owner, CreateGroupInput{Name: "Managed", JoinPolicy: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.JoinPublic(ctx, member, created.Slug); err != nil {
		t.Fatal(err)
	}
	name := "新名字"
	if _, err := service.UpdateGroup(ctx, member, created.Slug, UpdateGroupInput{Name: &name}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("普通成员改群 err = %v，期望 ErrForbidden", err)
	}
	updated, err := service.UpdateGroup(ctx, owner, created.Slug, UpdateGroupInput{Name: &name})
	if err != nil {
		t.Fatalf("群主改群失败: %v", err)
	}
	if updated.Name != name {
		t.Fatalf("群名 = %q，期望 %q", updated.Name, name)
	}
}
