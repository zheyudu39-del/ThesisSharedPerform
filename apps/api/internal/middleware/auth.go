package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/identity"
)

type userContextKey struct{}

// TokenParser 是鉴权中间件对令牌解析器的最小依赖。
type TokenParser interface {
	Parse(string) (uuid.UUID, error)
}

// Authenticate 解析 Bearer 令牌，将用户 ID 放入请求上下文。
func Authenticate(tokens TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				unauthorized(w)
				return
			}
			userID, err := tokens.Parse(parts[1])
			if err != nil {
				unauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuthenticate 用于公开接口：带了有效令牌就把用户 ID 放入上下文，
// 没带、格式不对或已过期都按匿名处理，绝不拦截请求。
//
// 它只影响响应中的个性化字段（例如「当前用户是否已关注」），不构成任何访问
// 控制，因此过期令牌降级为匿名比直接 401 更符合公开页面的预期。
func OptionalAuthenticate(tokens TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				if userID, err := tokens.Parse(parts[1]); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, userID))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserID 从上下文读取已鉴权用户；缺失时 ok 为 false。
func UserID(ctx context.Context) (userID uuid.UUID, ok bool) {
	userID, ok = ctx.Value(userContextKey{}).(uuid.UUID)
	return userID, ok
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
		"code": "unauthorized", "message": identity.InvalidToken.Error(),
	}})
}
