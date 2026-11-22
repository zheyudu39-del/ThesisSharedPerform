package content

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.FixedZone("test", 3600))
	id := uuid.New()
	score := 12.5
	encoded, err := EncodeCursor("hot", now, id, &score)
	if err != nil {
		t.Fatal(err)
	}
	gotTime, gotID, gotScore, err := DecodeCursor(encoded, "hot")
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(now) || gotID != id || gotScore == nil || *gotScore != score {
		t.Fatalf("游标往返不一致: %v %v %v", gotTime, gotID, gotScore)
	}
}

func TestCursorRejectsInvalidInput(t *testing.T) {
	id := uuid.New()
	if _, err := EncodeCursor("all", time.Now(), id, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := DecodeCursor("not-base64", "all"); err == nil {
		t.Fatal("应拒绝损坏游标")
	}
	if _, _, _, err := DecodeCursor("", "all"); err != nil {
		t.Fatal(err)
	}
	encoded, _ := EncodeCursor("all", time.Now(), id, nil)
	if _, _, _, err := DecodeCursor(encoded, "hot"); err == nil {
		t.Fatal("应拒绝跨信息流游标")
	}
	if _, err := EncodeCursor("all", time.Time{}, id, nil); err == nil {
		t.Fatal("应拒绝空时间")
	}
}

// TestEncodeCursorProfileStreams 验证个人主页的两个 Tab 能用同一套游标。
// posts / reposts 的排序键是 (created_at, id)，与 all 完全一致，
// 如果不放进白名单，翻页时 EncodeCursor 会直接报错。
func TestEncodeCursorProfileStreams(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()

	for _, stream := range []string{"posts", "reposts", "paper"} {
		encoded, err := EncodeCursor(stream, now, id, nil)
		if err != nil {
			t.Fatalf("EncodeCursor(%q): %v", stream, err)
		}
		gotTime, gotID, _, err := DecodeCursor(encoded, stream)
		if err != nil {
			t.Fatalf("DecodeCursor(%q): %v", stream, err)
		}
		if gotID != id || !gotTime.Equal(now.Truncate(time.Millisecond)) {
			// base64 + JSON 往返会保留纳秒精度到毫秒；比较用 UnixNano。
			if gotID != id || gotTime.UnixNano() != now.UnixNano() {
				t.Fatalf("%s 往返不一致: id=%v time=%v", stream, gotID, gotTime)
			}
		}
	}

	// 跨 stream 复用应当被拒绝：posts 的游标不能拿去翻 reposts。
	encoded, _ := EncodeCursor("posts", now, id, nil)
	if _, _, _, err := DecodeCursor(encoded, "reposts"); err == nil {
		t.Fatal("跨 stream 的游标应当被拒绝")
	}
}

// TestDecodeCursorRejectsUnknownStream 验证未知 stream 不会静默通过。
func TestDecodeCursorRejectsUnknownStream(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	encoded, err := EncodeCursor("all", time.Now().UTC(), id, nil)
	if err != nil {
		t.Fatalf("EncodeCursor: %v", err)
	}
	if _, _, _, err := DecodeCursor(encoded, "bogus"); err == nil {
		t.Fatal("未知 stream 应当被拒绝")
	}
}
