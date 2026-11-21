// Package curation 实现精选策展：以「集合」为单位组织内容。
//
// 产品背景见 PRD §1.2 与 §5.1：平台不做个性化推荐，精选流由运营与大V
// 用人工判断策展，是冷启动期内容供给的主要手段。
package curation

import "errors"

// 领域错误由 HTTP 层映射为稳定的 API 错误码。
var (
	// ErrValidation 表示请求参数不合法（标题、简介、slug、条目引用等）。
	ErrValidation = errors.New("输入校验失败")
	// ErrCollectionNotFound 表示策展集不存在、已删除或未上架。
	ErrCollectionNotFound = errors.New("策展集不存在")
	// ErrForbidden 表示当前用户没有执行该操作的权限。
	ErrForbidden = errors.New("没有权限执行该操作")
	// ErrNotCurator 表示当前账号不具备策展资格（非运营、非大V）。
	ErrNotCurator = errors.New("只有运营或认证大V 可以创建策展集")
	// ErrItemNotFound 表示策展项不存在或不属于该集合。
	ErrItemNotFound = errors.New("策展项不存在")
	// ErrDuplicateItem 表示同一内容已在集合中。
	ErrDuplicateItem = errors.New("该内容已在策展集中")
	// ErrTargetNotFound 表示引用的帖子或论文不存在。
	ErrTargetNotFound = errors.New("引用的内容不存在")
	// ErrInvalidCursor 表示分页游标损坏或不属于当前列表。
	ErrInvalidCursor = errors.New("无效的分页游标")
)
