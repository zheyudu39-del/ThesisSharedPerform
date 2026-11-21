// Package doc 实现协同文档领域逻辑：文档 CRUD、文档级权限、成员覆盖、
// 快照元数据与批注。
//
// 设计边界：
//   - REST 侧只负责元数据与权限；正文由 Node 协同服务（Hocuspocus + Yjs）
//     实时同步，并把快照写入对象存储。
//   - wordCount / latestUpdateSeq 由协同服务维护，本包只读不写。
//   - 权限判定集中在 permissions.go 的纯函数里，service.go 只负责取事实。
package doc

import "errors"

// 领域错误由 HTTP 层映射为稳定的 API 错误码。
var (
	// ErrValidation 表示请求参数不合法（标题、状态、角色、批注内容等）。
	ErrValidation = errors.New("输入校验失败")
	// ErrGroupNotFound 表示目标群组不存在或已删除。
	ErrGroupNotFound = errors.New("群组不存在")
	// ErrNotFound 覆盖「文档不存在」「文档已删除」「当前用户无权查看」三种情况。
	// 合并为一个错误，避免非成员通过响应差异探测文档是否存在。
	ErrNotFound = errors.New("文档不存在或无权查看")
	// ErrForbidden 表示用户能看到文档，但没有执行该写操作的权限（例如 viewer 编辑）。
	ErrForbidden = errors.New("没有权限执行该操作")
	// ErrMemberNotFound 表示目标用户不是该文档所属群组的成员，或没有文档级角色。
	ErrMemberNotFound = errors.New("该用户不是文档成员")
	// ErrOwnerImmutable 表示试图修改创建者/群主在文档中的角色——他们始终是 owner。
	ErrOwnerImmutable = errors.New("文档创建者与群主的角色不可修改")
	// ErrAnnotationNotFound 表示批注不存在、已删除或不属于该文档。
	ErrAnnotationNotFound = errors.New("批注不存在")
	// ErrInvalidCursor 表示分页游标损坏或不属于当前列表。
	ErrInvalidCursor = errors.New("无效的分页游标")
)
