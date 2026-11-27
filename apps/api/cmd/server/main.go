package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thesis-shared/api/internal/config"
	"github.com/thesis-shared/api/internal/db"
	"github.com/thesis-shared/api/internal/server"
	"github.com/thesis-shared/api/internal/storage"
)

// version 在构建时通过 -ldflags "-X main.version=..." 注入。
var version = "dev"

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLvl),
	}))
	slog.SetDefault(logger)

	// 生产环境必须提供强 JWT 密钥；否则拒绝启动，避免默认密钥导致令牌可伪造。
	if err := cfg.Validate(); err != nil {
		slog.Error("配置校验失败", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		slog.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("database connected")

	// 对象存储是可选依赖：配置缺失只降级文件功能，不影响其余接口。
	//
	// 注意这里**不**用「启动时能否连上 S3」决定是否启用文件功能。本地开发
	// 常见顺序是 API 先起、S3 后起（Docker 刚启动时尤其如此），旧写法会让
	// /files/presign 路由根本不注册，前端上传直接 404，且必须重启 API 才能
	// 恢复。桶的确认改为在首次使用存储时惰性完成，失败返回 503 并自动重试。
	var store *storage.Client
	if sc, err := storage.New(storage.Config{
		Endpoint:       cfg.S3Endpoint,
		PublicEndpoint: cfg.S3PublicEndpoint,
		PublicUseSSL:   cfg.S3PublicUseSSL,
		AccessKey:      cfg.S3AccessKey,
		SecretKey:      cfg.S3SecretKey,
		Bucket:         cfg.S3Bucket,
		UseSSL:         cfg.S3UseSSL,
	}); err != nil {
		slog.Warn("对象存储未配置，文件上传功能已禁用", "err", err)
	} else {
		store = sc
		// 启动时探测一次，只用于打日志；失败不影响启用。
		if err := sc.EnsureBucket(ctx); err != nil {
			slog.Warn("对象存储暂不可用，文件接口将返回 503 并在后台重试", "err", err, "endpoint", cfg.S3Endpoint)
		} else {
			slog.Info("对象存储已连接", "endpoint", cfg.S3Endpoint, "bucket", cfg.S3Bucket)
		}
	}

	srv := server.New(server.Deps{
		Config:  cfg,
		Version: version,
		Pool:    pool,
		Storage: store,
	})

	go func() {
		slog.Info("api listening", "addr", cfg.APIAddr, "env", cfg.Env, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	// 优雅关闭：捕获 SIGINT/SIGTERM，给在途请求 10s 收尾。
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	slog.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
