package doc

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
	"github.com/thesis-shared/api/internal/storage"
)

// 文档图片的导入与读取。
//
// 权限模型与 files 表不同：files 是「上传者独占」，而文档里的图片
// 必须让所有能看这篇文档的人看到。因此图片单独存 doc_assets 表，
// 访问时校验「能否查看所属文档」，复用 doc 包已有的角色判定。
//
// 为什么不用预签名 URL 直接嵌进 HTML：
// 预签名有有效期，而文档是长期存在的——三个月后打开这篇文档，
// 图片链接已经失效。所以 HTML 里放的是稳定的
// /api/v1/documents/{id}/assets/{assetId}，由服务端每次请求时校验权限并转发内容。

const (
	// maxImageBytes 限制单张图片体积。导入时超过的图片会被跳过并提示。
	maxImageBytes = 8 << 20
	// imgPlaceholderPrefix 是正文里图片占位符的前缀。
	// 形如 NUL + "IMG:rId5" + NUL，由 ImportFile 在上传完成后替换成真实地址。
	// 用 NUL 包裹是为了不可能与正文里自然出现的文本冲突。
	imgPlaceholderPrefix = "\x00IMG:"
	imgPlaceholderSuffix = "\x00"
)

// imgPlaceholder 生成一张图片的占位符。
func imgPlaceholder(relID string) string {
	return imgPlaceholderPrefix + relID + imgPlaceholderSuffix
}

// imgPlaceholderRegexp 匹配正文里的图片占位符。
var imgPlaceholderRegexp = regexp.MustCompile(regexp.QuoteMeta(imgPlaceholderPrefix) + `([^` + regexp.QuoteMeta(imgPlaceholderSuffix) + `]+)` + regexp.QuoteMeta(imgPlaceholderSuffix))

// regexpEmbed 匹配正文里的 r:embed="rIdN"。
// Word 用这个属性把 <w:drawing> 关联到关系表里的图片条目。
var regexpEmbed = regexp.MustCompile(`r:embed="([^"]+)"`)

// assetQuerier 是图片相关查询的最小依赖。
// 与主 Querier 分开：现有测试替身不必为图片功能补方法。
type assetQuerier interface {
	CreateDocAsset(context.Context, db.CreateDocAssetParams) (db.DocAsset, error)
	GetDocAsset(context.Context, pgtype.UUID) (db.DocAsset, error)
}

// docxImage 是从 docx 压缩包里抽出来的一张图片。
type docxImage struct {
	// relID 是 document.xml 里 r:embed 引用的关系 ID。
	relID string
	data  []byte
	mime  string
	name  string
}

// docxRelationships 是 word/_rels/document.xml.rels 的投影。
type docxRelationships struct {
	Relationships []struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

// extractDocxImages 抽出 docx 里被正文引用的图片。
//
// 只处理 r:embed（内嵌图片），不处理 r:link（外链）——
// 外链图片依赖外部主机可用性，导入进来也未必打得开。
func extractDocxImages(archive *zip.Reader, bodyXML []byte) ([]docxImage, []string, error) {
	// 1) 读关系表，建立 relID → 包内路径。
	relsData, err := readZipFile(archive, "word/_rels/document.xml.rels")
	if err != nil {
		// 没有关系表说明文档没有图片，不是错误。
		return nil, nil, nil
	}
	var rels docxRelationships
	if err := xml.Unmarshal(relsData, &rels); err != nil {
		return nil, nil, fmt.Errorf("%w: 文档关系表无法解析", ErrValidation)
	}

	relTarget := make(map[string]string, len(rels.Relationships))
	for _, rel := range rels.Relationships {
		if strings.Contains(rel.Type, "/image") {
			target := strings.TrimPrefix(rel.Target, "/")
			if !strings.HasPrefix(target, "word/") {
				target = path.Join("word", target)
			}
			relTarget[rel.ID] = path.Clean(target)
		}
	}
	if len(relTarget) == 0 {
		return nil, nil, nil
	}

	// 2) 扫正文，找出所有 r:embed 引用。
	matches := regexpEmbed.FindAllSubmatch(bodyXML, -1)
	if len(matches) == 0 {
		return nil, nil, nil
	}

	var images []docxImage
	var warnings []string
	seen := make(map[string]struct{}, len(matches))

	for _, match := range matches {
		relID := string(match[1])
		if _, ok := seen[relID]; ok {
			continue
		}
		seen[relID] = struct{}{}

		target, ok := relTarget[relID]
		if !ok {
			continue
		}
		data, err := readZipFile(archive, target)
		if err != nil {
			warnings = append(warnings, "部分图片无法读取，已跳过："+path.Base(target))
			continue
		}
		if len(data) > maxImageBytes {
			warnings = append(warnings, fmt.Sprintf("图片 %s 超过 %d MB，已跳过", path.Base(target), maxImageBytes>>20))
			continue
		}
		mime := imageMIME(target, data)
		if mime == "" {
			warnings = append(warnings, "不支持的图片格式，已跳过："+path.Base(target))
			continue
		}
		images = append(images, docxImage{relID: relID, data: data, mime: mime, name: path.Base(target)})
	}
	return images, warnings, nil
}

func readZipFile(archive *zip.Reader, name string) ([]byte, error) {
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		return io.ReadAll(io.LimitReader(reader, maxImportBytes))
	}
	return nil, fmt.Errorf("未找到 %s", name)
}

// imageMIME 按扩展名与文件签名判断图片类型。
// 只接受浏览器能直接渲染的格式；EMF/WMF 这类矢量格式会返回空串被跳过。
func imageMIME(name string, data []byte) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	// 扩展名不可靠时看文件头。
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG")):
		return "image/png"
	case bytes.HasPrefix(data, []byte("\xff\xd8")):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF8")):
		return "image/gif"
	}
	return ""
}

// persistDocImages 把抽出的图片写入对象存储并登记到 doc_assets。
// 返回 relID → assetID 的映射，供正文替换 <img> 的地址。
func (s *Service) persistDocImages(
	ctx context.Context,
	userID, docID uuid.UUID,
	images []docxImage,
) (map[string]uuid.UUID, []string, error) {
	if len(images) == 0 {
		return nil, nil, nil
	}
	if s.store == nil {
		return nil, []string{"对象存储不可用，文档中的图片未导入"}, nil
	}

	mapping := make(map[string]uuid.UUID, len(images))
	var warnings []string

	for _, image := range images {
		assetID := uuid.Must(uuid.NewV7())
		key := fmt.Sprintf("doc-asset/%s/%s", docID, assetID.String())

		if err := s.store.Put(ctx, key, bytes.NewReader(image.data), int64(len(image.data)), image.mime); err != nil {
			// 单张图片失败不影响整篇导入：正文能进来比图片完整更重要。
			warnings = append(warnings, "图片上传失败，已跳过："+image.name)
			continue
		}
		name := image.name
		if _, err := s.assets.CreateDocAsset(ctx, db.CreateDocAssetParams{
			ID:           uuidParam(assetID),
			DocID:        uuidParam(docID),
			StorageKey:   key,
			Mime:         image.mime,
			SizeBytes:    int64(len(image.data)),
			OriginalName: &name,
			CreatedBy:    uuidParam(userID),
		}); err != nil {
			warnings = append(warnings, "图片登记失败，已跳过："+image.name)
			continue
		}
		mapping[image.relID] = assetID
	}
	return mapping, warnings, nil
}

// DocAsset 是一次图片读取的结果。
type DocAsset struct {
	Body        []byte
	ContentType string
}

// GetDocAsset 读取文档图片，校验查看者能否访问该文档。
//
// 权限：与查看文档一致（EffectiveRole 能读即可）。图片不是独立资源，
// 它只是文档的一部分——能看到文档就该能看到它的图。
func (s *Service) GetDocAsset(ctx context.Context, userID, docID, assetID uuid.UUID) (DocAsset, error) {
	// 先校验文档可见性，再查图片：否则无权用户能通过响应差异
	// 判断某个 assetID 是否存在。
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanView); err != nil {
		return DocAsset{}, err
	}
	if s.store == nil {
		return DocAsset{}, ErrNotFound
	}

	asset, err := s.assets.GetDocAsset(ctx, uuidParam(assetID))
	if err != nil {
		return DocAsset{}, ErrNotFound
	}
	// 图片必须属于这篇文档，防止拿 A 文档的 assetID 去读 B 文档的图。
	if uuid.UUID(asset.DocID.Bytes) != docID {
		return DocAsset{}, ErrNotFound
	}

	reader, err := s.store.Get(ctx, asset.StorageKey)
	if err != nil {
		return DocAsset{}, fmt.Errorf("读取图片失败: %w", err)
	}
	defer func() { _ = reader.Close() }()

	body, err := io.ReadAll(io.LimitReader(reader, maxImageBytes))
	if err != nil {
		return DocAsset{}, fmt.Errorf("读取图片失败: %w", err)
	}
	return DocAsset{Body: body, ContentType: asset.Mime}, nil
}

// SetStorage 注入对象存储。导入图片与读取图片都需要它。
func (s *Service) SetStorage(store *storage.Client) { s.store = store }
