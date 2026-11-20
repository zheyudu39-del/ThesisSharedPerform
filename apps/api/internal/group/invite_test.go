package group

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("生成 UUID 失败: %v", err)
	}
	return id
}

func inviteFixture(expiresAt pgtype.Timestamptz, maxUses *int32, used int32) db.GroupInvite {
	return db.GroupInvite{ExpiresAt: expiresAt, MaxUses: maxUses, UsedCount: used}
}

func validTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func int32Ptr(v int32) *int32 { return &v }

func TestInviteUsable(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		invite db.GroupInvite
		want   bool
	}{
		{
			name:   "无过期无上限始终可用",
			invite: inviteFixture(pgtype.Timestamptz{}, nil, 0),
			want:   true,
		},
		{
			name:   "未过期且未用尽",
			invite: inviteFixture(validTime(now.Add(time.Hour)), int32Ptr(5), 4),
			want:   true,
		},
		{
			name:   "恰好用尽",
			invite: inviteFixture(validTime(now.Add(time.Hour)), int32Ptr(5), 5),
			want:   false,
		},
		{
			name:   "超过上限",
			invite: inviteFixture(validTime(now.Add(time.Hour)), int32Ptr(5), 6),
			want:   false,
		},
		{
			name:   "已过期",
			invite: inviteFixture(validTime(now.Add(-time.Second)), nil, 0),
			want:   false,
		},
		{
			name:   "恰好在过期时刻",
			invite: inviteFixture(validTime(now), nil, 0),
			want:   false,
		},
		{
			name:   "有上限但未使用",
			invite: inviteFixture(pgtype.Timestamptz{}, int32Ptr(1), 0),
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inviteUsable(tt.invite, now); got != tt.want {
				t.Fatalf("inviteUsable() = %v，期望 %v", got, tt.want)
			}
		})
	}
}

func TestInviteFromDB(t *testing.T) {
	expires := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	record := db.GroupInvite{
		Code: "abcdefghijklmnop", ExpiresAt: validTime(expires), MaxUses: int32Ptr(3),
	}
	invite := inviteFromDB(record)
	if invite.Code != record.Code {
		t.Fatalf("Code = %q", invite.Code)
	}
	if invite.ExpiresAt == nil || !invite.ExpiresAt.Equal(expires) {
		t.Fatalf("ExpiresAt = %v，期望 %v", invite.ExpiresAt, expires)
	}
	if invite.MaxUses == nil || *invite.MaxUses != 3 {
		t.Fatalf("MaxUses = %v", invite.MaxUses)
	}

	noExpiry := inviteFromDB(db.GroupInvite{Code: "x"})
	if noExpiry.ExpiresAt != nil {
		t.Fatalf("无过期时间应输出 null，得到 %v", noExpiry.ExpiresAt)
	}
	if noExpiry.MaxUses != nil {
		t.Fatalf("无上限应输出 null，得到 %v", noExpiry.MaxUses)
	}
}

func TestValidateCreate(t *testing.T) {
	tests := []struct {
		name    string
		input   CreateGroupInput
		wantErr bool
	}{
		{name: "合法", input: CreateGroupInput{Name: "研究组", JoinPolicy: "public"}},
		{name: "名称为空", input: CreateGroupInput{Name: "   ", JoinPolicy: "public"}, wantErr: true},
		{name: "名称过长", input: CreateGroupInput{Name: string(make([]rune, 65)), JoinPolicy: "public"}, wantErr: true},
		{name: "非法策略", input: CreateGroupInput{Name: "组", JoinPolicy: "secret"}, wantErr: true},
		{name: "空策略", input: CreateGroupInput{Name: "组"}, wantErr: true},
		{name: "简介过长", input: CreateGroupInput{Name: "组", JoinPolicy: "invite", Description: string(make([]rune, 501))}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := validateCreate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v，wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id := mustUUID(t)
	encoded, err := encodeCursor("members", now, id)
	if err != nil {
		t.Fatal(err)
	}
	gotTime, gotID, err := decodeCursor(encoded, "members")
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(now) || gotID != id {
		t.Fatalf("游标往返不一致: %v %v", gotTime, gotID)
	}
	if _, _, err := decodeCursor(encoded, "groups"); err == nil {
		t.Fatal("应拒绝跨列表游标")
	}
	if _, _, err := decodeCursor("not-base64", "members"); err == nil {
		t.Fatal("应拒绝损坏游标")
	}
	if _, _, err := decodeCursor("", "members"); err != nil {
		t.Fatalf("空游标应表示第一页: %v", err)
	}
}
