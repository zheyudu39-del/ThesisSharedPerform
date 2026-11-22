// Package file 实现文件上传流水线：预签名直传、完成校验、PDF 页图转码与查询。
//
// 上传分三步（浏览器直传对象存储，不经过 API 服务器）：
//  1. Presign 校验类型与大小，写入 pending 记录并返回预签名 PUT 地址；
//  2. 浏览器 PUT 对象；
//  3. Complete 以对象存储的 Stat 结果为准校验大小，随后异步转码。
package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
	"github.com/thesis-shared/api/internal/search"
)

var (
	// ErrValidation 表示请求参数不合法（类型、大小、文件名、分页参数）。
	ErrValidation = errors.New("输入校验失败")
	// ErrFileNotFound 同时覆盖「不存在」与「无权查看」，避免探测他人文件。
	ErrFileNotFound = errors.New("文件不存在")
	// ErrUploadMissing 表示对象存储里没有对应对象，客户端跳过了上传步骤。
	ErrUploadMissing = errors.New("文件尚未上传")
	// ErrSizeMismatch 表示对象真实大小与客户端声称的不一致。
	ErrSizeMismatch = errors.New("文件大小与声明不一致")
	// ErrStorageUnavailable 表示对象存储未配置或不可用。
	ErrStorageUnavailable = errors.New("对象存储不可用")
)

const (
	// PresignTTL 预签名地址有效期 15 分钟（PRD §7.3）。
	PresignTTL = 15 * time.Minute
	// PageURLTTL 页图地址有效期 15 分钟。
	PageURLTTL = 15 * time.Minute
	// maxTranscodes 限制并发转码数，避免 pdftoppm 吃满 CPU。
	maxTranscodes = 2
	// maxErrorReason 限制写入 error_reason 的长度，避免把整段 stderr 塞进库。
	maxErrorReason = 500
)

// ObjectStore 是服务对对象存储的最小依赖，便于单元测试替身。
type ObjectStore interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	Stat(ctx context.Context, key string) (size int64, exists bool, err error)
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// TextIndexer 把「文件文本层」接到检索索引上。
//
// 单独声明接口而不是直接 import search：依赖方向始终是「业务包 ← server → search」，
// 与 content / group 的 Indexer 写法一致。未注入时文本层照常落库，只是不建索引。
type TextIndexer interface {
	// IndexFileText 为尚未关联论文的文件建索引（保留文本，暂不参与检索）。
	IndexFileText(ctx context.Context, fileID uuid.UUID, filename, text string) error
	// IndexPaperFullText 把文本层并入论文索引（PRD S-04）。
	IndexPaperFullText(ctx context.Context, paperID uuid.UUID, title, abstract, text string) error
}

// Querier 限定文件服务使用的查询，便于注入替身。
type Querier interface {
	CreateFile(context.Context, db.CreateFileParams) (db.File, error)
	GetFileByID(context.Context, pgtype.UUID) (db.File, error)
	MarkFileUploaded(context.Context, db.MarkFileUploadedParams) (db.File, error)
	MarkFileTranscoding(context.Context, pgtype.UUID) (db.File, error)
	MarkFileReady(context.Context, db.MarkFileReadyParams) (db.File, error)
	MarkFileFailed(context.Context, db.MarkFileFailedParams) (db.File, error)
	InsertFilePage(context.Context, db.InsertFilePageParams) error
	ListFilePages(context.Context, db.ListFilePagesParams) ([]db.FilePage, error)
	CountFilePages(context.Context, pgtype.UUID) (int64, error)
	UpdateFileStatus(context.Context, db.UpdateFileStatusParams) (db.File, error)
	SetFileTextLayerKey(context.Context, db.SetFileTextLayerKeyParams) error
	GetPaperForFile(context.Context, pgtype.UUID) (db.GetPaperForFileRow, error)
}

// File 是对外的文件 DTO，字段与前端契约严格一致。
type File struct {
	ID           uuid.UUID `json:"id"`
	OriginalName string    `json:"originalName"`
	Mime         string    `json:"mime"`
	Kind         string    `json:"kind"`
	SizeBytes    int64     `json:"sizeBytes"`
	Status       string    `json:"status"`
	PageCount    int       `json:"pageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	CoverURL     *string   `json:"coverUrl"`
	// ErrorReason 在 status=failed/rejected 时说明原因（转码超时、未装
	// LibreOffice、文件损坏等）。不返回的话用户只能看到一个失败状态，
	// 无法判断是自己的文件有问题还是部署缺依赖。
	ErrorReason *string `json:"errorReason"`
}

// Page 是单页页图 DTO。
type Page struct {
	PageNo int    `json:"pageNo"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// PresignInput 对应 POST /files/presign 的请求体。
type PresignInput struct {
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"sizeBytes"`
}

// PresignResult 是预签名直传的返回体。
type PresignResult struct {
	FileID    uuid.UUID         `json:"fileId"`
	UploadURL string            `json:"uploadUrl"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresIn int               `json:"expiresIn"`
}

// PagesResult 是分页页图查询结果。
type PagesResult struct {
	Pages     []Page `json:"pages"`
	Total     int64  `json:"total"`
	PageCount int    `json:"pageCount"`
}

// Service 实现文件领域逻辑。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	store   ObjectStore
	indexer TextIndexer
	sem     chan struct{}
	now     func() time.Time
}

// NewService 创建文件服务。store 为 nil 时接口仍可注册，但会返回 ErrStorageUnavailable。
func NewService(pool *pgxpool.Pool, store ObjectStore) *Service {
	return &Service{
		pool:    pool,
		queries: db.New(pool),
		store:   store,
		sem:     make(chan struct{}, maxTranscodes),
		now:     time.Now,
	}
}

// NewServiceWithDeps 用于测试注入替身。
func NewServiceWithDeps(q Querier, store ObjectStore) *Service {
	return &Service{
		queries: q,
		store:   store,
		sem:     make(chan struct{}, maxTranscodes),
		now:     time.Now,
	}
}

// SetTextIndexer 注入文本层索引实现；不注入时只落文本层、不建索引。
func (s *Service) SetTextIndexer(indexer TextIndexer) { s.indexer = indexer }

// Presign 校验上传意图并生成预签名 PUT 地址。
func (s *Service) Presign(ctx context.Context, userID uuid.UUID, input PresignInput) (PresignResult, error) {
	if s.store == nil {
		return PresignResult{}, ErrStorageUnavailable
	}

	name := sanitizeFilename(input.Filename)
	if name == "" {
		return PresignResult{}, fmt.Errorf("%w: 文件名不能为空", ErrValidation)
	}
	kind, ok := MIMEKind(input.Mime)
	if !ok {
		return PresignResult{}, fmt.Errorf("%w: %s", ErrUnsupportedType, normalizeMIME(input.Mime))
	}
	if input.SizeBytes <= 0 {
		return PresignResult{}, fmt.Errorf("%w: 文件大小必须大于 0", ErrValidation)
	}
	if input.SizeBytes > MaxUploadBytes {
		return PresignResult{}, fmt.Errorf("%w: 文件不能超过 %d MB", ErrValidation, MaxUploadBytes>>20)
	}

	fileID := uuid.Must(uuid.NewV7())
	mime := normalizeMIME(input.Mime)
	key := originalKey(fileID, safeExt(name, mime), s.now())

	record, err := s.queries.CreateFile(ctx, db.CreateFileParams{
		ID:           uuidParam(fileID),
		OwnerID:      uuidParam(userID),
		StorageKey:   key,
		OriginalName: name,
		Mime:         mime,
		Kind:         kind,
		SizeBytes:    input.SizeBytes,
	})
	if err != nil {
		return PresignResult{}, fmt.Errorf("创建文件记录: %w", err)
	}

	uploadURL, err := s.store.PresignPut(ctx, record.StorageKey, mime, PresignTTL)
	if err != nil {
		return PresignResult{}, err
	}

	return PresignResult{
		FileID:    uuid.UUID(record.ID.Bytes),
		UploadURL: uploadURL,
		Method:    "PUT",
		Headers:   map[string]string{"Content-Type": mime},
		ExpiresIn: int(PresignTTL / time.Second),
	}, nil
}

// Complete 以对象存储的真实状态为准完成上传，并触发后台转码。
func (s *Service) Complete(ctx context.Context, userID, fileID uuid.UUID, sizeBytes int64) (File, error) {
	if s.store == nil {
		return File{}, ErrStorageUnavailable
	}
	record, err := s.ownedFile(ctx, userID, fileID)
	if err != nil {
		return File{}, err
	}

	switch record.Status {
	case db.FileStatusReady:
		return s.toFile(ctx, record), nil
	case db.FileStatusTranscoding:
		// 重复调用：转码已在进行，直接返回当前状态供前端轮询。
		return s.toFile(ctx, record), nil
	case db.FileStatusRejected:
		return File{}, fmt.Errorf("%w: 文件未通过校验", ErrValidation)
	}

	// 真实大小与存在性只能信对象存储，客户端声称的 sizeBytes 仅用于比对。
	actual, exists, err := s.store.Stat(ctx, record.StorageKey)
	if err != nil {
		return File{}, err
	}
	if !exists {
		return File{}, ErrUploadMissing
	}
	if actual <= 0 {
		return File{}, fmt.Errorf("%w: 对象为空", ErrUploadMissing)
	}
	if actual > MaxUploadBytes {
		return File{}, fmt.Errorf("%w: 文件超过 %d MB", ErrValidation, MaxUploadBytes>>20)
	}
	if sizeBytes > 0 && sizeBytes != actual {
		return File{}, fmt.Errorf("%w: 声明 %d 字节，实际 %d 字节", ErrSizeMismatch, sizeBytes, actual)
	}

	if record.Status == db.FileStatusPending || record.Status == db.FileStatusUploading {
		updated, err := s.queries.MarkFileUploaded(ctx, db.MarkFileUploadedParams{
			ID:        uuidParam(fileID),
			SizeBytes: actual,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// 并发完成：另一个请求已推进状态，读回最新记录即可。
				return s.reload(ctx, userID, fileID)
			}
			return File{}, fmt.Errorf("更新文件状态: %w", err)
		}
		record = updated
	} else if record.Status == db.FileStatusFailed {
		// 失败重试：状态机会在后台重新进入 transcoding。
		record.SizeBytes = actual
	} else {
		record.SizeBytes = actual
	}

	// Office 文档与 PDF 都需要页图才能预览，一起进转码流水线；
	// Office 多一步 LibreOffice 转 PDF（见 office.go）。
	// 图片、Markdown 等无需转码，直接置 ready 由前端按类型展示。
	if !needsTranscode(string(record.Kind)) {
		ready, err := s.queries.UpdateFileStatus(ctx, db.UpdateFileStatusParams{
			ID:     uuidParam(fileID),
			Status: db.FileStatusReady,
		})
		if err != nil {
			return File{}, fmt.Errorf("更新文件状态: %w", err)
		}
		return s.toFile(ctx, ready), nil
	}

	// 先进入 transcoding 再返回，前端第一次轮询就能看到正确状态。
	transcoding, err := s.queries.MarkFileTranscoding(ctx, uuidParam(fileID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.reload(ctx, userID, fileID)
		}
		return File{}, fmt.Errorf("更新文件状态: %w", err)
	}

	s.startTranscode(fileID, record.StorageKey, string(record.Kind))
	return s.toFile(ctx, transcoding), nil
}

// Get 查询自己上传的文件。
func (s *Service) Get(ctx context.Context, userID, fileID uuid.UUID) (File, error) {
	record, err := s.ownedFile(ctx, userID, fileID)
	if err != nil {
		return File{}, err
	}
	return s.toFile(ctx, record), nil
}

// Pages 分页返回页图的预签名地址。
func (s *Service) Pages(ctx context.Context, userID, fileID uuid.UUID, page, size int32) (PagesResult, error) {
	if page < 1 {
		return PagesResult{}, fmt.Errorf("%w: page 须大于等于 1", ErrValidation)
	}
	if size < 1 || size > 50 {
		return PagesResult{}, fmt.Errorf("%w: size 须为 1 到 50", ErrValidation)
	}

	record, err := s.ownedFile(ctx, userID, fileID)
	if err != nil {
		return PagesResult{}, err
	}

	rows, err := s.queries.ListFilePages(ctx, db.ListFilePagesParams{
		FileID: uuidParam(fileID),
		Limit:  size,
		Offset: (page - 1) * size,
	})
	if err != nil {
		return PagesResult{}, fmt.Errorf("查询页图: %w", err)
	}
	total, err := s.queries.CountFilePages(ctx, uuidParam(fileID))
	if err != nil {
		return PagesResult{}, fmt.Errorf("统计页图: %w", err)
	}

	pages := make([]Page, 0, len(rows))
	for _, row := range rows {
		url, err := s.presignGet(ctx, row.ImageKey)
		if err != nil {
			return PagesResult{}, err
		}
		pages = append(pages, Page{
			PageNo: int(row.PageNo),
			URL:    url,
			Width:  int(row.Width),
			Height: int(row.Height),
		})
	}

	pageCount := int(total)
	if record.PageCount != nil {
		pageCount = int(*record.PageCount)
	}
	return PagesResult{Pages: pages, Total: total, PageCount: pageCount}, nil
}

// ownedFile 读取文件并校验归属。不存在与无权访问都返回 ErrFileNotFound。
func (s *Service) ownedFile(ctx context.Context, userID, fileID uuid.UUID) (db.File, error) {
	record, err := s.queries.GetFileByID(ctx, uuidParam(fileID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.File{}, ErrFileNotFound
		}
		return db.File{}, fmt.Errorf("查询文件: %w", err)
	}
	if uuid.UUID(record.OwnerID.Bytes) != userID {
		return db.File{}, ErrFileNotFound
	}
	return record, nil
}

func (s *Service) reload(ctx context.Context, userID, fileID uuid.UUID) (File, error) {
	record, err := s.ownedFile(ctx, userID, fileID)
	if err != nil {
		return File{}, err
	}
	return s.toFile(ctx, record), nil
}

func (s *Service) toFile(ctx context.Context, record db.File) File {
	out := File{
		ID:           uuid.UUID(record.ID.Bytes),
		OriginalName: record.OriginalName,
		Mime:         record.Mime,
		Kind:         string(record.Kind),
		SizeBytes:    record.SizeBytes,
		Status:       string(record.Status),
		CreatedAt:    record.CreatedAt.Time,
		ErrorReason:  record.ErrorReason,
	}
	if record.PageCount != nil {
		out.PageCount = int(*record.PageCount)
	}
	if record.CoverKey != nil && *record.CoverKey != "" {
		if url, err := s.presignGet(ctx, *record.CoverKey); err == nil {
			out.CoverURL = &url
		}
	}
	return out
}

func (s *Service) presignGet(ctx context.Context, key string) (string, error) {
	if s.store == nil {
		return "", ErrStorageUnavailable
	}
	return s.store.PresignGet(ctx, key, PageURLTTL)
}

// startTranscode 在后台转码。必须使用 context.Background()：
// 请求 context 在 Complete 返回后即被取消，会让下载与上传全部中断。
func (s *Service) startTranscode(fileID uuid.UUID, storageKey, kind string) {
	go func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		if err := s.transcode(ctx, fileID, storageKey, kind); err != nil {
			slog.Error("文件转码失败", "err", err, "fileId", fileID, "kind", kind)
			reason := truncate(err.Error(), maxErrorReason)
			if _, markErr := s.queries.MarkFileFailed(context.Background(), db.MarkFileFailedParams{
				ID:          uuidParam(fileID),
				ErrorReason: &reason,
			}); markErr != nil {
				slog.Error("写入转码失败状态出错", "err", markErr, "fileId", fileID)
			}
		}
	}()
}

// transcode 下载原文件、渲染页图、上传页图并回写数据库。
//
// PDF 直接渲染；Office 文档先经 LibreOffice 转成 PDF 再走同一套渲染，
// 两条路径的产出一致（页图 + 文本层），前端无需区分。
func (s *Service) transcode(ctx context.Context, fileID uuid.UUID, storageKey, kind string) error {
	workDir, err := os.MkdirTemp("", "thesis-pdf-*")
	if err != nil {
		return fmt.Errorf("创建临时目录: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	// 保留原始扩展名：LibreOffice 按扩展名判断文档类型，
	// 存成 .pdf 会让它把 docx 当 PDF 打开而失败。
	srcPath := path.Join(workDir, "source"+sourceExt(kind))
	if err := s.download(ctx, storageKey, srcPath); err != nil {
		return err
	}

	pdfPath := srcPath
	if isOfficeKind(kind) {
		pdfPath, err = ConvertToPDF(ctx, srcPath, workDir)
		if err != nil {
			return err
		}
	}

	pages, err := RenderPDF(ctx, pdfPath, workDir, PageWidth)
	if err != nil {
		return err
	}

	// 文本层与页图互不依赖：提取失败（扫描件、滤镜不支持）只影响检索，
	// 不能让整份文件变成 failed，因此这里吞掉错误、只记日志。
	// 放在 MarkFileReady 之前：就绪后前端可能立刻检索，文本层应先就位。
	s.extractTextLayer(ctx, fileID, pdfPath)

	var coverKey string
	for _, p := range pages {
		key := PageImageKey(fileID, p.PageNo)
		if err := s.uploadFile(ctx, key, p.Path, "image/png"); err != nil {
			return err
		}
		if err := s.queries.InsertFilePage(ctx, db.InsertFilePageParams{
			FileID:   uuidParam(fileID),
			PageNo:   int32(p.PageNo),
			ImageKey: key,
			Width:    int32(p.Width),
			Height:   int32(p.Height),
		}); err != nil {
			return fmt.Errorf("写入页图记录: %w", err)
		}
		if p.PageNo == 1 {
			coverKey = key
		}
	}

	pageCount := int32(len(pages))
	_, err = s.queries.MarkFileReady(ctx, db.MarkFileReadyParams{
		ID:        uuidParam(fileID),
		PageCount: &pageCount,
		CoverKey:  &coverKey,
	})
	if err != nil {
		return fmt.Errorf("更新就绪状态: %w", err)
	}
	return nil
}

// TextLayerKey 返回文本层的对象存储 key：text-layer/{fileId}.txt。
// 与 files.text_layer_key 的取值约定一致。
func TextLayerKey(fileID uuid.UUID) string {
	return fmt.Sprintf("text-layer/%s.txt", fileID)
}

// extractTextLayer 从 PDF 提取文本层，写入对象存储与 files.text_layer_key，
// 并把文本接入检索索引（PRD S-04）。
//
// 失败语义：文本层是「增强项」，不是文件的必需产物。扫描件本来就提取不到文本，
// 因此这里只记日志、不返回错误，绝不把文件状态改成 failed。
func (s *Service) extractTextLayer(ctx context.Context, fileID uuid.UUID, pdfPath string) {
	f, err := os.Open(pdfPath)
	if err != nil {
		slog.Warn("打开 PDF 提取文本层失败", "err", err, "fileId", fileID)
		return
	}
	defer func() { _ = f.Close() }()

	text, err := search.ExtractText(f)
	if err != nil {
		slog.Warn("PDF 文本层提取失败", "err", err, "fileId", fileID)
		return
	}
	if strings.TrimSpace(text) == "" {
		// 扫描版 PDF：没有文本层，索引也无从谈起。
		slog.Info("PDF 没有可提取的文本层（可能是扫描件）", "fileId", fileID)
		return
	}

	key := TextLayerKey(fileID)
	data := []byte(text)
	if err := s.store.Put(ctx, key, strings.NewReader(text), int64(len(data)), "text/plain; charset=utf-8"); err != nil {
		slog.Warn("上传文本层失败", "err", err, "fileId", fileID)
		return
	}
	if err := s.setTextLayerKey(ctx, fileID, key); err != nil {
		slog.Warn("记录文本层 key 失败", "err", err, "fileId", fileID)
	}

	s.indexTextLayer(ctx, fileID, text)
}

// indexTextLayer 把文本层接到检索：论文关联的 PDF 并入论文索引，
// 未关联的按文件单独保留（见 search.IndexFileText 的说明）。
func (s *Service) indexTextLayer(ctx context.Context, fileID uuid.UUID, text string) {
	if s.indexer == nil {
		return
	}
	paperID, title, abstract, ok, err := s.paperForFile(ctx, fileID)
	if err != nil {
		slog.Warn("查询文件关联论文失败", "err", err, "fileId", fileID)
	}
	if ok {
		if err := s.indexer.IndexPaperFullText(ctx, paperID, title, abstract, text); err != nil {
			slog.Warn("索引论文全文失败", "err", err, "fileId", fileID, "paperId", paperID)
		}
		return
	}
	if err := s.indexer.IndexFileText(ctx, fileID, "", text); err != nil {
		slog.Warn("索引文件文本层失败", "err", err, "fileId", fileID)
	}
}

// setTextLayerKey 把文本层 key 写回 files。
func (s *Service) setTextLayerKey(ctx context.Context, fileID uuid.UUID, key string) error {
	if err := s.queries.SetFileTextLayerKey(ctx, db.SetFileTextLayerKeyParams{
		ID:           uuidParam(fileID),
		TextLayerKey: &key,
	}); err != nil {
		return fmt.Errorf("更新文本层 key: %w", err)
	}
	return nil
}

// paperForFile 查出该文件被哪篇论文引用（papers.pdf_file_id）。
// 返回 ok=false 表示没有论文引用它。
func (s *Service) paperForFile(ctx context.Context, fileID uuid.UUID) (uuid.UUID, string, string, bool, error) {
	row, err := s.queries.GetPaperForFile(ctx, uuidParam(fileID))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", "", false, nil
	}
	if err != nil {
		return uuid.Nil, "", "", false, err
	}
	abstract := ""
	if row.Abstract != nil {
		abstract = *row.Abstract
	}
	return row.PaperID.Bytes, row.Title, abstract, true, nil
}

func (s *Service) download(ctx context.Context, key, dest string) error {
	reader, err := s.store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("下载原文件: %w", err)
	}
	defer func() { _ = reader.Close() }()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		return fmt.Errorf("写入临时文件: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	return nil
}

func (s *Service) uploadFile(ctx context.Context, key, src, contentType string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开页图: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("读取页图大小: %w", err)
	}
	return s.store.Put(ctx, key, f, info.Size(), contentType)
}

// PageImageKey 返回页图的对象存储 key：preview/{fileId}/page-{n}.png。
func PageImageKey(fileID uuid.UUID, pageNo int) string {
	return fmt.Sprintf("preview/%s/page-%d.png", fileID, pageNo)
}

// originalKey 返回原文件的 key：original/{yyyy}/{mm}/{fileId}{ext}（架构 §5.1）。
func originalKey(fileID uuid.UUID, ext string, now time.Time) string {
	return fmt.Sprintf("original/%04d/%02d/%s%s", now.Year(), int(now.Month()), fileID, ext)
}

func uuidParam(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	// 去掉客户端可能带上的路径部分（Windows 反斜杠一并处理）。
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "." || name == "/" {
		return ""
	}
	if !utf8.ValidString(name) {
		return ""
	}
	// 控制字符会被日志与响应头误解析，直接剔除。
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 255 {
		name = string([]rune(name)[:255])
	}
	return name
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
