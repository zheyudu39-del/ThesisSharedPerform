package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/thesis-shared/api/internal/search"
)

// SearchHandler 提供统一检索接口。
//
// 命名带 Search 前缀：handler 包内已有其他模块的 Handler / NewHandler
// （如通知），同包不能重名；这也与本项目 GroupHandler、PaperHandler 的命名一致。
type SearchHandler struct{ service *search.Service }

// NewSearchHandler 构造检索处理器。
func NewSearchHandler(service *search.Service) *SearchHandler {
	return &SearchHandler{service: service}
}

// Query 处理 GET /api/v1/search?q=关键词&type=paper,post&limit=20。
//
// 参数：
//   - q：必填，关键词；
//   - type：可选，逗号分隔或重复出现，取 paper/post/user/group；
//   - limit：可选，默认 20，超过 50 时按 50 处理。
func (h *SearchHandler) Query(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	text := strings.TrimSpace(values.Get("q"))
	if text == "" {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "搜索关键词不能为空")
		return
	}

	limit := 0
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "validation_error", "limit 须为整数")
			return
		}
		limit = parsed
	}

	result, err := h.service.Query(r.Context(), search.QueryInput{
		Text:  text,
		Types: parseTypes(values["type"]),
		Limit: limit,
	})
	if err != nil {
		writeSearchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// parseTypes 展开逗号分隔的 type 参数（同时支持 ?type=a,b 与 ?type=a&type=b）。
func parseTypes(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	types := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			if part = strings.TrimSpace(part); part != "" {
				types = append(types, part)
			}
		}
	}
	return types
}

func writeSearchError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, search.ErrValidation):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	default:
		slog.Error("检索失败", "err", err)
	}
	WriteError(w, status, code, message)
}
