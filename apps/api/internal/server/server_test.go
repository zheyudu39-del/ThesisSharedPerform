package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/config"
	"github.com/thesis-shared/api/internal/identity"
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
		"GET /api/v1/groups",
		"POST /api/v1/groups",
		"GET /api/v1/groups/{slug}",
		"PATCH /api/v1/groups/{slug}",
		"POST /api/v1/groups/join",
		"POST /api/v1/groups/{slug}/join",
		"GET /api/v1/groups/{slug}/members",
		"PATCH /api/v1/groups/{slug}/members/{userId}",
		"DELETE /api/v1/groups/{slug}/members/{userId}",
		"POST /api/v1/groups/{slug}/invites",
		"POST /api/v1/groups/{slug}/requests",
		"GET /api/v1/groups/{slug}/requests",
		"POST /api/v1/groups/{slug}/requests/{requestId}/approve",
		"POST /api/v1/groups/{slug}/requests/{requestId}/reject",
		"GET /api/v1/groups/{slug}/feed",
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

// TestPublicReadRoutesAllowAnonymous 校验公开读接口（帖子详情、评论列表、论文）
// 在没有令牌时不再返回 401——这是 SSR / SEO 的前提（PRD §7.4）。
//
// 注意这里不断言 200：测试环境的 Deps.Pool 为 nil，请求会一路走到服务层并因
// 缺少连接而失败；关键在于失败发生在鉴权之后，即状态码不是 401。
func TestPublicReadRoutesAllowAnonymous(t *testing.T) {
	srv := New(Deps{Config: config.Config{JWTSecret: "test-secret"}, Version: "test"})
	id := uuid.New().String()

	tests := []struct {
		name   string
		method string
		target string
	}{
		{name: "帖子详情", method: http.MethodGet, target: "/api/v1/posts/" + id},
		{name: "帖子评论", method: http.MethodGet, target: "/api/v1/posts/" + id + "/comments"},
		{name: "论文详情", method: http.MethodGet, target: "/api/v1/papers/" + id},
		{name: "论文列表", method: http.MethodGet, target: "/api/v1/papers"},
		{name: "群组列表", method: http.MethodGet, target: "/api/v1/groups"},
		{name: "群组详情", method: http.MethodGet, target: "/api/v1/groups/some-lab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.target, nil)
			srv.Handler.ServeHTTP(recorder, request)
			if recorder.Code == http.StatusUnauthorized {
				t.Fatalf("%s %s 无令牌返回 401，公开读接口不应要求登录", tt.method, tt.target)
			}
		})
	}
}

// TestWriteRoutesStillRequireAuth 防止「开放匿名读」顺手把写接口也放开了。
func TestWriteRoutesStillRequireAuth(t *testing.T) {
	srv := New(Deps{Config: config.Config{JWTSecret: "test-secret"}, Version: "test"})
	id := uuid.New().String()

	tests := []struct {
		name   string
		method string
		target string
	}{
		{name: "发帖", method: http.MethodPost, target: "/api/v1/posts"},
		{name: "信息流", method: http.MethodGet, target: "/api/v1/feed"},
		{name: "点赞", method: http.MethodPost, target: "/api/v1/posts/" + id + "/like"},
		{name: "取消点赞", method: http.MethodDelete, target: "/api/v1/posts/" + id + "/like"},
		{name: "收藏", method: http.MethodPost, target: "/api/v1/posts/" + id + "/bookmark"},
		{name: "取消收藏", method: http.MethodDelete, target: "/api/v1/posts/" + id + "/bookmark"},
		{name: "发评论", method: http.MethodPost, target: "/api/v1/posts/" + id + "/comments"},
		{name: "论文解析", method: http.MethodPost, target: "/api/v1/papers/resolve"},
		{name: "关注", method: http.MethodPost, target: "/api/v1/users/someone/follow"},
		{name: "取消关注", method: http.MethodDelete, target: "/api/v1/users/someone/follow"},
		{name: "更新资料", method: http.MethodPatch, target: "/api/v1/users/me"},
		{name: "建群", method: http.MethodPost, target: "/api/v1/groups"},
		{name: "改群", method: http.MethodPatch, target: "/api/v1/groups/some-lab"},
		{name: "邀请码加入", method: http.MethodPost, target: "/api/v1/groups/join"},
		{name: "直接加入公开群", method: http.MethodPost, target: "/api/v1/groups/some-lab/join"},
		{name: "成员列表", method: http.MethodGet, target: "/api/v1/groups/some-lab/members"},
		{name: "改成员角色", method: http.MethodPatch, target: "/api/v1/groups/some-lab/members/" + id},
		{name: "移除成员", method: http.MethodDelete, target: "/api/v1/groups/some-lab/members/" + id},
		{name: "创建邀请码", method: http.MethodPost, target: "/api/v1/groups/some-lab/invites"},
		{name: "提交入群申请", method: http.MethodPost, target: "/api/v1/groups/some-lab/requests"},
		{name: "查看入群申请", method: http.MethodGet, target: "/api/v1/groups/some-lab/requests"},
		{name: "通过申请", method: http.MethodPost, target: "/api/v1/groups/some-lab/requests/" + id + "/approve"},
		{name: "拒绝申请", method: http.MethodPost, target: "/api/v1/groups/some-lab/requests/" + id + "/reject"},
		{name: "群内动态", method: http.MethodGet, target: "/api/v1/groups/some-lab/feed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.target, nil)
			srv.Handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s 无令牌状态码 = %d，期望 401", tt.method, tt.target, recorder.Code)
			}
		})
	}
}

// TestPublicReadRoutesAcceptValidToken 校验公开读接口仍会识别有效令牌，
// 这样已登录用户的 liked / bookmarked 等个性化字段不会因为改成可选鉴权而丢失。
func TestPublicReadRoutesAcceptValidToken(t *testing.T) {
	srv := New(Deps{Config: config.Config{JWTSecret: "test-secret", JWTTTL: time.Hour}, Version: "test"})
	token, err := identity.NewTokenManager("test-secret", time.Hour).Issue(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+uuid.New().String(), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusUnauthorized {
		t.Fatal("有效令牌仍被 401 拒绝")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
