package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/identity"
)

type userContextKey struct{}

// 会话与 CSRF 的 Cookie / Header 名称。
// 与 handler 包中的定义保持一致；这里单独定义是为了避免 middleware 反向依赖 handler。
const (
	sessionCookie = "thesis_session"
	csrfCookie    = "thesis_csrf"
	csrfHeader    = "X-CSRF-Token"
)

// TokenParser 是鉴权中间件对令牌解析器的最小依赖。
type TokenParser interface {
	Parse(string) (uuid.UUID, error)
}

// Authenticate 校验会话 Cookie，将用户 ID 放入请求上下文。
//
// 令牌来自 HttpOnly Cookie 而非 Authorization 头：前端脚本读不到令牌，
// 因此 XSS 无法直接窃取会话。代价是浏览器会自动携带 Cookie，
// 必须配合 CSRF 防护 —— 写请求额外经过 CSRFProtect 中间件。
func Authenticate(tokens TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := userFromRequest(r, tokens)
			if !ok {
				unauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuthenticate 用于公开接口：带了有效会话就把用户 ID 放入上下文，
// 没带、格式不对或已过期都按匿名处理，绝不拦截请求。
//
// 它只影响响应中的个性化字段（例如「当前用户是否已关注」），不构成访问控制，
// 因此过期会话降级为匿名比直接 401 更符合公开页面的预期。
func OptionalAuthenticate(tokens TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userID, ok := userFromRequest(r, tokens); ok {
				r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, userID))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFProtect 用双提交 Cookie 方案校验写请求。
//
// 原理：服务端下发一个可被 JS 读取的随机令牌（CSRF Cookie），前端必须把它
// 放进请求头回传。攻击者站点虽然能让浏览器自动带上会话 Cookie，但读不到
// CSRF Cookie（受同源策略限制），因此无法伪造请求头。
//
// 只应对会改变状态的请求（POST/PUT/PATCH/DELETE）挂载。
func CSRFProtect() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(csrfCookie)
			if err != nil || cookie.Value == "" {
				forbidden(w, "缺少 CSRF 令牌")
				return
			}
			header := r.Header.Get(csrfHeader)
			if header == "" {
				forbidden(w, "缺少 CSRF 请求头")
				return
			}
			// 常数时间比较，避免逐字节时序泄露。
			if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
				forbidden(w, "CSRF 令牌不匹配")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// userFromRequest 优先从会话 Cookie 取令牌；为兼容非浏览器客户端（curl、移动端）
// 仍保留 Authorization: Bearer 作为回退。
func userFromRequest(r *http.Request, tokens TokenParser) (uuid.UUID, bool) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if userID, err := tokens.Parse(c.Value); err == nil {
			return userID, true
		}
		return uuid.Nil, false
	}
	// 回退：显式 Bearer 头（脚本、集成测试使用）。
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		if userID, err := tokens.Parse(parts[1]); err == nil {
			return userID, true
		}
	}
	return uuid.Nil, false
}

// UserID 从上下文读取已鉴权用户；缺失时 ok 为 false。
func UserID(ctx context.Context) (userID uuid.UUID, ok bool) {
	userID, ok = ctx.Value(userContextKey{}).(uuid.UUID)
	return userID, ok
}

func unauthorized(w http.ResponseWriter) {
	writeJSONError(w, http.StatusUnauthorized, "unauthorized", identity.InvalidToken.Error())
}

func forbidden(w http.ResponseWriter, message string) {
	writeJSONError(w, http.StatusForbidden, "csrf_failed", message)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
		"code": code, "message": message,
	}})
}
