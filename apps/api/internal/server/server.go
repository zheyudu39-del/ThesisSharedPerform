package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/config"
	"github.com/thesis-shared/api/internal/content"
	"github.com/thesis-shared/api/internal/file"
	"github.com/thesis-shared/api/internal/handler"
	"github.com/thesis-shared/api/internal/identity"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
	"github.com/thesis-shared/api/internal/paper"
	"github.com/thesis-shared/api/internal/storage"
)

// Deps 是 server 构建路由时所需的外部依赖。
type Deps struct {
	Config  config.Config
	Version string
	Pool    *pgxpool.Pool
	// Storage 为 nil 时文件相关路由不注册，其余功能照常可用。
	// 这样对象存储不可用不会让整个服务启动失败。
	Storage *storage.Client
}

// New 组装 HTTP 路由与中间件，返回可启动的 http.Server。
func New(d Deps) *http.Server {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// 版本化 API 前缀，后续业务路由都挂载在 /api/v1 下。
	tokens := identity.NewTokenManager(d.Config.JWTSecret, d.Config.JWTTTL)
	authService := identity.NewService(d.Pool, tokens)
	auth := handler.NewAuthHandler(authService)
	paperSvc := paper.NewService(d.Pool, paper.NewFetcher())
	papers := handler.NewPaperHandler(paperSvc)
	contents := handler.NewContentHandler(content.NewService(d.Pool))
	users := handler.NewUserHandler(authService)

	// 限流：登录/注册是暴力破解与 Argon2 资源耗尽的主要入口。
	// 每分钟每 IP 最多 10 次认证尝试（对应 PRD §7.3 安全设计中的登录限流）。
	authLimiter := appmiddleware.NewRateLimiter(10, time.Minute)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", handler.HealthCheck(d.Config.Env, d.Version, d.Pool))
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.LimitByIP(authLimiter))
			r.Post("/auth/register", auth.Register)
			r.Post("/auth/login", auth.Login)
		})
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.Authenticate(tokens))
			r.Get("/users/me", auth.Me)
			r.Patch("/users/me", users.UpdateMe)
			r.Post("/users/{handle}/follow", users.Follow)
			r.Delete("/users/{handle}/follow", users.Unfollow)

			// 论文解析：识别 DOI / arXiv 并抓取元数据，需登录（避免被匿名刷外部 API）。
			r.Post("/papers/resolve", papers.Resolve)
			r.Get("/papers", papers.List)
			r.Get("/papers/{id}", papers.Get)

			// 信息流与互动均要求登录，互动写操作在服务层事务中保证幂等。
			r.Post("/posts", contents.CreatePost)
			r.Get("/posts/{id}", contents.GetPost)
			r.Get("/feed", contents.Feed)
			r.Post("/posts/{id}/like", contents.Like)
			r.Delete("/posts/{id}/like", contents.Unlike)
			r.Post("/posts/{id}/bookmark", contents.Bookmark)
			r.Delete("/posts/{id}/bookmark", contents.Unbookmark)
			r.Post("/posts/{id}/comments", contents.CreateComment)
			r.Get("/posts/{id}/comments", contents.ListComments)

			// 文件上传流水线（PRD V-01~V-07 / P-05）。
			// 对象存储不可用时（d.Storage == nil）不注册，避免出现必然失败的接口。
			if d.Storage != nil {
				files := handler.NewFileHandler(file.NewService(d.Pool, d.Storage))
				r.Post("/files/presign", files.Presign)
				r.Post("/files/{id}/complete", files.Complete)
				r.Get("/files/{id}", files.Get)
				r.Get("/files/{id}/pages", files.Pages)
			}
		})

		// 公开个人主页：无需登录，供分享链接与搜索引擎抓取（PRD A-03 / §7.4）。
		// 令牌可选：有效令牌会让响应附带 following，供关注按钮正确初始化；
		// 缺失或过期都按匿名处理，不拦截请求。
		// 必须最后注册：chi 中静态段优先于参数段，/users/me 仍会命中上面的路由，
		// 但把它排在 {handle} 之后可让意图更明确，也避免以后误加通配路由时被遮蔽。
		r.With(appmiddleware.OptionalAuthenticate(tokens)).Get("/users/{handle}", users.GetPublic)
	})

	return &http.Server{
		Addr:              d.Config.APIAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
