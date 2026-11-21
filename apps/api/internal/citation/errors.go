// Package citation 实现文档引用管理与参考文献渲染（PRD D-06）。
//
// 设计边界：
//   - 引用条目（doc_references）是文档的附属数据，权限复用 doc 包的
//     纯函数判定，保证「谁能看文档、谁能改文档」与引用接口完全一致。
//   - BibTeX 解析与三种参考文献格式的渲染都是纯函数，不依赖数据库，
//     可以直接单元测试；service.go 只负责取事实、做权限校验与持久化。
//   - 引用格式是「渲染时」的决定：同一份条目可以按 GB/T 7714、APA、IEEE
//     分别输出，因此数据库只存书目字段，不存渲染后的字符串。
package citation

import "errors"

// 领域错误由 HTTP 层映射为稳定的 API 错误码。
var (
	// ErrValidation 表示请求参数或 BibTeX 文本不合法。
	ErrValidation = errors.New("输入校验失败")
	// ErrNotFound 覆盖「文档不存在」「文档已删除」「当前用户无权查看」三种情况。
	// 合并为一个错误，避免非成员通过响应差异探测文档是否存在。
	ErrNotFound = errors.New("文档不存在或无权查看")
	// ErrForbidden 表示用户能看到文档，但没有修改引用的权限（viewer）。
	ErrForbidden = errors.New("没有权限执行该操作")
	// ErrReferenceNotFound 表示引用条目不存在或不属于该文档。
	ErrReferenceNotFound = errors.New("引用条目不存在")
	// ErrUnsupportedFormat 表示参考文献格式不在支持列表内。
	ErrUnsupportedFormat = errors.New("不支持的参考文献格式")
)
