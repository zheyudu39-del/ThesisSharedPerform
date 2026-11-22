package file

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/thesis-shared/api/internal/officeconv"
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

// 这些错误是对 officeconv 错误的投影，让调用方不必同时 import 两个包，
// 也让 handler 的错误映射保持在一个包内。
var (
	// ErrOfficeConverterMissing 表示部署环境没有安装 LibreOffice。
	ErrOfficeConverterMissing = officeconv.ErrMissing
	// ErrOfficeConvertFailed 表示 LibreOffice 存在但转换本身失败。
	ErrOfficeConvertFailed = officeconv.ErrConvertFailed
)

// ConvertToPDF 把 Office 文档转成 PDF，返回产物路径。
func ConvertToPDF(ctx context.Context, srcPath, workDir string) (string, error) {
	return officeconv.ToPDF(ctx, srcPath, workDir, filterFor(srcPath))
}

// filterFor 按扩展名挑 LibreOffice 的导出过滤器。
// 用错过滤器的表现不是报错，而是产出排版错乱的 PDF，因此不能统一用
// writer：xlsx / pptx 必须走各自的 calc / impress 导出器。
func filterFor(srcPath string) officeconv.SOfficeFilter {
	switch strings.ToLower(filepath.Ext(srcPath)) {
	case ".xlsx", ".xls", ".ods", ".csv":
		return officeconv.FilterCalc
	case ".pptx", ".ppt", ".odp":
		return officeconv.FilterImpress
	default:
		return officeconv.FilterWriter
	}
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

