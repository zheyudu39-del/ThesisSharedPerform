package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/middleware"
	"github.com/thesis-shared/api/internal/paper"
)

// PaperHandler 提供论文解析与查询接口。
type PaperHandler struct {
	svc *paper.Service
}

// NewPaperHandler 构造处理器。
func NewPaperHandler(svc *paper.Service) *PaperHandler {
	return &PaperHandler{svc: svc}
}

// maxResolveBody 限制解析请求体大小，防止超长输入。
const maxResolveBody = 4 << 10 // 4 KiB

type resolveRequest struct {
	Input string `json:"input"`
}

// Resolve 处理 POST /api/v1/papers/resolve。
// 接收 DOI / arXiv 编号 / URL，识别后抓取元数据并入库（幂等）。
func (h *PaperHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}

	var req resolveRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxResolveBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "请求体格式不正确")
		return
	}
	if req.Input == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "请输入 DOI、arXiv 编号或论文链接")
		return
	}

	result, err := h.svc.Resolve(r.Context(), req.Input, userID)
	if err != nil {
		switch {
		case errors.Is(err, paper.ErrInvalidInput):
			writeError(w, http.StatusUnprocessableEntity, "invalid_input", err.Error())
		case errors.Is(err, paper.ErrNotFound):
			// 外部源没有这篇论文：引导用户手动补录（PRD P-04）。
			writeError(w, http.StatusNotFound, "not_found", "未找到该论文，可手动补录元数据")
		default:
			slog.Error("解析论文失败", "err", err, "input", req.Input)
			writeError(w, http.StatusBadGateway, "fetch_failed", "暂时无法从外部源获取元数据，请稍后重试")
		}
		return
	}

	status := http.StatusOK
	if !result.Existing {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

// Get 处理 GET /api/v1/papers/{id}。
func (h *PaperHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "论文 ID 格式不正确")
		return
	}

	p, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, paper.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "论文不存在")
			return
		}
		slog.Error("查询论文失败", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paper": p})
}

type createManualRequest struct {
	Title         string `json:"title"`
	Authors       []struct {
		Name        string `json:"name"`
		Affiliation string `json:"affiliation"`
		ORCID       string `json:"orcid"`
	} `json:"authors"`
	Abstract      string `json:"abstract"`
	Venue         string `json:"venue"`
	PublishedYear int    `json:"publishedYear"`
	DOI           string `json:"doi"`
	ArxivID       string `json:"arxivId"`
}

// CreateManual 处理 POST /api/v1/papers，手动补录论文（PRD P-04）。
//
// 适用于没有 DOI / arXiv 编号的论文：专著、技术报告、内部文档。
// 必填标题与至少一位作者，其余字段可选。
func (h *PaperHandler) CreateManual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}

	var req createManualRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	authors := make([]paper.Author, 0, len(req.Authors))
	for _, a := range req.Authors {
		authors = append(authors, paper.Author{
			Name: a.Name, Affiliation: a.Affiliation, ORCID: a.ORCID,
		})
	}

	result, err := h.svc.CreateManual(r.Context(), userID, paper.CreateManualInput{
		Title:         req.Title,
		Authors:       authors,
		Abstract:      req.Abstract,
		Venue:         req.Venue,
		PublishedYear: req.PublishedYear,
		DOI:           req.DOI,
		ArxivID:       req.ArxivID,
	})
	if err != nil {
		switch {
		case errors.Is(err, paper.ErrInvalidInput):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		default:
			slog.Error("手动补录论文失败", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
		}
		return
	}

	status := http.StatusOK
	if !result.Existing {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

type updatePaperRequest struct {
	Title         *string `json:"title"`
	Authors       *[]struct {
		Name        string `json:"name"`
		Affiliation string `json:"affiliation"`
		ORCID       string `json:"orcid"`
	} `json:"authors"`
	Abstract      *string `json:"abstract"`
	Venue         *string `json:"venue"`
	PublishedYear *int    `json:"publishedYear"`
	CitationCount *int    `json:"citationCount"`
}

// Update 处理 PATCH /api/v1/papers/{id}，局部更新元数据（PRD P-02）。
//
// 字段为 nil 表示不修改；空字符串表示清空。标题与作者会触发
// title_norm / dedupe_hash 重算，保证软去重仍然有效。
func (h *PaperHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	paperID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "论文 ID 格式不正确")
		return
	}

	var req updatePaperRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	input := paper.UpdateMetadataInput{
		Title:         req.Title,
		Abstract:      req.Abstract,
		Venue:         req.Venue,
		PublishedYear: req.PublishedYear,
		CitationCount: req.CitationCount,
	}
	if req.Authors != nil {
		authors := make([]paper.Author, 0, len(*req.Authors))
		for _, a := range *req.Authors {
			authors = append(authors, paper.Author{
				Name: a.Name, Affiliation: a.Affiliation, ORCID: a.ORCID,
			})
		}
		input.Authors = &authors
	}

	updated, err := h.svc.UpdateMetadata(r.Context(), userID, paperID, input)
	if err != nil {
		switch {
		case errors.Is(err, paper.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "论文不存在")
		case errors.Is(err, paper.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "没有权限修改这篇论文")
		case errors.Is(err, paper.ErrInvalidInput):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		default:
			slog.Error("更新论文失败", "err", err, "paperId", paperID)
			writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paper": updated})
}

// Merge 处理 POST /api/v1/papers/{id}/merge，请求体 {duplicateId}。
//
// 把 {duplicateId} 合并进 {id} 并删除重复记录（PRD P-02）。
func (h *PaperHandler) Merge(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	keepID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "论文 ID 格式不正确")
		return
	}
	var req struct {
		DuplicateID string `json:"duplicateId"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	dupID, err := uuid.Parse(req.DuplicateID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "重复论文 ID 格式不正确")
		return
	}

	result, err := h.svc.MergePapers(r.Context(), userID, keepID, dupID)
	if err != nil {
		switch {
		case errors.Is(err, paper.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "论文不存在")
		case errors.Is(err, paper.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "没有权限合并这两篇论文")
		case errors.Is(err, paper.ErrInvalidInput):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		default:
			slog.Error("合并论文失败", "err", err, "keep", keepID, "dup", dupID)
			writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// SetPDFFile 处理 PATCH /api/v1/papers/{id}/pdf。
//
// 请求体 {"fileId": "..."}；fileId 为 null 或空串表示卸载全文。
// 这是「论文页上传 PDF 后刷新还在」的那条链路：上传只把文件写进
// files 表，关联论文记录靠这个接口。
func (h *PaperHandler) SetPDFFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	paperID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "论文 ID 格式不正确")
		return
	}

	var req struct {
		FileID *string `json:"fileId"`
	}
	if !decodeOptionalRequest(w, r, &req) {
		return
	}

	var fileID *uuid.UUID
	if req.FileID != nil && *req.FileID != "" {
		parsed, err := uuid.Parse(*req.FileID)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "validation_error", "文件 ID 格式不正确")
			return
		}
		fileID = &parsed
	}

	updated, err := h.svc.SetPDFFile(r.Context(), userID, paperID, fileID)
	if err != nil {
		switch {
		case errors.Is(err, paper.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "论文不存在")
		case errors.Is(err, paper.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "没有权限修改这篇论文的全文")
		case errors.Is(err, paper.ErrInvalidInput):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		default:
			slog.Error("设置论文全文失败", "err", err, "paperId", paperID)
			writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paper": updated})
}

// List 处理 GET /api/v1/papers。
func (h *PaperHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := int32(20)
	offset := int32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}

	papers, total, err := h.svc.List(r.Context(), limit, offset)
	if err != nil {
		slog.Error("列出论文失败", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"papers": papers,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{
		"code": code, "message": message,
	}})
}
