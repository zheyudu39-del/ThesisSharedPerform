package admin

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// cursorStreams 是允许使用时间序游标的列表。
//
// 每个 stream 对应一个独立的排序上下文。跨 stream 复用游标会被拒绝：
// 否则把「帖子列表」的游标传给「用户列表」，会从错误的时间点开始翻页，
// 而且看起来一切正常。
var cursorStreams = map[string]struct{}{
	"users":    {},
	"posts":    {},
	"papers":   {},
	"groups":   {},
	"files":    {},
	"comments": {},
}

type cursor struct {
	Version   int       `json:"v"`
	Stream    string    `json:"s"`
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
}

// encodeCursor 把 (created_at, id) 编码成 URL 安全的 base64。
//
// 用 (时间, ID) 而不是 offset：后台数据一直在变，offset 分页会漏行或重复行。
// ID 用于打破同一微秒内的并列。
func encodeCursor(stream string, createdAt time.Time, id uuid.UUID) *string {
	if createdAt.IsZero() || id == uuid.Nil {
		return nil
	}
	payload, err := json.Marshal(cursor{
		Version:   1,
		Stream:    stream,
		CreatedAt: createdAt.UTC(),
		ID:        id,
	})
	if err != nil {
		return nil
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return &encoded
}

// decodeCursor 解析游标。空串表示第一页，返回零值即可。
func decodeCursor(raw, stream string) (pgtype.Timestamptz, pgtype.UUID, error) {
	var emptyAt pgtype.Timestamptz
	var emptyID pgtype.UUID
	if raw == "" {
		return emptyAt, emptyID, nil
	}
	if len(raw) > 1024 {
		return emptyAt, emptyID, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return emptyAt, emptyID, ErrInvalidCursor
	}
	var c cursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return emptyAt, emptyID, ErrInvalidCursor
	}
	if c.Version != 1 || c.Stream != stream || c.CreatedAt.IsZero() || c.ID == uuid.Nil {
		return emptyAt, emptyID, ErrInvalidCursor
	}
	if _, ok := cursorStreams[stream]; !ok {
		return emptyAt, emptyID, ErrInvalidCursor
	}
	return pgtype.Timestamptz{Time: c.CreatedAt, Valid: true},
		pgtype.UUID{Bytes: c.ID, Valid: true}, nil
}
