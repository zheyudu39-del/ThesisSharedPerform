package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/curation"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// CurationHandler 提供精选策展接口。
type CurationHandler struct{ service *curation.Service }

func NewCurationHandler(service *curation.Service) *CurationHandler {
	return &CurationHandler{service: service}
}

// List 处理 GET /api/v1/collections，公开可读。
func (h *CurationHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListCollections(r.Context(), viewerID(r), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeCurationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Get 处理 GET /api/v1/collections/{slug}，公开可读。
func (h *CurationHandler) Get(w http.ResponseWriter, r *http.Request) {
	slug, ok := curationSlug(w, r)
	if !ok {
		return
	}
	detail, err := h.service.GetCollection(r.Context(), viewerID(r), slug)
	if err != nil {
		writeCurationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"collection": detail.Collection,
		"items":      detail.Items,
	})
}

type createCollectionRequest struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	StartsAt    *time.Time `json:"startsAt"`
	EndsAt      *time.Time `json:"endsAt"`
}

// Create 处理 POST /api/v1/collections。
func (h *CurationHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req createCollectionRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	created, err := h.service.CreateCollection(r.Context(), userID, curation.CreateCollectionInput{
		Title:       req.Title,
		Description: req.Description,
		StartsAt:    req.StartsAt,
		EndsAt:      req.EndsAt,
	})
	if err != nil {
		writeCurationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"collection": created})
}

type updateCollectionRequest struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	StartsAt    *time.Time `json:"startsAt"`
	EndsAt      *time.Time `json:"endsAt"`
}

// Update 处理 PATCH /api/v1/collections/{slug}。
func (h *CurationHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := curationSlug(w, r)
	if !ok {
		return
	}
	var req updateCollectionRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	updated, err := h.service.UpdateCollection(r.Context(), userID, slug, curation.UpdateCollectionInput{
		Title:       req.Title,
		Description: req.Description,
		StartsAt:    req.StartsAt,
		EndsAt:      req.EndsAt,
	})
	if err != nil {
		writeCurationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collection": updated})
}

// Delete 处理 DELETE /api/v1/collections/{slug}。
func (h *CurationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := curationSlug(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteCollection(r.Context(), userID, slug); err != nil {
		writeCurationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type addItemRequest struct {
	PostID   *uuid.UUID `json:"postId"`
	PaperID  *uuid.UUID `json:"paperId"`
	Note     string     `json:"note"`
	Position *int32     `json:"position"`
}

// AddItem 处理 POST /api/v1/collections/{slug}/items。
func (h *CurationHandler) AddItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := curationSlug(w, r)
	if !ok {
		return
	}
	var req addItemRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	item, err := h.service.AddItem(r.Context(), userID, slug, curation.AddItemInput{
		PostID:   req.PostID,
		PaperID:  req.PaperID,
		Note:     req.Note,
		Position: req.Position,
	})
	if err != nil {
		writeCurationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": item})
}

// RemoveItem 处理 DELETE /api/v1/collections/{slug}/items/{itemId}。
func (h *CurationHandler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := curationSlug(w, r)
	if !ok {
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemId"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "策展项 ID 格式不正确")
		return
	}
	if err := h.service.RemoveItem(r.Context(), userID, slug, itemID); err != nil {
		writeCurationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReorderItem 处理 PATCH /api/v1/collections/{slug}/items/{itemId}。
func (h *CurationHandler) ReorderItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := curationSlug(w, r)
	if !ok {
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemId"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "策展项 ID 格式不正确")
		return
	}
	var req struct {
		Position int32 `json:"position"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	if err := h.service.ReorderItem(r.Context(), userID, slug, itemID, req.Position); err != nil {
		writeCurationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func curationSlug(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		WriteError(w, http.StatusNotFound, "collection_not_found", "策展集不存在")
		return "", false
	}
	return slug, true
}

func writeCurationError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, curation.ErrValidation), errors.Is(err, curation.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, curation.ErrCollectionNotFound):
		status, code, message = http.StatusNotFound, "collection_not_found", "策展集不存在"
	case errors.Is(err, curation.ErrNotCurator):
		status, code, message = http.StatusForbidden, "not_curator", "只有运营或认证大V 可以创建策展集"
	case errors.Is(err, curation.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "没有权限执行该操作"
	case errors.Is(err, curation.ErrItemNotFound):
		status, code, message = http.StatusNotFound, "item_not_found", "策展项不存在"
	case errors.Is(err, curation.ErrDuplicateItem):
		status, code, message = http.StatusConflict, "duplicate_item", "该内容已在策展集中"
	case errors.Is(err, curation.ErrTargetNotFound):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", "引用的内容不存在"
	default:
		slog.Error("策展操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
