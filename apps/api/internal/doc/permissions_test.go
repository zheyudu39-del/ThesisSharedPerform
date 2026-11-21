package doc

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseRole(t *testing.T) {
	for _, raw := range []string{"owner", "editor", "viewer"} {
		if role, ok := ParseRole(raw); !ok || string(role) != raw {
			t.Fatalf("ParseRole(%q) = %q, %v", raw, role, ok)
		}
	}
	for _, raw := range []string{"", "Owner", "admin", "editor "} {
		if _, ok := ParseRole(raw); ok {
			t.Fatalf("ParseRole(%q) 应失败", raw)
		}
	}
}

func TestParseAssignableRole(t *testing.T) {
	for _, raw := range []string{"editor", "viewer"} {
		if role, ok := ParseAssignableRole(raw); !ok || string(role) != raw {
			t.Fatalf("ParseAssignableRole(%q) = %q, %v", raw, role, ok)
		}
	}
	// owner 由创建者与群主隐含，不能通过成员接口授予。
	for _, raw := range []string{"owner", "", "admin"} {
		if _, ok := ParseAssignableRole(raw); ok {
			t.Fatalf("ParseAssignableRole(%q) 应失败", raw)
		}
	}
}

func TestAtLeast(t *testing.T) {
	tests := []struct {
		role     Role
		required Role
		want     bool
	}{
		{RoleOwner, RoleOwner, true},
		{RoleOwner, RoleEditor, true},
		{RoleOwner, RoleViewer, true},
		{RoleEditor, RoleOwner, false},
		{RoleEditor, RoleEditor, true},
		{RoleEditor, RoleViewer, true},
		{RoleViewer, RoleEditor, false},
		{RoleViewer, RoleViewer, true},
		{"", RoleViewer, false},
	}
	for _, tt := range tests {
		if got := AtLeast(tt.role, tt.required); got != tt.want {
			t.Fatalf("AtLeast(%q, %q) = %v，期望 %v", tt.role, tt.required, got, tt.want)
		}
	}
}

// TestEffectiveRole 覆盖权限矩阵：身份（匿名/创建者/群主/群成员/群外）、
// 文档级覆盖与 default_role 的优先级。
func TestEffectiveRole(t *testing.T) {
	creator := uuid.New()
	groupOwner := uuid.New()
	member := uuid.New()
	outsider := uuid.New()
	anonymous := uuid.Nil

	base := AccessFacts{
		GroupID:      uuid.New(),
		CreatedBy:    creator,
		GroupOwnerID: groupOwner,
		DefaultRole:  RoleEditor,
	}

	tests := []struct {
		name  string
		facts AccessFacts
		want  Role
	}{
		{
			name:  "匿名无角色",
			facts: withUser(base, anonymous),
			want:  "",
		},
		{
			name:  "创建者恒为 owner",
			facts: withUser(base, creator),
			want:  RoleOwner,
		},
		{
			name:  "群主恒为 owner（即使不是创建者）",
			facts: withUser(base, groupOwner),
			want:  RoleOwner,
		},
		{
			name:  "群成员按 default_role 获得 editor",
			facts: withUser(base, member, "member"),
			want:  RoleEditor,
		},
		{
			name:  "群成员按 default_role=viewer",
			facts: withUser(base, member, "member", RoleViewer),
			want:  RoleViewer,
		},
		{
			name:  "doc_members 覆盖把 editor 降为 viewer",
			facts: withUser(base, member, "member", RoleEditor, RoleViewer),
			want:  RoleViewer,
		},
		{
			name:  "doc_members 覆盖把 viewer 提升为 editor",
			facts: withUser(base, member, "member", RoleViewer, RoleEditor),
			want:  RoleEditor,
		},
		{
			name:  "群外用户即使有 doc_members 覆盖也无权限",
			facts: withUser(base, outsider, "", RoleEditor),
			want:  "",
		},
		{
			name:  "非成员无角色",
			facts: withUser(base, outsider),
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveRole(tt.facts); got != tt.want {
				t.Fatalf("EffectiveRole = %q，期望 %q", got, tt.want)
			}
		})
	}
}

// withUser 构造事实：可选参数依次为 groupRole、defaultRole、docRole。
func withUser(base AccessFacts, user uuid.UUID, opts ...any) AccessFacts {
	facts := base
	facts.UserID = user
	for i, opt := range opts {
		switch i {
		case 0:
			facts.GroupRole = string(opt.(string))
		case 1:
			facts.DefaultRole = opt.(Role)
		case 2:
			facts.DocRole = opt.(Role)
		}
	}
	return facts
}

func TestCapabilityMatrix(t *testing.T) {
	tests := []struct {
		role      Role
		canView   bool
		canEdit   bool
		canManage bool
		canDelete bool
	}{
		{RoleOwner, true, true, true, true},
		{RoleEditor, true, true, false, false},
		{RoleViewer, true, false, false, false},
		{"", false, false, false, false},
	}
	for _, tt := range tests {
		if got := CanView(tt.role); got != tt.canView {
			t.Fatalf("CanView(%q) = %v", tt.role, got)
		}
		if got := CanEdit(tt.role); got != tt.canEdit {
			t.Fatalf("CanEdit(%q) = %v", tt.role, got)
		}
		if got := CanManageMembers(tt.role); got != tt.canManage {
			t.Fatalf("CanManageMembers(%q) = %v", tt.role, got)
		}
		if got := CanDelete(tt.role); got != tt.canDelete {
			t.Fatalf("CanDelete(%q) = %v", tt.role, got)
		}
	}
}

func TestCanModifyAnnotation(t *testing.T) {
	tests := []struct {
		name     string
		role     Role
		isAuthor bool
		want     bool
	}{
		{"owner 可处理他人批注", RoleOwner, false, true},
		{"owner 可处理自己的批注", RoleOwner, true, true},
		{"editor 可处理自己的批注", RoleEditor, true, true},
		{"editor 不能处理他人批注", RoleEditor, false, false},
		{"viewer 不能处理自己的批注", RoleViewer, true, false},
		{"非成员不能处理批注", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanModifyAnnotation(tt.role, tt.isAuthor); got != tt.want {
				t.Fatalf("CanModifyAnnotation = %v，期望 %v", got, tt.want)
			}
		})
	}
}
