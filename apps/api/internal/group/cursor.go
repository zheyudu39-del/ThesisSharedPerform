package group

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// 游标与 content 模块采用同样的思路：复合排序键 (时间, ID) 编码为 URL 安全的
// base64，ID 用于打破同一时间的并列，保证翻页稳定不重不漏。
//
// 独立实现而非复用 content.EncodeCursor，是因为那边的 stream 名称是固定枚举；
// 群组列表、成员列表、群内动态各有自己的排序键，塞进同一份校验会互相牵制。
type cursorPayload struct {
	Version   int       `json:"v"`
	Stream    string    `json:"s"`
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
}

const cursorMaxLen = 1024

// encodeCursor 编码游标；stream 标识列表种类，防止把某个列表的游标
// 拿去翻另一个列表（排序键不同，混用会产生错误结果）。
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

// decodeCursor 解码游标。空串表示第一页，返回零值且 err 为 nil。
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
	if err := json.Unmarshal(payload, &c); err != nil || c.Version != 1 || c.Stream != stream || c.ID == uuid.Nil || c.CreatedAt.IsZero() {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return c.CreatedAt.UTC(), c.ID, nil
}
