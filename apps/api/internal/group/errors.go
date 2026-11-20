// Package group 实现群组领域逻辑：群组 CRUD、成员与角色、邀请码、入群申请与群内动态。
package group

import "errors"

// 领域错误由 HTTP 层映射为稳定的 API 错误码。
var (
	// ErrValidation 表示请求参数不合法（名称、简介、加入策略、分页参数等）。
	ErrValidation = errors.New("输入校验失败")
	// ErrGroupNotFound 表示群组不存在或已删除。
	ErrGroupNotFound = errors.New("群组不存在")
	// ErrForbidden 表示当前用户没有执行该操作的权限。
	ErrForbidden = errors.New("没有权限执行该操作")
	// ErrOwnerImmutable 表示试图修改或移除群主（群主不可降级、不可移除、不可退出）。
	ErrOwnerImmutable = errors.New("群主的角色不可修改，且群主不能退出群组")
	// ErrMemberNotFound 表示目标用户不是该群组成员。
	ErrMemberNotFound = errors.New("该用户不是群组成员")
	// ErrAlreadyMember 表示当前用户已经是群组成员。
	ErrAlreadyMember = errors.New("已经是群组成员")
	// ErrInviteNotFound 覆盖邀请码不存在、已过期、已用尽三种情况。
	// 合并为一个错误，避免调用方通过响应差异区分它们。
	ErrInviteNotFound = errors.New("邀请码不存在或已失效")
	// ErrAlreadyRequested 表示已有待处理的入群申请。
	ErrAlreadyRequested = errors.New("已提交过入群申请，请等待审核")
	// ErrJoinPolicy 表示当前群组的加入策略不允许这种加入方式。
	ErrJoinPolicy = errors.New("该群组不支持这种加入方式")
	// ErrRequestNotFound 表示入群申请不存在或已被处理。
	ErrRequestNotFound = errors.New("入群申请不存在或已处理")
	// ErrInvalidCursor 表示分页游标损坏或不属于当前列表。
	ErrInvalidCursor = errors.New("无效的分页游标")
)
