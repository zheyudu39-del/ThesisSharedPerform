package identity

import "errors"

// 领域错误由 HTTP 层映射为稳定的 API 错误码。
var (
	EmailTaken         = errors.New("邮箱已被注册")
	HandleTaken        = errors.New("句柄已被使用")
	InvalidCredentials = errors.New("邮箱或密码错误")
	InvalidToken       = errors.New("无效或过期的令牌")
	ValidationError    = errors.New("请求参数校验失败")
)
