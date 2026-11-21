package doc

import "github.com/google/uuid"

// Role 是文档角色，与数据库 doc_role 枚举一一对应。
// 独立定义成字符串常量而非直接用 db.DocRole，是为了让权限判定成为
// 不依赖生成代码的纯函数，单元测试无需构造 sqlc 类型。
type Role string

const (
	RoleOwner  Role = "owner"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// ParseRole 把外部输入解析为文档角色，未知值返回 false。
func ParseRole(raw string) (Role, bool) {
	switch Role(raw) {
	case RoleOwner, RoleEditor, RoleViewer:
		return Role(raw), true
	}
	return "", false
}

// ParseAssignableRole 解析可以被显式授予的角色。
// 文档成员表只用于「覆盖群成员的基础权限」，owner 由创建者与群主隐含，
// 不能通过成员接口授予或收回。
func ParseAssignableRole(raw string) (Role, bool) {
	switch Role(raw) {
	case RoleEditor, RoleViewer:
		return Role(raw), true
	}
	return "", false
}

// rank 给出角色优先级：owner > editor > viewer，未知角色（非成员）为 0。
func rank(role Role) int {
	switch role {
	case RoleOwner:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// AtLeast 判断 role 是否达到 required 的层级。
// 非成员（空角色）永远不满足任何要求。
func AtLeast(role, required Role) bool { return rank(role) >= rank(required) }

// AccessFacts 是计算有效角色所需的全部事实。
// 由 service 从 GetDocumentAccess 一次性取回后填充。
type AccessFacts struct {
	// UserID 是当前用户；uuid.Nil 表示匿名。
	UserID uuid.UUID
	// GroupID 是文档所属群组，用于校验成员覆盖的目标是否为群成员。
	GroupID uuid.UUID
	// CreatedBy 是文档创建者，GroupOwnerID 是所属群组群主，两者隐含 owner。
	CreatedBy    uuid.UUID
	GroupOwnerID uuid.UUID
	// GroupRole 是用户在群组内的角色（owner/admin/member）；空表示非群成员。
	// 只有「是否为空」参与文档权限判定：群内具体角色不改变文档权限，
	// 群主另有 GroupOwnerID 精确判定。
	GroupRole string
	// DocRole 是 doc_members 中的覆盖角色；空表示没有覆盖。
	DocRole Role
	// DefaultRole 是 documents.default_role，即群成员的默认文档角色。
	DefaultRole Role
}

// EffectiveRole 计算用户在文档中的有效角色。
//
// 规则（任务契约）：
//  1. 匿名用户没有任何角色；
//  2. 文档创建者与群主恒为 owner；
//  3. 非群成员没有任何角色——文档属于群组，群外用户即使被写进
//     doc_members 也不能访问（防止跨群越权）；
//  4. doc_members 的显式角色覆盖群成员的默认角色；
//  5. 其余群成员按 default_role 获得基础权限。
//
// 返回空字符串表示无权限。
func EffectiveRole(facts AccessFacts) Role {
	if facts.UserID == uuid.Nil {
		return ""
	}
	if facts.UserID == facts.CreatedBy || facts.UserID == facts.GroupOwnerID {
		return RoleOwner
	}
	// 群外用户一律无权限：doc_members 只在群成员范围内生效。
	if facts.GroupRole == "" {
		return ""
	}
	if facts.DocRole != "" {
		return facts.DocRole
	}
	if facts.DefaultRole == "" {
		// 数据异常（default_role 非空约束下不应发生），保守地按 viewer 处理。
		return RoleViewer
	}
	return facts.DefaultRole
}

// CanView 判断角色能否读取文档。
func CanView(role Role) bool { return rank(role) >= rank(RoleViewer) }

// CanEdit 判断角色能否修改标题/状态、创建快照、写批注。
func CanEdit(role Role) bool { return AtLeast(role, RoleEditor) }

// CanManageMembers 判断角色能否授予/收回文档成员角色。
// 成员管理会改变他人的访问级别，仅 owner 可以。
func CanManageMembers(role Role) bool { return role == RoleOwner }

// CanDelete 判断角色能否删除（软删除）文档。
func CanDelete(role Role) bool { return role == RoleOwner }

// CanModifyAnnotation 判断角色能否修改/解决某条批注。
// owner 可以处理任何批注；editor 只能处理自己写的；viewer 一律不行。
func CanModifyAnnotation(role Role, isAuthor bool) bool {
	if !CanEdit(role) {
		return false
	}
	return role == RoleOwner || isAuthor
}
