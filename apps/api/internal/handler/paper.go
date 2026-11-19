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
