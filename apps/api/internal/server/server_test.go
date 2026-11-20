package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/thesis-shared/api/internal/config"
)

// TestRoutesRegistered 校验新增路由已挂载，且 /users/me 不会被公开的
// /users/{handle} 抢占：未带令牌访问 /users/me 必须得到 401 而不是 404。
func TestRoutesRegistered(t *testing.T) {
	srv := New(Deps{Config: config.Config{JWTSecret: "test-secret"}, Version: "test"})

	registered := map[string]bool{}
	routes, ok := srv.Handler.(*chi.Mux)
	if !ok {
		t.Fatalf("Handler 不是 *chi.Mux: %T", srv.Handler)
	}
	_ = chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	})
	for _, want := range []string{
		"GET /api/v1/users/{handle}",
		"POST /api/v1/users/{handle}/follow",
		"DELETE /api/v1/users/{handle}/follow",
		"GET /api/v1/users/me",
		"PATCH /api/v1/users/me",
		"GET /api/v1/posts/{id}",
	} {
		if !registered[want] {
			t.Errorf("路由未注册: %s（已注册: %s）", want, strings.Join(keys(registered), ", "))
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	srv.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("GET /users/me 状态码 = %d，期望 401（说明命中了公开的 {handle} 路由）", recorder.Code)
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
