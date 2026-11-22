package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Office 文档 → PDF（PRD V-03）。
//
// 为什么不自己解析 docx/pptx/xlsx：
// 这三种格式的版式（浮动对象、分栏、母版、图表）没有任何 Go 库能还原到
// 「版式无明显错位」的程度，而 V-03 的验收标准正是版式保真。LibreOffice
// 的 headless 转换是这类需求的事实标准，代价是必须在部署环境里装一份。
//
// 转换产物只用来生成页图与文本层，不落回对象存储：原文件才是用户的资产，
// PDF 只是预览的中间产物，重新转码即可再生。

const (
	// officeConvertTimeout 是单次 LibreOffice 转换的超时。
	// 冷启动本身就要 5~10 秒，大文档再叠加，留足余量。
	officeConvertTimeout = 5 * time.Minute
)

var (
	// ErrOfficeConverterMissing 表示部署环境没有安装 LibreOffice。
	ErrOfficeConverterMissing = errors.New("未找到 LibreOffice（soffice），无法转换 Office 文档")
	// ErrOfficeConvertFailed 表示 LibreOffice 存在但转换本身失败。
	// 与 ErrRenderFailed 分开：前者是「文档/转换器的问题」，后者是
	// 「PDF 渲染的问题」，混在一起会让排查方向跑偏。
	ErrOfficeConvertFailed = errors.New("Office 文档转换失败")
)

// sofficeCandidates 是查找 soffice 的顺序。
// 先看 PATH，再退回各平台的标准安装位置——Windows 上 winget 装的
// LibreOffice 默认不会进 PATH，只靠 PATH 会误报「未安装」。
func sofficeCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{
			"soffice",
			`C:\Program Files\LibreOffice\program\soffice.exe`,
			`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
		}
	}
	return []string{"soffice", "libreoffice"}
}

// findSoffice 返回可用的 soffice 路径。
func findSoffice() (string, error) {
	for _, candidate := range sofficeCandidates() {
		path, err := exec.LookPath(candidate)
		if err == nil {
			return path, nil
		}
	}
	return "", ErrOfficeConverterMissing
}

// ConvertToPDF 用 LibreOffice 把 Office 文档转成 PDF，返回产物路径。
//
// workDir 里会同时创建：
//   - out/      转换输出目录
//   - profile/  独立的 LibreOffice 用户配置目录
//
// profile 必须隔离：LibreOffice 用文件锁保护用户配置，多个进程共用默认
// 配置目录时，第二个并发转换会直接失败或卡住。每次转换给一个临时 profile
// 是最省事也最可靠的隔离方式。
func ConvertToPDF(ctx context.Context, srcPath, workDir string) (string, error) {
	soffice, err := findSoffice()
	if err != nil {
		return "", err
	}

	outDir := filepath.Join(workDir, "converted")
	profileDir := filepath.Join(workDir, "profile")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("创建转换输出目录: %w", err)
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return "", fmt.Errorf("创建转换配置目录: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, officeConvertTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, soffice,
		"--headless",
		"--norestore",
		"--nolockcheck",
		"--nodefault",
		// UserInstallation 要的是 file URL，Windows 路径需转成斜杠形式。
		"-env:UserInstallation=file:///"+filepath.ToSlash(profileDir),
		"--convert-to", "pdf:writer_pdf_Export",
		"--outdir", outDir,
		srcPath,
	)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: 转换超时", ErrOfficeConvertFailed)
		}
		return "", fmt.Errorf("%w: %v: %s",
			ErrOfficeConvertFailed, err, truncateRunes(strings.TrimSpace(stderr.String()), 300))
	}

	// LibreOffice 的退出码不总是可靠（某些版本转换失败仍返回 0），
	// 所以以产物是否存在为准。
	pdfPath := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))+".pdf")
	if info, err := os.Stat(pdfPath); err != nil || info.Size() == 0 {
		return "", fmt.Errorf("%w: 没有产出 PDF（可能是文件损坏或格式不受支持）", ErrOfficeConvertFailed)
	}
	return pdfPath, nil
}

// truncateRunes 按字符（而非字节）截断。
// 错误信息里常带中文文件名，按字节切会把一个汉字劈成半个非法 UTF-8。
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// isOfficeKind 报告该类型是否需要先经 LibreOffice 转 PDF。
func isOfficeKind(kind string) bool {
	switch kind {
	case "office_doc", "office_slide", "office_sheet":
		return true
	}
	return false
}

// needsTranscode 报告该类型是否需要走转码流水线才能预览。
func needsTranscode(kind string) bool {
	return kind == "pdf" || isOfficeKind(kind)
}

// sourceExt 返回下载到临时目录时应使用的扩展名。
//
// 为什么不统一存成 source.pdf：LibreOffice 完全靠扩展名判断文档类型，
// 把一个 .docx 存成 .pdf 会让它按 PDF 解析，直接报「source file could
// not be loaded」。PDF 本身也用 .pdf，因此这个映射对两条路径都成立。
func sourceExt(kind string) string {
	switch kind {
	case "pdf":
		return ".pdf"
	case "office_doc":
		return ".docx"
	case "office_slide":
		return ".pptx"
	case "office_sheet":
		return ".xlsx"
	default:
		return ".bin"
	}
}
