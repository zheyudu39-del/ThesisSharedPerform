package curation

import (
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/db"
)

// Curator 是策展人身份的最小投影。
//
// 单独定义而非直接传 identity.User，是为了让权限判定成为不依赖
// 生成代码、也不依赖 identity 包的纯函数，单元测试无需构造数据库类型。
type Curator struct {
	ID         uuid.UUID
	Name       string
	Handle     string
	Role       string
	VerifyType string
}

// 与 users.role / users.verify_type 枚举保持一致。
const (
	RoleAdmin            = "admin"
	VerifyTypeInfluencer = "influencer"
)

// CanCurate 判断该账号是否具备策展资格。
//
// PRD §3 权限表：精选流准入仅限运营（role='admin'）与认证大V
// （verify_type='influencer'）。这里只做资格判定，不涉及具体某个集合
// 的编辑权限——后者见 CanEdit。
func CanCurate(user Curator) bool {
	return user.Role == RoleAdmin || user.VerifyType == VerifyTypeInfluencer
}

// CanEdit 判断该账号能否编辑某个策展集。
//
// 规则：策展人本人或平台运营。大V 只能编辑自己的集合，
// 运营可以编辑任何人的集合（用于下架不当内容）。
func CanEdit(user Curator, curatorID uuid.UUID) bool {
	if user.ID != uuid.Nil && user.ID == curatorID {
		return true
	}
	return user.Role == RoleAdmin
}

// VerifyTypeFromDB 把 sqlc 枚举转换为字符串，便于与 Curator 比较。
func VerifyTypeFromDB(v db.VerifyType) string { return string(v) }

// RoleFromDB 把 sqlc 枚举转换为字符串。
func RoleFromDB(r db.UserRole) string { return string(r) }
