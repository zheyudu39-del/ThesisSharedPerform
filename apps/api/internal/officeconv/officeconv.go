// Package officeconv 封装 LibreOffice headless 文档转换。
//
// 单独成包而不是放在 file 里：上传转码（file 包）和文档导出（doc 包）
// 都需要「把 Office 文档转成 PDF」，而 file 与 doc 之间已有依赖方向，
// 让任何一方依赖另一方都会成环。这里只做一件事：调 soffice，不涉及
// 存储、数据库或业务规则。
package officeconv

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

// convertTimeout 是单次转换的超时。冷启动本身就要 5~10 秒，
// 大文档再叠加，留足余量。
const convertTimeout = 5 * time.Minute

var (
	// ErrMissing 表示部署环境没有安装 LibreOffice。
	ErrMissing = errors.New("未找到 LibreOffice（soffice）")
	// ErrConvertFailed 表示 LibreOffice 存在但转换本身失败。
	ErrConvertFailed = errors.New("Office 文档转换失败")
)

// SOfficeFilter 是 --convert-to 的过滤器取值。
type SOfficeFilter string

const (
	// FilterWriter 用于文字处理文档（docx / odt / rtf / txt）。
	FilterWriter SOfficeFilter = "pdf:writer_pdf_Export"
	// FilterCalc 用于电子表格（xlsx / ods / csv）。
	FilterCalc SOfficeFilter = "pdf:calc_pdf_Export"
	// FilterImpress 用于演示文稿（pptx / odp）。
	FilterImpress SOfficeFilter = "pdf:impress_pdf_Export"
)

// candidates 是查找 soffice 的顺序。
// 先看 PATH，再退回各平台的标准安装位置——Windows 上 winget 装的
// LibreOffice 默认不会进 PATH，只靠 PATH 会误报「未安装」。
func candidates() []string {
	if runtime.GOOS == "windows" {
		return []string{
			"soffice",
			`C:\Program Files\LibreOffice\program\soffice.exe`,
			`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
		}
	}
	return []string{"soffice", "libreoffice"}
}

// Available 报告当前环境是否安装了可用的 LibreOffice。
func Available() bool {
	_, err := find()
	return err == nil
}

func find() (string, error) {
	for _, candidate := range candidates() {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", ErrMissing
}

// ToPDF 把 srcPath 转成 PDF 并返回产物路径。
//
// workDir 里会创建：
//   - converted/  转换输出目录
//   - profile/    独立的 LibreOffice 用户配置目录
//
// profile 必须隔离：LibreOffice 用文件锁保护用户配置，多个进程共用默认
// 配置目录时，第二个并发转换会直接失败或卡住。每次转换给一个临时 profile
// 是最省事也最可靠的隔离方式。
//
// 扩展名必须正确：LibreOffice 完全靠扩展名判断文档类型，把 docx 存成
// .pdf 会让它按 PDF 解析并报「source file could not be loaded」。
func ToPDF(ctx context.Context, srcPath, workDir string, filter SOfficeFilter) (string, error) {
	soffice, err := find()
	if err != nil {
		return "", err
	}
	if filter == "" {
		filter = FilterWriter
	}

	outDir := filepath.Join(workDir, "converted")
	profileDir := filepath.Join(workDir, "profile")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("创建转换输出目录: %w", err)
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return "", fmt.Errorf("创建转换配置目录: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, soffice,
		"--headless",
		"--norestore",
		"--nolockcheck",
		"--nodefault",
		// UserInstallation 要的是 file URL，Windows 路径需转成斜杠形式。
		"-env:UserInstallation=file:///"+filepath.ToSlash(profileDir),
		"--convert-to", string(filter),
		"--outdir", outDir,
		srcPath,
	)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: 转换超时", ErrConvertFailed)
		}
		return "", fmt.Errorf("%w: %v: %s", ErrConvertFailed, err, Trim(stderr.String(), 300))
	}

	// LibreOffice 的退出码不总是可靠（某些版本转换失败仍返回 0），
	// 所以以产物是否存在为准。
	pdfPath := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))+".pdf")
	if info, err := os.Stat(pdfPath); err != nil || info.Size() == 0 {
		return "", fmt.Errorf("%w: 没有产出 PDF（可能是文件损坏或格式不受支持）", ErrConvertFailed)
	}
	return pdfPath, nil
}

// Trim 按字符（而非字节）截断。
// 错误信息里常带中文文件名，按字节切会把一个汉字劈成半个非法 UTF-8。
func Trim(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
