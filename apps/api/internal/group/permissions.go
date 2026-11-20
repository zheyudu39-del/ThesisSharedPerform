package group

import "github.com/thesis-shared/api/internal/db"

// Role 是群内角色，与数据库 group_role 枚举一一对应。
// 单独定义成字符串常量而非直接用 db.GroupRole，是为了让权限判定
// 成为不依赖生成代码的纯函数，单元测试无需构造 sqlc 类型。
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// ParseRole 把外部输入解析为角色，未知值返回 false。
func ParseRole(raw string) (Role, bool) {
	switch Role(raw) {
	case RoleOwner, RoleAdmin, RoleMember:
		return Role(raw), true
	}
	return "", false
}

// RoleFromDB 把 sqlc 的枚举值转换为本包角色。
func RoleFromDB(role db.GroupRole) Role { return Role(role) }

// rank 给出角色优先级：owner > admin > member，未知角色为 0。
func rank(role Role) int {
	switch role {
	case RoleOwner:
		return 3
	case RoleAdmin:
		return 2
	case RoleMember:
		return 1
	default:
		return 0
	}
}

// AtLeast 判断 role 是否达到 required 的层级。
// 非成员（空角色）永远不满足任何要求，包括 RoleMember。
func AtLeast(role, required Role) bool { return rank(role) >= rank(required) }

// CanManageGroup 判断该角色能否修改群资料、审批申请、创建邀请码。
// 仅 owner 与 admin 可以。
func CanManageGroup(role Role) bool { return AtLeast(role, RoleAdmin) }

// CanModifyMember 判断操作者能否修改或移除目标成员。
//
// 规则（任务契约）：
//   - 非 owner/admin 一律拒绝；
//   - owner 不能被任何人降级或移除（包括自己）；
//   - admin 不能改动其他 admin，也不能改动 owner —— 只有 owner 能改 admin；
//   - owner 可以改动任何非 owner 成员；
//   - 可以移除自己（退出），但 owner 例外。
//
// 返回 nil 表示允许，否则返回对应的领域错误。
//
// 判定顺序是「先权限、后规则」：非 owner 操作 owner 属于越权（403），
// 只有 owner 自己触发「群主不可变」规则时才返回 422。顺序反了会让
// admin 得到「参数不合法」的误导性响应。
func CanModifyMember(actor, target Role, sameUser bool) error {
	if !CanManageGroup(actor) {
		return ErrForbidden
	}
	if target == "" {
		return ErrMemberNotFound
	}
	if target == RoleOwner {
		if actor == RoleOwner {
			return ErrOwnerImmutable
		}
		return ErrForbidden
	}
	if sameUser {
		// 本人操作自己（非 owner）：admin 可以把自己降级或退出。
		return nil
	}
	if target == RoleAdmin && actor != RoleOwner {
		// 只有 owner 能改动 admin；admin 之间不能互相操作。
		return ErrForbidden
	}
	return nil
}

// CanChangeRoleTo 判断把目标成员改为 newRole 是否合法。
//
// 目标角色只能是 admin 或 member。目标为 owner 时分两种情况：
//   - 操作者本人就是 owner：属于「群主不可降级」的规则违例，返回 422；
//   - 操作者不是 owner（admin/member/非成员）：根本没有管理群主的权限，
//     返回 403。先判权限再判规则，否则 admin 改群主会错误地得到 422，
//     让客户端误以为是「参数不合法」而非「越权」。
func CanChangeRoleTo(actor, target Role, newRole Role, sameUser bool) error {
	if newRole != RoleAdmin && newRole != RoleMember {
		return ErrValidation
	}
	if target == RoleOwner {
		if actor == RoleOwner {
			return ErrOwnerImmutable
		}
		return ErrForbidden
	}
	return CanModifyMember(actor, target, sameUser)
}

// CanLeave 判断该角色能否主动退出。
// owner 不能退出——群必须始终有群主，否则群将失去管理入口。
func CanLeave(role Role) error {
	if role == RoleOwner {
		return ErrOwnerImmutable
	}
	if role == "" {
		return ErrMemberNotFound
	}
	return nil
}

// CanRemoveMember 判断操作者能否移除目标成员。
// 本人退出走 CanLeave；移除他人要求管理权限与 CanModifyMember 规则。
func CanRemoveMember(actor, target Role, sameUser bool) error {
	if sameUser {
		return CanLeave(target)
	}
	return CanModifyMember(actor, target, false)
}
