package curation

import (
	"testing"

	"github.com/google/uuid"
)

// TestCanCurate 验证策展资格：只有运营与认证大V 通过。
//
// 这条规则直接对应 PRD §3 权限表，是精选流不被滥用的第一道闸门；
// 任何放宽都会让普通用户把自我推广内容塞进精选流。
func TestCanCurate(t *testing.T) {
	tests := []struct {
		name string
		user Curator
		want bool
	}{
		{"运营", Curator{Role: RoleAdmin, VerifyType: "none"}, true},
		{"认证大V", Curator{Role: "user", VerifyType: VerifyTypeInfluencer}, true},
		{"既是运营又是大V", Curator{Role: RoleAdmin, VerifyType: VerifyTypeInfluencer}, true},
		{"普通用户", Curator{Role: "user", VerifyType: "none"}, false},
		{"机构认证但不是大V", Curator{Role: "user", VerifyType: "institution"}, false},
		{"空身份", Curator{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanCurate(tt.user); got != tt.want {
				t.Fatalf("CanCurate(%+v) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}

// TestCanEdit 验证集合编辑权限：策展人本人或运营。
//
// 关键区分：大V 只能改自己的集合，运营可以改任何人的。
// 如果写反，任何大V 都能篡改他人的精选栏目。
func TestCanEdit(t *testing.T) {
	owner := uuid.MustParse("0193a000-0000-7000-8000-000000000001")
	other := uuid.MustParse("0193a000-0000-7000-8000-000000000002")

	tests := []struct {
		name      string
		user      Curator
		curatorID uuid.UUID
		want      bool
	}{
		{"策展人本人", Curator{ID: owner, Role: "user", VerifyType: VerifyTypeInfluencer}, owner, true},
		{"运营改他人集合", Curator{ID: other, Role: RoleAdmin}, owner, true},
		{"其他大V 改别人集合", Curator{ID: other, Role: "user", VerifyType: VerifyTypeInfluencer}, owner, false},
		{"普通用户", Curator{ID: other, Role: "user"}, owner, false},
		{"匿名", Curator{}, owner, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanEdit(tt.user, tt.curatorID); got != tt.want {
				t.Fatalf("CanEdit(%+v, %s) = %v, want %v", tt.user, tt.curatorID, got, tt.want)
			}
		})
	}
}
