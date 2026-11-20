package group

import (
	"errors"
	"testing"
)

func TestAtLeast(t *testing.T) {
	tests := []struct {
		role     Role
		required Role
		want     bool
	}{
		{RoleOwner, RoleOwner, true},
		{RoleOwner, RoleAdmin, true},
		{RoleOwner, RoleMember, true},
		{RoleAdmin, RoleOwner, false},
		{RoleAdmin, RoleAdmin, true},
		{RoleAdmin, RoleMember, true},
		{RoleMember, RoleAdmin, false},
		{RoleMember, RoleMember, true},
		{"", RoleMember, false},
	}
	for _, tt := range tests {
		if got := AtLeast(tt.role, tt.required); got != tt.want {
			t.Fatalf("AtLeast(%q, %q) = %v，期望 %v", tt.role, tt.required, got, tt.want)
		}
	}
}

func TestCanManageGroup(t *testing.T) {
	if !CanManageGroup(RoleOwner) || !CanManageGroup(RoleAdmin) {
		t.Fatal("owner 与 admin 应能管理群组")
	}
	if CanManageGroup(RoleMember) || CanManageGroup("") {
		t.Fatal("member 与非成员不应能管理群组")
	}
}

func TestCanChangeRoleTo(t *testing.T) {
	tests := []struct {
		name    string
		actor   Role
		target  Role
		newRole Role
		same    bool
		wantErr error
	}{
		{"owner 提升 member 为 admin", RoleOwner, RoleMember, RoleAdmin, false, nil},
		{"owner 降级 admin 为 member", RoleOwner, RoleAdmin, RoleMember, false, nil},
		{"admin 提升 member 为 admin", RoleAdmin, RoleMember, RoleAdmin, false, nil},
		{"admin 不能降级另一个 admin", RoleAdmin, RoleAdmin, RoleMember, false, ErrForbidden},
		{"admin 不能修改 owner（越权，403）", RoleAdmin, RoleOwner, RoleMember, false, ErrForbidden},
		{"member 不能修改 owner（越权，403）", RoleMember, RoleOwner, RoleMember, false, ErrForbidden},
		{"owner 不能修改 owner（即使本人）", RoleOwner, RoleOwner, RoleMember, true, ErrOwnerImmutable},
		{"member 不能修改他人", RoleMember, RoleMember, RoleAdmin, false, ErrForbidden},
		{"非成员不能修改他人", "", RoleMember, RoleAdmin, false, ErrForbidden},
		{"新角色不能是 owner", RoleOwner, RoleMember, RoleOwner, false, ErrValidation},
		{"新角色非法", RoleOwner, RoleMember, Role("super"), false, ErrValidation},
		{"admin 可以把自己降级", RoleAdmin, RoleAdmin, RoleMember, true, nil},
		{"目标不是成员", RoleOwner, "", RoleAdmin, false, ErrMemberNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanChangeRoleTo(tt.actor, tt.target, tt.newRole, tt.same)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v，期望 %v", err, tt.wantErr)
			}
		})
	}
}

func TestCanRemoveMember(t *testing.T) {
	tests := []struct {
		name    string
		actor   Role
		target  Role
		same    bool
		wantErr error
	}{
		{"owner 移除 member", RoleOwner, RoleMember, false, nil},
		{"owner 移除 admin", RoleOwner, RoleAdmin, false, nil},
		{"admin 移除 member", RoleAdmin, RoleMember, false, nil},
		{"admin 不能移除另一个 admin", RoleAdmin, RoleAdmin, false, ErrForbidden},
		{"admin 不能移除 owner（越权，403）", RoleAdmin, RoleOwner, false, ErrForbidden},
		{"member 不能移除他人", RoleMember, RoleMember, false, ErrForbidden},
		{"member 本人退出", RoleMember, RoleMember, true, nil},
		{"admin 本人退出", RoleAdmin, RoleAdmin, true, nil},
		{"owner 不能退出", RoleOwner, RoleOwner, true, ErrOwnerImmutable},
		{"非成员退出", "", "", true, ErrMemberNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanRemoveMember(tt.actor, tt.target, tt.same)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v，期望 %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseRole(t *testing.T) {
	for _, raw := range []string{"owner", "admin", "member"} {
		if role, ok := ParseRole(raw); !ok || string(role) != raw {
			t.Fatalf("ParseRole(%q) = %q, %v", raw, role, ok)
		}
	}
	for _, raw := range []string{"", "Owner", "super", "member "} {
		if _, ok := ParseRole(raw); ok {
			t.Fatalf("ParseRole(%q) 应失败", raw)
		}
	}
}
