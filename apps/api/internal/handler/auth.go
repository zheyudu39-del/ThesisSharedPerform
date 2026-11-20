package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/thesis-shared/api/internal/identity"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// AuthHandler 暴露注册、登录、登出和当前用户资料接口。
type AuthHandler struct {
	service *identity.Service
	// ttl 是会话 Cookie 的有效期，与 JWT 的 exp 保持一致。
	ttl  time.Duration
	cook cookieConfig
}

func NewAuthHandler(service *identity.Service, ttl time.Duration, isProduction bool) *AuthHandler {
	return &AuthHandler{
		service: service,
		ttl:     ttl,
		cook:    newCookieConfig(isProduction),
	}
}

// authResponse 只返回用户资料。
//
// 令牌不再出现在响应体里：它写在 HttpOnly Cookie 中，前端脚本无法读取，
// 这正是本次改造的目的（XSS 无法窃取会话）。
type authResponse struct {
	User identity.User `json:"user"`
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Handle   string `json:"handle"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	user, token, err := h.service.Register(r.Context(), identity.RegisterInput{
		Email: req.Email, Password: req.Password, Name: req.Name, Handle: req.Handle,
	})
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if !h.issueSession(w, token) {
		return
	}
	writeJSON(w, http.StatusCreated, authResponse{User: user})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	user, token, err := h.service.Login(r.Context(), identity.LoginInput{
		Email: req.Email, Password: req.Password,
	})
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if !h.issueSession(w, token) {
		return
	}
	writeJSON(w, http.StatusOK, authResponse{User: user})
}

// issueSession 下发会话与 CSRF 两个 Cookie。失败时已写好 500 响应。
func (h *AuthHandler) issueSession(w http.ResponseWriter, token string) bool {
	csrf, err := newCSRFToken()
	if err != nil {
		slog.Error("生成 CSRF 令牌失败", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return false
	}
	setSessionCookie(w, token, h.ttl, h.cook)
	setCSRFCookie(w, csrf, h.ttl, h.cook)
	return true
}

// Logout 清除会话 Cookie。
//
// 该接口故意不要求登录：已过期或已失效的会话也应能安全登出。
// 仍受 CSRFProtect 保护，防止第三方站点强制用户登出。
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookies(w, h.cook)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "未通过身份认证")
		return
	}
	user, err := h.service.GetByID(r.Context(), userID)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		User identity.User `json:"user"`
	}{User: user})
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer func() { _ = r.Body.Close() }()
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "请求体格式不正确")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func writeAuthError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "服务暂时不可用"
	switch {
	case errors.Is(err, identity.EmailTaken):
		status, code, message = http.StatusConflict, "email_taken", err.Error()
	case errors.Is(err, identity.HandleTaken):
		status, code, message = http.StatusConflict, "handle_taken", err.Error()
	case errors.Is(err, identity.InvalidCredentials):
		status, code, message = http.StatusUnauthorized, "invalid_credentials", identity.InvalidCredentials.Error()
	case errors.Is(err, identity.ValidationError):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, identity.InvalidToken):
		status, code, message = http.StatusUnauthorized, "unauthorized", "无效或过期的令牌"
	default:
		slog.Error("账号操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]errorBody{"error": {Code: code, Message: message}})
}
