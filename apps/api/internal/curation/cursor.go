package curation

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// 游标沿用 group 模块的复合排序键思路：(starts_at, id) 编码为 URL 安全的
// base64。ID 用于打破同一时刻的并列，保证翻页稳定不重不漏。
//
// 与群组游标的差别在排序键：策展集按「上架时间倒序」排，
// 不是创建时间，因此游标里携带的是 starts_at。
type cursorPayload struct {
	Version  int       `json:"v"`
	Stream   string    `json:"s"`
	StartsAt time.Time `json:"t"`
	ID       uuid.UUID `json:"id"`
}

const (
	cursorMaxLen = 1024
	// cursorStreamCollections 标识策展集列表。
	// stream 字段防止把某个列表的游标拿去翻另一个列表（排序键不同，会翻出错页）。
	cursorStreamCollections = "collections"
)

func encodeCursor(stream string, startsAt time.Time, id uuid.UUID) (string, error) {
	if stream == "" || id == uuid.Nil || startsAt.IsZero() {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursorPayload{Version: 1, Stream: stream, StartsAt: startsAt.UTC(), ID: id})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// decodeCursor 解码游标。空串表示第一页，返回零值且 err 为 nil。
func decodeCursor(raw, stream string) (startsAt time.Time, id uuid.UUID, err error) {
	if raw == "" {
		return time.Time{}, uuid.Nil, nil
	}
	if len(raw) > cursorMaxLen {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	var c cursorPayload
	if err := json.Unmarshal(payload, &c); err != nil || c.Version != 1 || c.Stream != stream || c.ID == uuid.Nil || c.StartsAt.IsZero() {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return c.StartsAt.UTC(), c.ID, nil
}
