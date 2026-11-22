package paper

import "errors"

var (
	// ErrInvalidInput 表示用户输入无法识别为论文标识，或元数据不完整。
	ErrInvalidInput = errors.New("无效的论文输入")
	// ErrNotFound 表示未找到论文（外部源或本地库）。
	ErrNotFound = errors.New("未找到论文")
	// ErrForbidden 表示当前用户无权修改这篇论文的全文。
	ErrForbidden = errors.New("没有权限修改这篇论文")
)
