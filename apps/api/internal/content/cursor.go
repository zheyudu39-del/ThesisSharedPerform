package content

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidCursor 表示游标损坏、格式错误或不能用于当前信息流。
var ErrInvalidCursor = errors.New("无效的分页游标")

// cursor 通过复合排序键实现稳定翻页，ID 用于打破相同时间/分数的并列。
type cursor struct {
	Version   int       `json:"v"`
	Stream    string    `json:"s"`
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
	HotScore  *float64  `json:"h,omitempty"`
}

// EncodeCursor 将信息流游标编码为 URL 安全的 base64 字符串。
func EncodeCursor(stream string, createdAt time.Time, id uuid.UUID, hotScore *float64) (string, error) {
	if stream != "following" && stream != "all" && stream != "hot" && stream != "comments" {
		return "", ErrInvalidCursor
	}
	if id == uuid.Nil || createdAt.IsZero() || (stream == "hot" && hotScore == nil) || (stream != "hot" && hotScore != nil) {
		return "", ErrInvalidCursor
	}
	if hotScore != nil && (math.IsNaN(*hotScore) || math.IsInf(*hotScore, 0)) {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursor{Version: 1, Stream: stream, CreatedAt: createdAt.UTC(), ID: id, HotScore: hotScore})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeCursor 解码游标并校验它是否属于指定信息流。
func DecodeCursor(raw, stream string) (createdAt time.Time, id uuid.UUID, hotScore *float64, err error) {
	if raw == "" {
		return time.Time{}, uuid.Nil, nil, nil
	}
	if len(raw) > 1024 {
		return time.Time{}, uuid.Nil, nil, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, uuid.Nil, nil, ErrInvalidCursor
	}
	var c cursor
	if err := json.Unmarshal(payload, &c); err != nil || c.Version != 1 || c.Stream != stream || c.ID == uuid.Nil || c.CreatedAt.IsZero() {
		return time.Time{}, uuid.Nil, nil, ErrInvalidCursor
	}
	if (stream == "hot") != (c.HotScore != nil) {
		return time.Time{}, uuid.Nil, nil, ErrInvalidCursor
	}
	if c.HotScore != nil && (math.IsNaN(*c.HotScore) || math.IsInf(*c.HotScore, 0)) {
		return time.Time{}, uuid.Nil, nil, ErrInvalidCursor
	}
	return c.CreatedAt.UTC(), c.ID, c.HotScore, nil
}
