package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/thesis-shared/api/internal/identity"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// AuthHandler 暴露注册、登录和当前用户资料接口。
type AuthHandler struct {
	service *identity.Service
}

func NewAuthHandler(service *identity.Service) *AuthHandler {
	return &AuthHandler{service: service}
}

type authResponse struct {
	User  identity.User `json:"user"`
	Token string        `json:"token"`
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
	writeJSON(w, http.StatusCreated, authResponse{User: user, Token: token})
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
	writeJSON(w, http.StatusOK, authResponse{User: user, Token: token})
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
