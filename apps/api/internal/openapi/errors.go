// Package openapi 实现对外开放的只读 API：密钥管理、密钥认证与公开数据端点。
//
// 设计边界：
//   - 只读。开放 API 不提供任何写操作，避免第三方程序误改用户数据；
//   - 结构稳定。对外接口一旦发布不宜频繁变动，因此返回扁平 DTO，
//     不复用内部 API 里可能随产品迭代的嵌套结构；
//   - 与内部 API 隔离。/openapi/v1 挂独立前缀、用 X-API-Key 认证，
//     不读 Cookie，因此也不受 CSRF 影响。
package openapi

import "errors"

// 领域错误由 HTTP 层映射为稳定的错误码。
var (
	// ErrValidation 表示请求参数不合法（名称、scopes 等）。
	ErrValidation = errors.New("输入校验失败")
	// ErrUnauthorized 表示缺少密钥、密钥不存在、已撤销或已过期。
	// 三种情况合并，避免攻击者通过响应差异判断某个密钥是否存在过。
	ErrUnauthorized = errors.New("API Key 无效")
	// ErrScopeDenied 表示密钥有效，但不具备访问该端点所需的 scope。
	ErrScopeDenied = errors.New("该 API Key 没有访问此资源的权限")
	// ErrKeyNotFound 表示要撤销的密钥不存在或不属于当前用户。
	ErrKeyNotFound = errors.New("API Key 不存在")
	// ErrNotFound 表示请求的公开资源不存在。
	ErrNotFound = errors.New("资源不存在")
)
