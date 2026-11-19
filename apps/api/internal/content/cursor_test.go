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
