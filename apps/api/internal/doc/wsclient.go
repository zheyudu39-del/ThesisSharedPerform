package doc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CollabReloader 通知协同服务把某个文档从内存里卸载。
//
// 以接口形式注入而不是让 doc 包直接发 HTTP：
// 单元测试不需要起一个 Node 进程，也便于在协同服务不可用时优雅降级。
type CollabReloader interface {
	// ReloadDocument 断开该文档的全部连接并卸载内存中的 Y.Doc。
	// 返回 error 表示「没能让内存里的文档失效」，调用方应据此提示用户手动刷新。
	ReloadDocument(ctx context.Context, docID uuid.UUID) error
	// SnapshotDocument 让协同服务把当前正文写到指定版本的快照存储。
	// 返回写入的字节数；error 表示快照内容没能落盘。
	SnapshotDocument(ctx context.Context, docID uuid.UUID, version int32) (int64, error)
}

// wsReloader 通过协同服务的内部 HTTP 端点实现 CollabReloader。
type wsReloader struct {
	baseURL string
	// key 是内部通道的签名密钥，由 JWT_SECRET 派生（见 deriveInternalKey）。
	key     []byte
	client  *http.Client
	timeout time.Duration
}

// NewWSReloader 构造协同服务客户端。baseURL 为空时返回 nil，
// 表示不启用跨进程协作——调用方据此把回滚降级为「需要手动刷新」。
func NewWSReloader(baseURL, jwtSecret string) CollabReloader {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil
	}
	return &wsReloader{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     deriveInternalKey(jwtSecret),
		// 超时略大于协同服务自身的 5s 连接排空窗口：
		// 对方正在等连接断开时，我们不该先放弃。
		client:  &http.Client{Timeout: 8 * time.Second},
		timeout: 8 * time.Second,
	}
}

// deriveInternalKey 派生内部通道密钥。
// 与 apps/ws/src/internal-auth.js 的 deriveKey 保持一致：
// HMAC-SHA256(JWT_SECRET, "internal-api")。
func deriveInternalKey(jwtSecret string) []byte {
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	mac.Write([]byte("internal-api"))
	return mac.Sum(nil)
}

// sign 生成与 Node 侧一致的请求签名。
// 签名内容把「动作 + 目标」绑在一起，防止一个 reload 签名被挪用来触发快照。
func (r *wsReloader) sign(scope string, timestamp int64) string {
	mac := hmac.New(sha256.New, r.key)
	fmt.Fprintf(mac, "%s:%d", scope, timestamp)
	return hex.EncodeToString(mac.Sum(nil))
}

// ReloadDocument 调用 POST /internal/documents/{docId}/reload。
func (r *wsReloader) ReloadDocument(ctx context.Context, docID uuid.UUID) error {
	timestamp := time.Now().UnixMilli()
	url := fmt.Sprintf("%s/internal/documents/%s/reload", r.baseURL, docID.String())

	reqCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("构造协同服务请求失败: %w", err)
	}
	req.Header.Set("X-Internal-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Internal-Signature", r.sign("reload:"+docID.String(), timestamp))

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("连接协同服务失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// 409 表示连接没能在窗口内排空：文档仍会随最后一个连接离开而自然卸载，
	// 只是这次回滚不会立刻对所有在线用户生效。
	var payload struct {
		Reason      string `json:"reason"`
		Connections int    `json:"connections"`
	}
	_ = json.Unmarshal(body, &payload)
	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("文档仍有 %d 个连接未断开，稍后会自动生效", payload.Connections)
	}
	return fmt.Errorf("协同服务返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// SnapshotDocument 调用 POST /internal/documents/{docId}/snapshot/{version}。
func (r *wsReloader) SnapshotDocument(ctx context.Context, docID uuid.UUID, version int32) (int64, error) {
	timestamp := time.Now().UnixMilli()
	url := fmt.Sprintf("%s/internal/documents/%s/snapshot/%d", r.baseURL, docID.String(), version)

	reqCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, nil)
	if err != nil {
		return 0, fmt.Errorf("构造协同服务请求失败: %w", err)
	}
	req.Header.Set("X-Internal-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Internal-Signature", r.sign("snapshot:"+docID.String(), timestamp))

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("连接协同服务失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("协同服务返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		SizeBytes int64  `json:"sizeBytes"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, fmt.Errorf("解析协同服务响应失败: %w", err)
	}
	if payload.SizeBytes <= 0 {
		return 0, fmt.Errorf("快照内容为空（%s）", payload.Reason)
	}
	return payload.SizeBytes, nil
}
