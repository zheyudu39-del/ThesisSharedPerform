package file

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 页图规格：宽固定 1200（技术架构 §4.1 / PRD V-01 首屏清晰度）。
const (
	PageWidth = 1200
	// MaxPages 限制单次转码页数，防止超大 PDF 把磁盘和 CPU 吃满。
	MaxPages = 200
	// renderTimeout 是单次 pdftoppm 的总超时。
	renderTimeout = 5 * time.Minute
	// probeTimeout 是 pdfinfo 的超时（损坏文件可能卡住解析）。
	probeTimeout = 30 * time.Second
)

var (
	// ErrTooManyPages 表示 PDF 页数超过 MaxPages。
	ErrTooManyPages = errors.New("PDF 页数超过上限")
	// ErrRenderFailed 表示外部工具不可用或转码失败。
	ErrRenderFailed = errors.New("PDF 转页图失败")
)

// PageInfo 描述一页已生成的页图。
type PageInfo struct {
	PageNo int
	Path   string
	Width  int
	Height int
}

// PDFInfo 是 pdfinfo 探测到的元信息。
type PDFInfo struct {
	PageCount int
	// PageWidthPt / PageHeightPt 是第一页的物理尺寸（pt），用于按比例算页高。
	PageWidthPt  float64
	PageHeightPt float64
}

// RenderPDF 调用 pdftoppm 把 PDF 逐页渲染成 PNG，返回按页码排序的页图列表。
// outDir 必须已存在；输出文件名为 page-{n}.png。
func RenderPDF(ctx context.Context, pdfPath, outDir string, width int) ([]PageInfo, error) {
	if width <= 0 {
		width = PageWidth
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建页图目录: %w", err)
	}

	info, err := ProbePDF(ctx, pdfPath)
	if err != nil {
		return nil, err
	}
	if info.PageCount > MaxPages {
		return nil, fmt.Errorf("%w: %d 页，最多 %d 页", ErrTooManyPages, info.PageCount, MaxPages)
	}
	if info.PageCount <= 0 {
		return nil, fmt.Errorf("%w: PDF 没有可渲染的页面", ErrRenderFailed)
	}

	prefix := filepath.Join(outDir, "page")
	args := buildPdftoppmArgs(pdfPath, prefix, width)

	runCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "pdftoppm", args...)
	// 把输出限制在内存里便于错误上报；正常输出很短。
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: 转码超时", ErrRenderFailed)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: 未找到 pdftoppm，请安装 poppler-utils", ErrRenderFailed)
		}
		return nil, fmt.Errorf("%w: %v: %s", ErrRenderFailed, err, strings.TrimSpace(stderr.String()))
	}

	files, err := collectPages(prefix)
	if err != nil {
		return nil, err
	}
	if len(files) != info.PageCount {
		return nil, fmt.Errorf("%w: 期望 %d 页，实际生成 %d 页", ErrRenderFailed, info.PageCount, len(files))
	}

	height := scaledHeight(width, info.PageWidthPt, info.PageHeightPt)
	pages := make([]PageInfo, 0, len(files))
	for _, f := range files {
		pages = append(pages, PageInfo{PageNo: f.pageNo, Path: f.path, Width: width, Height: height})
	}
	return pages, nil
}

// ProbePDF 用 pdfinfo 读取页数与第一页尺寸。
func ProbePDF(ctx context.Context, pdfPath string) (PDFInfo, error) {
	runCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "pdfinfo", pdfPath)
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return PDFInfo{}, fmt.Errorf("%w: 未找到 pdfinfo，请安装 poppler-utils", ErrRenderFailed)
		}
		var exitErr *exec.ExitError
		msg := ""
		if errors.As(err, &exitErr) {
			msg = strings.TrimSpace(string(exitErr.Stderr))
		}
		return PDFInfo{}, fmt.Errorf("%w: pdfinfo: %v %s", ErrRenderFailed, err, msg)
	}
	return parsePDFInfo(string(out))
}

var pageSizeRe = regexp.MustCompile(`(?m)^Page size:\s+([0-9.]+)\s+x\s+([0-9.]+)`)

// parsePDFInfo 解析 pdfinfo 的文本输出。
// 只依赖 Pages 与 Page size 两行，其余字段随版本变化，不做解析。
func parsePDFInfo(out string) (PDFInfo, error) {
	var info PDFInfo
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Pages:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err == nil {
				info.PageCount = n
			}
		}
	}
	if info.PageCount <= 0 {
		return PDFInfo{}, fmt.Errorf("%w: 无法读取 PDF 页数", ErrRenderFailed)
	}

	// 页尺寸缺失（极少数 PDF）时按 A4 处理，页图仍可生成，只是高度略偏。
	info.PageWidthPt, info.PageHeightPt = 595.276, 841.89
	if m := pageSizeRe.FindStringSubmatch(out); m != nil {
		w, errW := strconv.ParseFloat(m[1], 64)
		h, errH := strconv.ParseFloat(m[2], 64)
		if errW == nil && errH == nil && w > 0 && h > 0 {
			info.PageWidthPt, info.PageHeightPt = w, h
		}
	}
	return info, nil
}

// buildPdftoppmArgs 构造 pdftoppm 参数。
// -scale-to-x 固定宽、-scale-to-y -1 保持纵横比，比换算 DPI 更稳。
func buildPdftoppmArgs(pdfPath, prefix string, width int) []string {
	return []string{
		"-png",
		"-scale-to-x", strconv.Itoa(width),
		"-scale-to-y", "-1",
		"-r", "150",
		pdfPath,
		prefix,
	}
}

// scaledHeight 按第一页的宽高比计算输出像素高度。
func scaledHeight(width int, pageWidthPt, pageHeightPt float64) int {
	if pageWidthPt <= 0 {
		return width
	}
	return int(math.Round(float64(width) * pageHeightPt / pageWidthPt))
}

type renderedPage struct {
	pageNo int
	path   string
}

// collectPages 收集 pdftoppm 输出并按页码排序。
// pdftoppm 的补零位数随总页数变化（page-1 或 page-01），所以按数字解析而非字符串排序。
func collectPages(prefix string) ([]renderedPage, error) {
	matches, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return nil, fmt.Errorf("扫描页图: %w", err)
	}
	pages := make([]renderedPage, 0, len(matches))
	for _, m := range matches {
		base := strings.TrimSuffix(filepath.Base(m), ".png")
		idx := strings.LastIndexByte(base, '-')
		if idx < 0 {
			continue
		}
		n, err := strconv.Atoi(base[idx+1:])
		if err != nil {
			continue
		}
		pages = append(pages, renderedPage{pageNo: n, path: m})
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].pageNo < pages[j].pageNo })
	return pages, nil
}
