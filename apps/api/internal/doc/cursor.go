package doc

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// 游标沿用 group / content 模块的思路：把复合排序键编码为 URL 安全的 base64，
// 并用 stream 名称防止把某个列表的游标拿去翻另一个列表。
//
// 文档列表按 (updated_at, id) 倒序；快照列表按 version 倒序（版本号单调递增
// 且唯一，不需要第二排序键），因此这里提供两种载荷。
const cursorMaxLen = 1024

// stream 名称。与列表一一对应。
const (
	streamDocuments = "documents"
	streamSnapshots = "snapshots"
)

type cursorPayload struct {
	Version   int       `json:"v"`
	Stream    string    `json:"s"`
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
}

type versionCursorPayload struct {
	Version int    `json:"v"`
	Stream  string `json:"s"`
	Before  int32  `json:"before"`
}

// encodeCursor 编码 (时间, ID) 复合游标。
func encodeCursor(stream string, createdAt time.Time, id uuid.UUID) (string, error) {
	if stream == "" || id == uuid.Nil || createdAt.IsZero() {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursorPayload{Version: 1, Stream: stream, CreatedAt: createdAt.UTC(), ID: id})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// decodeCursor 解码 (时间, ID) 复合游标。空串表示第一页。
func decodeCursor(raw, stream string) (createdAt time.Time, id uuid.UUID, err error) {
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
	if err := json.Unmarshal(payload, &c); err != nil ||
		c.Version != 1 || c.Stream != stream || c.ID == uuid.Nil || c.CreatedAt.IsZero() {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return c.CreatedAt.UTC(), c.ID, nil
}

// encodeVersionCursor 编码快照版本游标。
func encodeVersionCursor(stream string, before int32) (string, error) {
	if stream == "" || before <= 0 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(versionCursorPayload{Version: 1, Stream: stream, Before: before})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// decodeVersionCursor 解码快照版本游标。空串表示第一页，返回 nil。
func decodeVersionCursor(raw, stream string) (*int32, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > cursorMaxLen {
		return nil, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var c versionCursorPayload
	if err := json.Unmarshal(payload, &c); err != nil ||
		c.Version != 1 || c.Stream != stream || c.Before <= 0 {
		return nil, ErrInvalidCursor
	}
	before := c.Before
	return &before, nil
}
