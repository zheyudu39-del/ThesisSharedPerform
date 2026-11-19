package middleware

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.Allow("k") {
			t.Fatalf("第 %d 次应被允许", i+1)
		}
	}
	if rl.Allow("k") {
		t.Fatal("超出上限应被拒绝")
	}
}

func TestRateLimiterIsolatesKeys(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	if !rl.Allow("a") {
		t.Fatal("a 首次应允许")
	}
	if !rl.Allow("b") {
		t.Fatal("b 不应受 a 影响")
	}
	if rl.Allow("a") {
		t.Fatal("a 第二次应被拒绝")
	}
}

func TestRateLimiterExpiresAfterWindow(t *testing.T) {
	rl := NewRateLimiter(1, 50*time.Millisecond)
	if !rl.Allow("k") {
		t.Fatal("首次应允许")
	}
	if rl.Allow("k") {
		t.Fatal("窗口内第二次应拒绝")
	}
	time.Sleep(70 * time.Millisecond)
	if !rl.Allow("k") {
		t.Fatal("窗口过后应重新允许")
	}
}
