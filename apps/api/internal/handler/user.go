package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/thesis-shared/api/internal/identity"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// UserHandler 提供公开个人主页、关注关系与本人资料编辑接口。
type UserHandler struct{ service *identity.Service }

func NewUserHandler(service *identity.Service) *UserHandler {
	return &UserHandler{service: service}
}

// Follow 处理 POST /api/v1/users/{handle}/follow。
func (h *UserHandler) Follow(w http.ResponseWriter, r *http.Request) {
	h.setFollow(w, r, true)
}

// Unfollow 处理 DELETE /api/v1/users/{handle}/follow。
func (h *UserHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	h.setFollow(w, r, false)
}

func (h *UserHandler) setFollow(w http.ResponseWriter, r *http.Request, follow bool) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	handle := chi.URLParam(r, "handle")
	var (
		following bool
		count     int64
		err       error
	)
	if follow {
		following, count, err = h.service.Follow(r.Context(), userID, handle)
	} else {
		following, count, err = h.service.Unfollow(r.Context(), userID, handle)
	}
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"following": following, "followerCount": count})
}

// GetPublic 处理 GET /api/v1/users/{handle}，无需登录。
//
// 令牌可选：带了有效令牌时响应会附带 `following`，供公开主页正确渲染关注按钮；
// 匿名访问不返回该字段。
func (h *UserHandler) GetPublic(w http.ResponseWriter, r *http.Request) {
	viewerID, authenticated := appmiddleware.UserID(r.Context())
	user, stats, following, err := h.service.GetPublicView(r.Context(), viewerID, chi.URLParam(r, "handle"))
	if err != nil {
		writeUserError(w, err)
		return
	}
	payload := map[string]any{"user": user, "stats": stats}
	if authenticated {
		payload["following"] = following
	}
	writeJSON(w, http.StatusOK, payload)
}

// updateMeRequest 的字段均为指针：nil 表示请求体未出现该字段，保持不变。
type updateMeRequest struct {
	Name           *string   `json:"name"`
	Bio            *string   `json:"bio"`
	Institution    *string   `json:"institution"`
	ResearchFields *[]string `json:"researchFields"`
}

// UpdateMe 处理 PATCH /api/v1/users/me。
func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req updateMeRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	user, err := h.service.UpdateProfile(r.Context(), userID, identity.UpdateProfileInput{
		Name: req.Name, Bio: req.Bio, Institution: req.Institution, ResearchFields: req.ResearchFields,
	})
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func writeUserError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, identity.ErrUserNotFound):
		status, code, message = http.StatusNotFound, "user_not_found", "用户不存在"
	case errors.Is(err, identity.ErrSelfFollow):
		status, code, message = http.StatusUnprocessableEntity, "self_follow", "不能关注自己"
	case errors.Is(err, identity.ValidationError):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, identity.InvalidToken):
		status, code, message = http.StatusUnauthorized, "unauthorized", "无效或过期的令牌"
	default:
		slog.Error("用户操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
