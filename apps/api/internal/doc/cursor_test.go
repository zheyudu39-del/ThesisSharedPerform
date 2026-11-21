package doc

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := uuid.New()
	encoded, err := encodeCursor(streamDocuments, now, id)
	if err != nil {
		t.Fatalf("encodeCursor 失败: %v", err)
	}
	gotTime, gotID, err := decodeCursor(encoded, streamDocuments)
	if err != nil {
		t.Fatalf("decodeCursor 失败: %v", err)
	}
	if !gotTime.Equal(now) || gotID != id {
		t.Fatalf("往返结果 = (%v, %v)，期望 (%v, %v)", gotTime, gotID, now, id)
	}
}

func TestDecodeCursorEmptyIsFirstPage(t *testing.T) {
	gotTime, gotID, err := decodeCursor("", streamDocuments)
	if err != nil || !gotTime.IsZero() || gotID != uuid.Nil {
		t.Fatalf("空游标应返回零值且无错误，得到 (%v, %v, %v)", gotTime, gotID, err)
	}
}

func TestDecodeCursorRejectsWrongStream(t *testing.T) {
	encoded, err := encodeCursor(streamDocuments, time.Now(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeCursor(encoded, streamSnapshots); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("跨列表游标应被拒绝，得到 %v", err)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"not-base64!", "e30", string(make([]byte, cursorMaxLen+1))} {
		if _, _, err := decodeCursor(raw, streamDocuments); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("decodeCursor(%q) 应返回 ErrInvalidCursor，得到 %v", raw, err)
		}
	}
}

func TestVersionCursorRoundTrip(t *testing.T) {
	encoded, err := encodeVersionCursor(streamSnapshots, 42)
	if err != nil {
		t.Fatalf("encodeVersionCursor 失败: %v", err)
	}
	before, err := decodeVersionCursor(encoded, streamSnapshots)
	if err != nil || before == nil || *before != 42 {
		t.Fatalf("往返结果 = %v, %v，期望 42", before, err)
	}
}

func TestVersionCursorEmptyIsFirstPage(t *testing.T) {
	before, err := decodeVersionCursor("", streamSnapshots)
	if err != nil || before != nil {
		t.Fatalf("空游标应返回 nil 且无错误，得到 (%v, %v)", before, err)
	}
}

func TestVersionCursorRejectsInvalid(t *testing.T) {
	encoded, err := encodeVersionCursor(streamSnapshots, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeVersionCursor(encoded, streamDocuments); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("跨列表版本游标应被拒绝，得到 %v", err)
	}
	if _, err := encodeVersionCursor(streamSnapshots, 0); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("版本号 0 不应被编码，得到 %v", err)
	}
}
