// Package moderation 实现举报与屏蔽：举报进入运营审核队列，屏蔽在查询层生效。
package moderation

import "errors"

// 领域错误由 HTTP 层映射为稳定的 API 错误码。
var (
	// ErrValidation 表示请求参数不合法（目标类型、原因、状态、备注长度等）。
	ErrValidation = errors.New("输入校验失败")
	// ErrReportNotFound 表示举报不存在。
	ErrReportNotFound = errors.New("举报不存在")
	// ErrAlreadyReported 表示当前用户已经举报过同一目标。
	// PRD F-06 要求「同一用户对同一目标只能举报一次」，重复提交视为冲突而非成功。
	ErrAlreadyReported = errors.New("你已经举报过该内容")
	// ErrSelfReport 表示举报对象是自己。
	// 自举报没有审核意义，且会成为骚扰运营的低成本手段。
	ErrSelfReport = errors.New("不能举报自己")
	// ErrForbidden 表示当前用户没有执行该操作的权限（仅 role='admin' 可查看与处理举报）。
	ErrForbidden = errors.New("没有权限执行该操作")
	// ErrBlocked 表示该用户已在屏蔽列表中。
	ErrBlocked = errors.New("已经屏蔽过该用户")
	// ErrNotBlocked 表示该用户不在屏蔽列表中。
	ErrNotBlocked = errors.New("尚未屏蔽该用户")
	// ErrUserNotFound 表示句柄对应的用户不存在或已停用。
	ErrUserNotFound = errors.New("用户不存在")
	// ErrInvalidCursor 表示分页游标损坏或不属于举报列表。
	ErrInvalidCursor = errors.New("无效的分页游标")
)
