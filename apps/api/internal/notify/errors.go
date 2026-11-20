// Package notify 实现站内通知：生成、列表、已读、偏好与点赞聚合。
package notify

import "errors"

// 领域错误由 HTTP 层映射为稳定的 API 错误码。
var (
	// ErrValidation 表示请求参数不合法（通知类型、偏好取值、分页参数等）。
	ErrValidation = errors.New("输入校验失败")
	// ErrInvalidCursor 表示分页游标损坏或不属于当前列表。
	ErrInvalidCursor = errors.New("无效的分页游标")
	// ErrNotificationNotFound 表示通知不存在，或不属于当前用户。
	ErrNotificationNotFound = errors.New("通知不存在")
)
