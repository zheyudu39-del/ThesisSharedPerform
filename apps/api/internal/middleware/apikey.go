package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// APIKeyAuthenticator 校验明文密钥，返回所属用户与授权范围。
//
// 以接口形式注入而非直接依赖 openapi.Service：middleware 包被许多业务包引用，
// 若反向依赖 openapi 会形成循环。server 包负责把具体实现接进来。
type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, rawKey string) (uuid.UUID, []string, error)
}

// apiKeyHeader 是携带密钥的请求头。
// 不用 Authorization: Bearer，是为了与会话令牌明确区分：
// 两者语义不同（一把钥匙 vs 一个用户会话），混用会让日志与网关规则难以辨别。
const apiKeyHeader = "X-API-Key"

type apiKeyCtxKey struct{}

// apiKeyContext 是注入到请求上下文中的密钥身份。
type apiKeyContext struct {
	userID uuid.UUID
	scopes []string
}

// APIKeyAuth 校验 X-API-Key 并注入用户身份。
//
// 失败一律返回 401，不区分「没带密钥」「密钥不存在」「密钥已撤销」：
// 任何区分都会让攻击者能判断某个字符串是否曾经是有效密钥。
func APIKeyAuth(authenticator APIKeyAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get(apiKeyHeader)
			if raw == "" {
				writeAPIKeyError(w, http.StatusUnauthorized, "unauthorized", "缺少 API Key")
				return
			}
			userID, scopes, err := authenticator.Authenticate(r.Context(), raw)
			if err != nil {
				// 认证失败是高频的正常事件（过期、撤销、扫库），
				// 只在服务端异常时打日志，避免噪音。
				writeAPIKeyError(w, http.StatusUnauthorized, "unauthorized", "API Key 无效")
				return
			}
			ctx := context.WithValue(r.Context(), apiKeyCtxKey{}, apiKeyContext{userID: userID, scopes: scopes})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIKeyUserID 取出密钥所属用户。
func APIKeyUserID(ctx context.Context) (uuid.UUID, bool) {
	identity, ok := ctx.Value(apiKeyCtxKey{}).(apiKeyContext)
	if !ok || identity.userID == uuid.Nil {
		return uuid.Nil, false
	}
	return identity.userID, true
}

// APIKeyScopes 取出密钥的授权范围。
func APIKeyScopes(ctx context.Context) []string {
	identity, ok := ctx.Value(apiKeyCtxKey{}).(apiKeyContext)
	if !ok {
		return nil
	}
	return identity.scopes
}

// RequireScope 断言当前密钥具备指定 scope，否则返回 403。
//
// 与认证分离：401 表示「你是谁不清楚」，403 表示「知道你是谁，但你不能做这个」。
// 混为一谈会让第三方开发者误以为是密钥填错了，白白排查。
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, granted := range APIKeyScopes(r.Context()) {
				if granted == scope {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeAPIKeyError(w, http.StatusForbidden, "insufficient_scope", "该 API Key 没有访问此资源的权限")
		})
	}
}

// LimitByAPIKey 按密钥限流。
//
// 按密钥而不是按 IP：第三方程序常部署在共享出口 IP 的云上，
// 按 IP 限流会让同一机房里的多个使用者互相挤占配额；
// 而一把密钥被滥用时，限流应当精确落在那把密钥上。
// 未认证的请求到不了这里（APIKeyAuth 在前），因此不存在空键的情况。
func LimitByAPIKey(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := APIKeyUserID(r.Context())
			if !ok || !limiter.Allow(userID.String()) {
				w.Header().Set("Retry-After", "60")
				writeAPIKeyError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAPIKeyError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// 手写 JSON 而不是复用 handler 包的 WriteError：
	// middleware 不应依赖 handler 包（依赖方向相反）。
	body := `{"error":{"code":"` + code + `","message":"` + message + `"}}`
	if _, err := w.Write([]byte(body)); err != nil {
		slog.Error("写入错误响应失败", "err", err)
	}
}
