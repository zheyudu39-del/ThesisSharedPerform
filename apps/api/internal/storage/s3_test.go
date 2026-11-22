package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnsureBucketRetriesUntilAvailable 锁定「S3 晚起可自愈」的行为。
//
// 真实事故：API 比 SeaweedFS 先启动，启动时 EnsureBucket 失败，旧实现
// 直接把客户端置为 nil，/files/presign 路由根本不注册，前端上传得到 404，
// 且只有重启 API 才能恢复。这里用可控的假 S3 复现该时序。
func TestEnsureBucketRetriesUntilAvailable(t *testing.T) {
	var up atomic.Bool
	var bucketCreated atomic.Bool

	const noSuchBucket = `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchBucket</Code><Message>not found</Message></Error>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			// 模拟 S3 还没起来：连接被拒绝在真实环境里是 ECONNREFUSED，
			// 这里用 503 等价表达「上游不可达」。
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// MakeBucket：PUT /<bucket>
		if r.Method == http.MethodPut {
			bucketCreated.Store(true)
			w.WriteHeader(http.StatusOK)
			return
		}
		// BucketExists：GET /<bucket>?location=
		if bucketCreated.Load() {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint></LocationConstraint>`))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(noSuchBucket))
	}))
	defer srv.Close()

	client, err := New(Config{
		Endpoint:  srv.Listener.Addr().String(),
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Bucket:    "thesis-files",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// 1. S3 未就绪：确认失败，且错误可被识别为「暂不可用」。
	if err := client.EnsureBucket(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("S3 未就绪时应返回 ErrUnavailable，实际 %v", err)
	}

	// 2. S3 起来了。重试限流是 1s，这里等过窗口后应能自愈。
	up.Store(true)
	time.Sleep(bucketRetryInterval + 100*time.Millisecond)

	if _, err := client.PresignPut(context.Background(), "k.pdf", "application/pdf", time.Minute); err != nil {
		t.Fatalf("S3 恢复后 PresignPut 应成功，实际 %v", err)
	}

	// 3. 一旦确认成功就不再重复探测（避免每个请求都打上游）。
	up.Store(false)
	if _, err := client.PresignPut(context.Background(), "k.pdf", "application/pdf", time.Minute); err != nil {
		t.Fatalf("已确认的桶不应再次探测，实际 %v", err)
	}
}

// TestEnsureBucketConfigMissing 端点缺失属于配置问题，应立即失败而不是重试。
func TestEnsureBucketConfigMissing(t *testing.T) {
	if _, err := New(Config{Bucket: "b"}); err == nil {
		t.Fatal("端点缺失时 New 应返回错误")
	}
	if _, err := New(Config{Endpoint: "localhost:9000"}); err == nil {
		t.Fatal("桶名缺失时 New 应返回错误")
	}
}
