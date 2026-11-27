package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/citation"
	"github.com/thesis-shared/api/internal/group"
)

// CitationHandler 提供文档引用与参考文献接口（PRD D-06）。
type CitationHandler struct{ service *citation.Service }

func NewCitationHandler(service *citation.Service) *CitationHandler {
	return &CitationHandler{service: service}
}

// GroupFileHandler 提供群文件库接口（PRD G-05）。
//
// 放在同一个文件里是临时安排：本次任务的改动范围限定在
// internal/handler/citation.go，群文件库的 HTTP 层没有单独的文件配额。
// 接线代理若要拆分，把下方「群文件库」一节整体移到 group_files.go 即可，
// 两个 handler 之间没有共享状态。
type GroupFileHandler struct{ service *group.FileService }

func NewGroupFileHandler(service *group.FileService) *GroupFileHandler {
	return &GroupFileHandler{service: service}
}

// ---------------------------------------------------------------------------
// 引用：导入 / 添加 / 列表 / 删除 / 参考文献
// ---------------------------------------------------------------------------

// ImportReferences 处理 POST /api/v1/documents/{id}/references/import。
// 请求体 {bibtex}；重复 cite_key 走更新而非报错（幂等导入）。
func (h *CitationHandler) ImportReferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		BibTeX string `json:"bibtex"`
	}
	// BibTeX 文件动辄几十上百条，几 MB 很常见，64KB 会误伤。
	if !decodeRequestLimit(w, r, &req, 8<<20) {
		return
	}
	result, err := h.service.ImportBibTeX(r.Context(), userID, docID, req.BibTeX)
	if err != nil {
		writeCitationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// AddReference 处理 POST /api/v1/documents/{id}/references。
func (h *CitationHandler) AddReference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		CiteKey   string            `json:"citeKey"`
		EntryType string            `json:"entryType"`
		Title     string            `json:"title"`
		Authors   []citation.Author `json:"authors"`
		Year      *int              `json:"year"`
		Container string            `json:"container"`
		Volume    string            `json:"volume"`
		Issue     string            `json:"issue"`
		Pages     string            `json:"pages"`
		DOI       string            `json:"doi"`
		URL       string            `json:"url"`
		Publisher string            `json:"publisher"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	created, err := h.service.AddReference(r.Context(), userID, docID, citation.AddReferenceInput{
		CiteKey: req.CiteKey, EntryType: req.EntryType, Title: req.Title,
		Authors: req.Authors, Year: req.Year, Container: req.Container,
		Volume: req.Volume, Issue: req.Issue, Pages: req.Pages,
		DOI: req.DOI, URL: req.URL, Publisher: req.Publisher,
	})
	if err != nil {
		writeCitationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"reference": created})
}

// ListReferences 处理 GET /api/v1/documents/{id}/references。
func (h *CitationHandler) ListReferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListReferences(r.Context(), userID, docID)
	if err != nil {
		writeCitationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// DeleteReference 处理 DELETE /api/v1/documents/{id}/references/{refId}。
func (h *CitationHandler) DeleteReference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	refID, err := uuid.Parse(chi.URLParam(r, "refId"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "引用 ID 格式不正确")
		return
	}
	if err := h.service.DeleteReference(r.Context(), userID, docID, refID); err != nil {
		writeCitationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Bibliography 处理 GET /api/v1/documents/{id}/bibliography?format=。
// format 缺省为 gbt7714：中文学位论文场景下这是最常用的默认值。
func (h *CitationHandler) Bibliography(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = string(citation.FormatGBT7714)
	}
	result, err := h.service.Bibliography(r.Context(), userID, docID, format)
	if err != nil {
		writeCitationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeCitationError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, citation.ErrValidation), errors.Is(err, citation.ErrUnsupportedFormat):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, citation.ErrNotFound):
		// 文档不存在与无权查看统一 404，避免泄露存在性。
		status, code, message = http.StatusNotFound, "document_not_found", "文档不存在或无权查看"
	case errors.Is(err, citation.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "没有权限执行该操作"
	case errors.Is(err, citation.ErrReferenceNotFound):
		status, code, message = http.StatusNotFound, "reference_not_found", "引用条目不存在"
	default:
		slog.Error("引用操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}

// ---------------------------------------------------------------------------
// 群文件库
// ---------------------------------------------------------------------------

// Save 处理 POST /api/v1/groups/{slug}/files，请求体
// {fileId, title?, description?, tags?}。仅群成员可调用。
func (h *GroupFileHandler) Save(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	var req struct {
		FileID      string   `json:"fileId"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	fileID, err := uuid.Parse(req.FileID)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "fileId 格式不正确")
		return
	}
	saved, err := h.service.Save(r.Context(), userID, slug, group.SaveGroupFileInput{
		FileID: fileID, Title: req.Title, Description: req.Description, Tags: req.Tags,
	})
	if err != nil {
		writeGroupFileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": saved})
}

// List 处理 GET /api/v1/groups/{slug}/files?tag=&cursor=&limit=。
func (h *GroupFileHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.List(r.Context(), userID, slug,
		r.URL.Query().Get("tag"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeGroupFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Delete 处理 DELETE /api/v1/groups/{slug}/files/{id}。
func (h *GroupFileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	entryID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "文件库条目 ID 格式不正确")
		return
	}
	if err := h.service.Delete(r.Context(), userID, slug, entryID); err != nil {
		writeGroupFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeGroupFileError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, group.ErrValidation), errors.Is(err, group.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, group.ErrGroupNotFound):
		status, code, message = http.StatusNotFound, "group_not_found", "群组不存在"
	case errors.Is(err, group.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "没有权限执行该操作"
	case errors.Is(err, group.ErrGroupFileNotFound):
		status, code, message = http.StatusNotFound, "group_file_not_found", "文件库条目不存在"
	case errors.Is(err, group.ErrGroupFileExists):
		status, code, message = http.StatusConflict, "group_file_exists", "该文件已在群文件库中"
	case errors.Is(err, group.ErrSourceFileNotFound):
		status, code, message = http.StatusNotFound, "file_not_found", "文件不存在"
	default:
		slog.Error("群文件库操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
