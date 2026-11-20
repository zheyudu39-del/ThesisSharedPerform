// Package storage 封装 S3 兼容对象存储（SeaweedFS / MinIO）。
//
// 客户端一律使用 path-style 寻址：SeaweedFS 不提供虚拟主机风格的桶域名，
// 而 path-style 在 MinIO 与真实 S3 上同样可用。
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config 是对象存储的连接参数，由 config.Config 映射而来。
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// Client 是对象存储的最小封装，只暴露上传流水线需要的操作。
type Client struct {
	mc     *minio.Client
	bucket string
}

// New 创建客户端。端点或桶名为空视为配置缺失，直接返回错误，
// 让调用方决定是降级（文件功能不可用）还是拒绝启动。
func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("对象存储端点未配置")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("对象存储桶未配置")
	}

	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       cfg.UseSSL,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("创建对象存储客户端: %w", err)
	}
	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

// Bucket 返回当前使用的桶名。
func (c *Client) Bucket() string { return c.bucket }

// PresignPut 生成浏览器直传用的预签名 PUT 地址。
//
// Content-Type 会被纳入签名：浏览器上传时必须原样带上同样的头，
// 否则签名校验失败。这样服务端才能确定对象真实类型，而不是仅凭扩展名。
func (c *Client) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	headers := http.Header{}
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	u, err := c.mc.PresignHeader(ctx, http.MethodPut, c.bucket, key, ttl, url.Values{}, headers)
	if err != nil {
		return "", fmt.Errorf("生成上传地址: %w", err)
	}
	return u.String(), nil
}

// PresignGet 生成只读的预签名 GET 地址。
func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("生成下载地址: %w", err)
	}
	return u.String(), nil
}

// Stat 查询对象大小。对象不存在时返回 exists=false 且 err=nil，
// 便于调用方把「未上传」与「存储故障」区分开。
func (c *Client) Stat(ctx context.Context, key string) (size int64, exists bool, err error) {
	info, err := c.mc.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("查询对象: %w", err)
	}
	return info.Size, true, nil
}

// Put 上传对象。size 必须准确，否则 S3 分片签名会失败。
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("上传对象 %s: %w", key, err)
	}
	return nil
}

// Get 下载对象，调用方负责关闭返回的 ReadCloser。
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("下载对象 %s: %w", key, err)
	}
	// GetObject 是惰性的，错误（含对象不存在）在首次读取时才暴露；
	// 这里先 Stat 一次，让调用方拿到确定的「不存在」语义。
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("下载对象 %s: %w", key, err)
	}
	return obj, nil
}

// EnsureBucket 确保桶存在，不存在则创建。
func (c *Client) EnsureBucket(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("检查存储桶: %w", err)
	}
	if exists {
		return nil
	}
	if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
		// 多实例同时启动时可能已被别的实例创建，复查一次即可。
		if again, checkErr := c.mc.BucketExists(ctx, c.bucket); checkErr == nil && again {
			return nil
		}
		return fmt.Errorf("创建存储桶 %s: %w", c.bucket, err)
	}
	return nil
}

func isNotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	switch resp.Code {
	case "NoSuchKey", "NoSuchObject", "NoSuchBucket", "NotFound":
		return true
	}
	return resp.StatusCode == http.StatusNotFound
}
