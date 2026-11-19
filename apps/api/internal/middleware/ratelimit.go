package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter 是基于内存的滑动窗口限流器。
//
// 适用范围：M1 单实例部署。多实例时需要换成 Redis 实现
// （见 technical-architecture.md §5.2 缓存策略中的 rate:{scope}:{key}）。
type RateLimiter struct {
	mu       sync.Mutex
	hits     map[string][]time.Time
	limit    int
	window   time.Duration
	lastGC   time.Time
	gcPeriod time.Duration
}

// NewRateLimiter 创建限流器：window 时间内最多 limit 次。
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		hits:     make(map[string][]time.Time),
		limit:    limit,
		window:   window,
		gcPeriod: 10 * time.Minute,
	}
}

// Allow 判断 key 在窗口内是否还有配额，并记录本次命中。
func (rl *RateLimiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.gcLocked(now, cutoff)

	entries := rl.hits[key]
	// 丢弃窗口外的记录
	idx := 0
	for idx < len(entries) && entries[idx].Before(cutoff) {
		idx++
	}
	entries = entries[idx:]

	if len(entries) >= rl.limit {
		rl.hits[key] = entries
		return false
	}
	rl.hits[key] = append(entries, now)
	return true
}

// gcLocked 周期性清理空 key，避免内存无限增长。
func (rl *RateLimiter) gcLocked(now, cutoff time.Time) {
	if now.Sub(rl.lastGC) < rl.gcPeriod {
		return
	}
	rl.lastGC = now
	for k, v := range rl.hits {
		if len(v) == 0 || v[len(v)-1].Before(cutoff) {
			delete(rl.hits, k)
		}
	}
}

// LimitByIP 按客户端 IP 限流，超限返回 429。
func LimitByIP(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow(clientIP(r)) {
				w.Header().Set("Retry-After", "60")
				tooManyRequests(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP 取远端地址的 IP 部分（chi 的 RealIP 中间件已处理 X-Forwarded-For）。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
		"code":    "rate_limited",
		"message": "请求过于频繁，请稍后再试",
	}})
}
