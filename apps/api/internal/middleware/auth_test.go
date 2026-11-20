package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/identity"
)

// TestOptionalAuthenticate 校验公开接口的「令牌可选」语义：
// 有效令牌注入用户，缺失/无效令牌按匿名放行而不是 401。
func TestOptionalAuthenticate(t *testing.T) {
	tokens := identity.NewTokenManager("test-secret", time.Hour)
	userID := uuid.New()
	token, err := tokens.Issue(userID)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		header    string
		wantUser  uuid.UUID
		wantHasID bool
	}{
		{name: "有效令牌", header: "Bearer " + token, wantUser: userID, wantHasID: true},
		{name: "无令牌", header: ""},
		{name: "格式错误", header: "Token " + token},
		{name: "无效令牌", header: "Bearer not-a-jwt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotID uuid.UUID
			var gotOK bool
			handler := OptionalAuthenticate(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotID, gotOK = UserID(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			request := httptest.NewRequest(http.MethodGet, "/api/v1/users/someone", nil)
			if tt.header != "" {
				request.Header.Set("Authorization", tt.header)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("状态码 = %d，期望 200（可选鉴权不应拦截）", recorder.Code)
			}
			if gotOK != tt.wantHasID || gotID != tt.wantUser {
				t.Fatalf("UserID = (%v, %v)，期望 (%v, %v)", gotID, gotOK, tt.wantUser, tt.wantHasID)
			}
		})
	}
}
