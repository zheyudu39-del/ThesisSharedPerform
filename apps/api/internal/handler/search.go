package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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
//   - limit：可选，默认 20，超过 50 时按 50 处理；
//   - from / to：可选，创建时间范围，接受 RFC3339 或 YYYY-MM-DD；
//   - tag：可选，标签（匹配帖子与论文的标签）；
//   - author：可选，作者 handle（可带 @）。
//
// from / to 格式非法、from 晚于 to 一律 422：静默忽略非法筛选会让用户
// 以为筛选生效了，实际看到的却是未筛选的结果。
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

	// 只给日期时，from 取当天 00:00，to 取当天 23:59:59.999999999，
	// 否则「from=to=某天」会筛出零结果，与用户直觉相反。
	from, ok := parseTimeParam(w, values.Get("from"), false, "from")
	if !ok {
		return
	}
	to, ok := parseTimeParam(w, values.Get("to"), true, "to")
	if !ok {
		return
	}
	if from != nil && to != nil && from.After(*to) {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "开始时间不能晚于结束时间")
		return
	}

	result, err := h.service.Query(r.Context(), search.QueryInput{
		Text:   text,
		Types:  parseTypes(values["type"]),
		Limit:  limit,
		From:   from,
		To:     to,
		Tag:    strings.TrimSpace(values.Get("tag")),
		Author: strings.TrimSpace(values.Get("author")),
	})
	if err != nil {
		writeSearchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// parseTimeParam 解析时间筛选参数。
//
// 接受 RFC3339（2024-01-02T15:04:05Z07:00）与 YYYY-MM-DD 两种写法：
// 前者给程序与高级用户，后者是日期选择器最自然的输出。
// 日期按 UTC 解释——服务端没有请求时区信息，统一 UTC 至少保证结果稳定可复现。
func parseTimeParam(w http.ResponseWriter, raw string, endOfDay bool, field string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		utc := parsed.UTC()
		return &utc, true
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			// 加一天再减 1 纳秒，避免手工拼 23:59:59 时漏掉最后一秒内的时间戳。
			value := parsed.AddDate(0, 0, 1).Add(-time.Nanosecond).UTC()
			return &value, true
		}
		utc := parsed.UTC()
		return &utc, true
	}
	WriteError(w, http.StatusUnprocessableEntity, "validation_error", field+" 须为 RFC3339 或 YYYY-MM-DD 格式")
	return nil, false
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
