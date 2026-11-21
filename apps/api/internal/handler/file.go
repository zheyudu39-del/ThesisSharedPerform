package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/file"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// FileHandler 提供文件预签名上传、完成校验与页图查询接口。
type FileHandler struct{ service *file.Service }

// NewFileHandler 构造处理器。
func NewFileHandler(service *file.Service) *FileHandler {
	return &FileHandler{service: service}
}

// NewFileHandlerWithIndexer 构造带文本层索引的处理器（PRD S-04）。
//
// 文本层提取在 file 服务里完成，但索引实现属于 search 包；
// 由 server 在组装时注入，避免 file 包反向依赖 search 的具体实现。
func NewFileHandlerWithIndexer(service *file.Service, indexer file.TextIndexer) *FileHandler {
	service.SetTextIndexer(indexer)
	return &FileHandler{service: service}
}

// Presign 处理 POST /api/v1/files/presign。
func (h *FileHandler) Presign(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req file.PresignInput
	if !decodeRequest(w, r, &req) {
		return
	}
	result, err := h.service.Presign(r.Context(), userID, req)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// Complete 处理 POST /api/v1/files/{id}/complete。
func (h *FileHandler) Complete(w http.ResponseWriter, r *http.Request) {
	userID, fileID, ok := h.userAndFile(w, r)
	if !ok {
		return
	}
	var req struct {
		SizeBytes int64 `json:"sizeBytes"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	record, err := h.service.Complete(r.Context(), userID, fileID, req.SizeBytes)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": record})
}

// Get 处理 GET /api/v1/files/{id}。
func (h *FileHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, fileID, ok := h.userAndFile(w, r)
	if !ok {
		return
	}
	record, err := h.service.Get(r.Context(), userID, fileID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": record})
}

// Pages 处理 GET /api/v1/files/{id}/pages?page=1&size=10。
func (h *FileHandler) Pages(w http.ResponseWriter, r *http.Request) {
	userID, fileID, ok := h.userAndFile(w, r)
	if !ok {
		return
	}
	page, ok := parsePositiveInt(w, r, "page", 1, 0)
	if !ok {
		return
	}
	size, ok := parsePositiveInt(w, r, "size", 10, 50)
	if !ok {
		return
	}
	result, err := h.service.Pages(r.Context(), userID, fileID, page, size)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *FileHandler) userAndFile(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return uuid.Nil, uuid.Nil, false
	}
	fileID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "文件 ID 格式不正确")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, fileID, true
}

// parsePositiveInt 解析查询参数：缺省用 def，超过 max（max>0 时）报错。
func parsePositiveInt(w http.ResponseWriter, r *http.Request, name string, def, max int32) (int32, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || (max > 0 && int32(n) > max) {
		if max > 0 {
			WriteError(w, http.StatusUnprocessableEntity, "validation_error", name+" 须为 1 到 "+strconv.Itoa(int(max)))
		} else {
			WriteError(w, http.StatusUnprocessableEntity, "validation_error", name+" 须为正整数")
		}
		return 0, false
	}
	return int32(n), true
}

func writeFileError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, file.ErrUnsupportedType):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, file.ErrValidation):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, file.ErrFileNotFound):
		// 不存在与无权访问共用 404，避免探测他人文件。
		status, code, message = http.StatusNotFound, "file_not_found", "文件不存在"
	case errors.Is(err, file.ErrUploadMissing):
		status, code, message = http.StatusUnprocessableEntity, "upload_missing", "未找到上传的对象，请重新上传"
	case errors.Is(err, file.ErrSizeMismatch):
		status, code, message = http.StatusUnprocessableEntity, "size_mismatch", err.Error()
	case errors.Is(err, file.ErrStorageUnavailable):
		status, code, message = http.StatusServiceUnavailable, "storage_unavailable", "文件服务暂时不可用"
	default:
		slog.Error("文件操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
