package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/config"
	"github.com/thesis-shared/api/internal/content"
	"github.com/thesis-shared/api/internal/curation"
	"github.com/thesis-shared/api/internal/doc"
	"github.com/thesis-shared/api/internal/file"
	"github.com/thesis-shared/api/internal/group"
	"github.com/thesis-shared/api/internal/handler"
	"github.com/thesis-shared/api/internal/identity"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
	"github.com/thesis-shared/api/internal/notify"
	"github.com/thesis-shared/api/internal/openapi"
	"github.com/thesis-shared/api/internal/paper"
	"github.com/thesis-shared/api/internal/search"
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
	auth := handler.NewAuthHandler(authService, d.Config.JWTTTL, d.Config.IsProduction())
	paperSvc := paper.NewService(d.Pool, paper.NewFetcher())
	papers := handler.NewPaperHandler(paperSvc)

	// 检索与通知：两者都不依赖其他业务包，先构造，再以接口注入到写入方。
	searchSvc := search.NewService(d.Pool)
	notifySvc := notify.NewService(d.Pool)
	notifications := handler.NewHandler(notifySvc)
	searches := handler.NewSearchHandler(searchSvc)

	// 内容与群组服务通过 Indexer 接口接收检索能力，避免包之间直接 import。
	// 内容服务另通过 Notifier 接口接收通知能力：点赞与评论在写入成功后生成通知。
	contentSvc := content.NewService(d.Pool)
	contentSvc.SetIndexer(contentIndexer{service: searchSvc})
	contentSvc.SetNotifier(contentNotifier{service: notifySvc})
	contents := handler.NewContentHandler(contentSvc)

	users := handler.NewUserHandler(authService)

	groupSvc := group.NewService(d.Pool)
	groupSvc.SetIndexer(groupIndexer{service: searchSvc})
	groups := handler.NewGroupHandler(groupSvc)

	// 文档元数据与权限。正文同步与快照落盘由 Node 协同服务负责，
	// 这里只提供 REST 侧的 CRUD、成员覆盖、快照登记与批注。
	docSvc := doc.NewService(d.Pool)
	documents := handler.NewDocumentHandler(docSvc, tokens, d.Config.WSPublicURL)

	// 精选策展。PRD §1.2 明确不做个性化推荐，精选流靠人工判断，
	// 因此策展资格（运营 / 大V）在服务层判定，路由本身只需区分读与写。
	curationSvc := curation.NewService(d.Pool)
	curations := handler.NewCurationHandler(curationSvc)

	// 开放 API：密钥管理与只读数据端点。
	// 数据端点挂在 /openapi/v1 独立前缀下，用 X-API-Key 认证，不读 Cookie，
	// 因此与内部 API 的 CSRF 体系完全隔离。
	openapiSvc := openapi.NewService(d.Pool)
	openAPI := handler.NewOpenAPIHandler(openapiSvc, paperSvc, contentSvc, authService)
	// 每把密钥每分钟 60 次：既够第三方程序做增量同步，
	// 又能挡住被滥用时的流量。
	openAPILimiter := appmiddleware.NewRateLimiter(60, time.Minute)

	// 限流：登录/注册是暴力破解与 Argon2 资源耗尽的主要入口。
	// 每分钟每 IP 最多 10 次认证尝试（对应 PRD §7.3 安全设计中的登录限流）。
	authLimiter := appmiddleware.NewRateLimiter(10, time.Minute)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", handler.HealthCheck(d.Config.Env, d.Version, d.Pool))

		// 登录/注册：限流保护。这两个接口不需要 CSRF ——
		// 攻击者无法凭空构造有效凭据，响应也只是下发一个新会话。
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.LimitByIP(authLimiter))
			r.Post("/auth/register", auth.Register)
			r.Post("/auth/login", auth.Login)
		})

		// 登出：需要 CSRF，否则第三方站点可以强制用户登出（拒绝服务）。
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.CSRFProtect())
			r.Post("/auth/logout", auth.Logout)
		})

		// 需要登录的接口。
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.Authenticate(tokens))

			// 只读：无需 CSRF。
			r.Get("/users/me", auth.Me)
			r.Get("/feed", contents.Feed)

			// 开放 API 密钥只读：只返回元数据（前缀、scopes、最后使用时间），
			// 不含明文或哈希，因此与其它只读接口一样不需要 CSRF。
			r.Get("/api-keys", openAPI.ListKeys)

			// 通知中心只读：通知天然属于个人数据，匿名没有读取语义。
			r.Get("/notifications", notifications.List)
			r.Get("/notifications/unread-count", notifications.UnreadCount)
			r.Get("/notification-prefs", notifications.GetPrefs)

			// 群组只读：成员名单、待审申请与群内动态都要求登录，
			// 服务层再按角色/成员身份做访问控制。
			r.Get("/groups/{slug}/members", groups.Members)
			r.Get("/groups/{slug}/requests", groups.Requests)
			r.Get("/groups/{slug}/feed", groups.Feed)

			// 文档只读：文档属于群组，服务层按群成员身份与文档角色判定，
			// 非成员一律 404（不泄露文档是否存在）。
			r.Get("/groups/{slug}/documents", documents.List)
			r.Get("/documents/{id}", documents.Get)
			r.Get("/documents/{id}/collab-token", documents.CollabToken)
			r.Get("/documents/{id}/members", documents.Members)
			r.Get("/documents/{id}/snapshots", documents.Snapshots)
			r.Get("/documents/{id}/annotations", documents.Annotations)

			// 写接口：会话 Cookie 由浏览器自动携带，必须用双提交令牌区分
			// 「本站发起」与「第三方站点伪造」，否则存在 CSRF 风险。
			r.Group(func(r chi.Router) {
				r.Use(appmiddleware.CSRFProtect())

				r.Patch("/users/me", users.UpdateMe)
				r.Post("/users/{handle}/follow", users.Follow)
				r.Delete("/users/{handle}/follow", users.Unfollow)

				// 论文解析：识别 DOI / arXiv 并抓取元数据，需登录（避免被匿名刷外部 API）。
				r.Post("/papers/resolve", papers.Resolve)

				// 信息流写操作在服务层事务中保证幂等。
				r.Post("/posts", contents.CreatePost)
				r.Post("/posts/{id}/like", contents.Like)
				r.Delete("/posts/{id}/like", contents.Unlike)
				r.Post("/posts/{id}/bookmark", contents.Bookmark)
				r.Delete("/posts/{id}/bookmark", contents.Unbookmark)
				r.Post("/posts/{id}/comments", contents.CreateComment)

				// 群组写操作。建群、改群、成员与角色、邀请码、入群申请都在这里；
				// 具体权限（owner > admin > member）由 group 服务层判定。
				r.Post("/groups", groups.Create)
				r.Patch("/groups/{slug}", groups.Update)
				r.Post("/groups/join", groups.JoinByInvite)
				r.Post("/groups/{slug}/join", groups.JoinPublic)
				r.Patch("/groups/{slug}/members/{userId}", groups.UpdateMember)
				r.Delete("/groups/{slug}/members/{userId}", groups.RemoveMember)
				r.Post("/groups/{slug}/invites", groups.CreateInvite)
				r.Post("/groups/{slug}/requests", groups.CreateRequest)
				r.Post("/groups/{slug}/requests/{requestId}/approve", groups.ApproveRequest)
				r.Post("/groups/{slug}/requests/{requestId}/reject", groups.RejectRequest)

				// 文档写操作。权限 owner > editor > viewer 由 doc 服务层判定：
				// viewer 只能读，editor 可改标题/状态、建快照、写批注，
				// 仅 owner 可删除文档、调整成员角色。
				r.Post("/groups/{slug}/documents", documents.Create)
				r.Patch("/documents/{id}", documents.Update)
				r.Delete("/documents/{id}", documents.Delete)
				r.Post("/documents/{id}/snapshots", documents.CreateSnapshot)
				// 导出：正文由前端从 TipTap 取出 HTML 提交，服务端转 Word/Markdown/LaTeX。
				// 要求 editor 及以上，viewer 只能读不能导出（PRD D-08）。
				r.Post("/documents/{id}/export", documents.Export)
				r.Post("/documents/{id}/annotations", documents.CreateAnnotation)
				r.Patch("/documents/{id}/annotations/{aid}", documents.UpdateAnnotation)
				r.Patch("/documents/{id}/members/{userId}", documents.UpdateMember)
				r.Delete("/documents/{id}/members/{userId}", documents.RemoveMember)

				// 策展写操作。策展资格（运营 / 认证大V）与「只能改自己的集合」
				// 由 curation 服务层判定，返回 403 而非 404：策展集是公开内容，
				// 隐藏其存在性没有意义，明确告知「无权限」更有助于运营排查。
				r.Post("/collections", curations.Create)
				r.Patch("/collections/{slug}", curations.Update)
				r.Delete("/collections/{slug}", curations.Delete)
				r.Post("/collections/{slug}/items", curations.AddItem)
				r.Patch("/collections/{slug}/items/{itemId}", curations.ReorderItem)
				r.Delete("/collections/{slug}/items/{itemId}", curations.RemoveItem)

				// 开放 API 密钥的写操作：走普通会话认证 + CSRF。
				// 密钥本身是长期凭据，创建与撤销都必须确认是本站发起的操作。
				r.Post("/api-keys", openAPI.CreateKey)
				r.Delete("/api-keys/{id}", openAPI.RevokeKey)

				// 通知写操作：标记已读会改变个人数据，必须带 CSRF 令牌。
				r.Post("/notifications/{id}/read", notifications.MarkRead)
				r.Post("/notifications/read-all", notifications.MarkAllRead)
				r.Patch("/notification-prefs", notifications.UpdatePrefs)

				// 文件上传流水线（PRD V-01~V-07 / P-05）。
				// 对象存储不可用时（d.Storage == nil）不注册，避免出现必然失败的接口。
				if d.Storage != nil {
					files := handler.NewFileHandler(file.NewService(d.Pool, d.Storage))
					r.Post("/files/presign", files.Presign)
					r.Post("/files/{id}/complete", files.Complete)
				}
			})

			// 文件读取：只读，无需 CSRF。
			if d.Storage != nil {
				files := handler.NewFileHandler(file.NewService(d.Pool, d.Storage))
				r.Get("/files/{id}", files.Get)
				r.Get("/files/{id}/pages", files.Pages)
			}
		})

		// 公开读接口：SSR 与 SEO 的前提（PRD §7.4）。
		//
		// 服务端渲染时没有用户令牌，若这些路由仍挂在 Authenticate 下，渲染进程只能
		// 拿到 401，爬虫也就只能看到空壳页面。因此它们改用 OptionalAuthenticate：
		// 带了有效会话照常注入 viewerID（liked/bookmarked 等个性化字段正确），
		// 没有会话按匿名放行，由服务层的可见性谓词只放行 public 内容。
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.OptionalAuthenticate(tokens))
			r.Get("/papers", papers.List)
			r.Get("/papers/{id}", papers.Get)
			r.Get("/posts/{id}", contents.GetPost)
			r.Get("/posts/{id}/comments", contents.ListComments)

			// 群组公开读：未登录也能查看群资料与公开群列表，
			// myRole 在匿名时为 null。
			r.Get("/groups", groups.List)
			r.Get("/groups/{slug}", groups.Get)

			// 统一检索：公开可读，匿名也能搜；结果只来自已索引的公开实体。
			r.Get("/search", searches.Query)

			// 精选策展：公开可读，供 SSR 与分享链接使用。
			// 未上架 / 已下架的集合由服务层对非策展人返回 404。
			r.Get("/collections", curations.List)
			r.Get("/collections/{slug}", curations.Get)
		})

		// 公开个人主页：无需登录，供分享链接与搜索引擎抓取（PRD A-03 / §7.4）。
		// 令牌可选：有效会话会让响应附带 following，供关注按钮正确初始化。
		// 必须最后注册：chi 中静态段优先于参数段，/users/me 仍会命中上面的路由。
		r.With(appmiddleware.OptionalAuthenticate(tokens)).Get("/users/{handle}", users.GetPublic)
	})

	// 开放 API：独立前缀，用 X-API-Key 认证。
	//
	// 不挂在 /api/v1 下有两个原因：
	//   1. 版本节奏不同——开放 API 一旦发布就要长期兼容，内部 API 可以随产品迭代；
	//   2. 认证方式不同——Cookie 与 CSRF 的整套机制在这里都不适用。
	r.Route("/openapi/v1", func(r chi.Router) {
		r.Use(appmiddleware.APIKeyAuth(openapiSvc))
		r.Use(appmiddleware.LimitByAPIKey(openAPILimiter))

		r.With(appmiddleware.RequireScope(openapi.ScopeReadPapers)).Get("/papers", openAPI.ListPapers)
		r.With(appmiddleware.RequireScope(openapi.ScopeReadPapers)).Get("/papers/{id}", openAPI.GetPaper)
		r.With(appmiddleware.RequireScope(openapi.ScopeReadPosts)).Get("/posts", openAPI.ListPosts)
		r.With(appmiddleware.RequireScope(openapi.ScopeReadUsers)).Get("/users/{handle}", openAPI.GetUser)
	})

	return &http.Server{
		Addr:              d.Config.APIAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// contentIndexer 把 search.Service 适配成 content.Indexer。
//
// 适配放在 server 包：content / group 只认识自己的 Indexer 接口，
// 不需要 import search；依赖方向始终是「业务包 ← server → search」。
type contentIndexer struct{ service *search.Service }

func (a contentIndexer) Index(ctx context.Context, doc content.SearchDocument) error {
	return a.service.Index(ctx, search.IndexInput{
		EntityType: doc.EntityType,
		EntityID:   doc.EntityID,
		Title:      doc.Title,
		Body:       doc.Body,
	})
}

// groupIndexer 把 search.Service 适配成 group.Indexer。
type groupIndexer struct{ service *search.Service }

func (a groupIndexer) Index(ctx context.Context, doc group.SearchDocument) error {
	return a.service.Index(ctx, search.IndexInput{
		EntityType: doc.EntityType,
		EntityID:   doc.EntityID,
		Title:      doc.Title,
		Body:       doc.Body,
	})
}

// contentNotifier 把 notify.Service 适配成 content.Notifier。
// 与 Indexer 同理：content 只认识自己的接口，通知的具体实现由 server 注入。
type contentNotifier struct{ service *notify.Service }

func (a contentNotifier) Notify(ctx context.Context, input content.NotifyInput) error {
	return a.service.Notify(ctx, notify.CreateInput{
		UserID:     input.UserID,
		Type:       input.Type,
		ActorID:    input.ActorID,
		TargetType: input.TargetType,
		TargetID:   input.TargetID,
		Payload:    input.Payload,
	})
}
