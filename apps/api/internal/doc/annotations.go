package doc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// Annotation 是文档批注 DTO。
//
// Anchor 是 Yjs 相对位置等前端自定义结构的原样回传：REST 不理解其内部语义，
// 只保证「写进去什么，读出来什么」。以 json.RawMessage 保存，避免服务端
// 重新序列化时改变键顺序或数值精度。
type Annotation struct {
	ID         uuid.UUID       `json:"id"`
	Author     UserRef         `json:"author"`
	Anchor     json.RawMessage `json:"anchor"`
	QuotedText string          `json:"quotedText"`
	Content    string          `json:"content"`
	Resolved   bool            `json:"resolved"`
	ResolvedBy *UserRef        `json:"resolvedBy"`
	ResolvedAt *time.Time      `json:"resolvedAt"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// AnnotationsResult 是批注列表。
type AnnotationsResult struct {
	Annotations []Annotation `json:"annotations"`
}

// CreateAnnotationInput 是创建批注入参。
type CreateAnnotationInput struct {
	Anchor     json.RawMessage
	QuotedText string
	Content    string
}

// UpdateAnnotationInput 是局部更新入参：nil 表示未提供。
type UpdateAnnotationInput struct {
	Content  *string
	Resolved *bool
}

// ListAnnotations 返回文档的全部未删除批注，要求至少 viewer。
func (s *Service) ListAnnotations(ctx context.Context, userID, docID uuid.UUID) (AnnotationsResult, error) {
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanView); err != nil {
		return AnnotationsResult{}, err
	}
	rows, err := s.queries.ListAnnotations(ctx, uuidParam(docID))
	if err != nil {
		return AnnotationsResult{}, fmt.Errorf("查询批注失败: %w", err)
	}
	annotations := make([]Annotation, 0, len(rows))
	for _, row := range rows {
		annotation, err := annotationFromListRow(row)
		if err != nil {
			return AnnotationsResult{}, err
		}
		annotations = append(annotations, annotation)
	}
	return AnnotationsResult{Annotations: annotations}, nil
}

// CreateAnnotation 创建批注，要求 editor 及以上。
// viewer 只能阅读，不能写批注。
func (s *Service) CreateAnnotation(ctx context.Context, userID, docID uuid.UUID, input CreateAnnotationInput) (Annotation, error) {
	content := strings.TrimSpace(input.Content)
	if n := utf8.RuneCountInString(content); n < 1 || n > 5000 {
		return Annotation{}, fmt.Errorf("%w: 批注内容长度须为 1 到 5000 个字符", ErrValidation)
	}
	quoted := strings.TrimSpace(input.QuotedText)
	if utf8.RuneCountInString(quoted) > 2000 {
		return Annotation{}, fmt.Errorf("%w: 引用文本不能超过 2000 个字符", ErrValidation)
	}
	anchor, err := normalizeAnchor(input.Anchor)
	if err != nil {
		return Annotation{}, err
	}
	if _, err := s.requireRole(ctx, s.queries, userID, docID, CanEdit); err != nil {
		return Annotation{}, err
	}
	created, err := s.queries.CreateAnnotation(ctx, db.CreateAnnotationParams{
		TargetID:   uuidParam(docID),
		AuthorID:   uuidParam(userID),
		Anchor:     anchor,
		QuotedText: nullableString(quoted),
		Content:    content,
	})
	if err != nil {
		return Annotation{}, fmt.Errorf("创建批注失败: %w", err)
	}
	row, err := s.queries.GetAnnotation(ctx, db.GetAnnotationParams{
		ID: created.ID, TargetID: uuidParam(docID),
	})
	if err != nil {
		return Annotation{}, fmt.Errorf("查询批注失败: %w", err)
	}
	return annotationFromRow(row)
}

// UpdateAnnotation 修改批注内容或标记解决状态。
//
// 权限：owner 可以处理任何批注；editor 只能处理自己写的；viewer 一律拒绝。
// 内容修改与解决状态可以在同一次请求里提交。
func (s *Service) UpdateAnnotation(ctx context.Context, userID, docID, annotationID uuid.UUID, input UpdateAnnotationInput) (Annotation, error) {
	if input.Content == nil && input.Resolved == nil {
		return Annotation{}, fmt.Errorf("%w: 至少需要提供 content 或 resolved", ErrValidation)
	}
	var content string
	if input.Content != nil {
		content = strings.TrimSpace(*input.Content)
		if n := utf8.RuneCountInString(content); n < 1 || n > 5000 {
			return Annotation{}, fmt.Errorf("%w: 批注内容长度须为 1 到 5000 个字符", ErrValidation)
		}
	}
	var updatedID pgtype.UUID
	err := s.withTx(ctx, func(q Querier) error {
		role, err := s.requireRole(ctx, q, userID, docID, CanEdit)
		if err != nil {
			return err
		}
		existing, err := q.GetAnnotation(ctx, db.GetAnnotationParams{
			ID: uuidParam(annotationID), TargetID: uuidParam(docID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAnnotationNotFound
		}
		if err != nil {
			return fmt.Errorf("查询批注失败: %w", err)
		}
		if !CanModifyAnnotation(role, existing.AuthorID.Bytes == userID) {
			return ErrForbidden
		}
		if input.Content != nil {
			if _, err := q.UpdateAnnotationContent(ctx, db.UpdateAnnotationContentParams{
				ID: uuidParam(annotationID), TargetID: uuidParam(docID), Content: content,
			}); errors.Is(err, pgx.ErrNoRows) {
				return ErrAnnotationNotFound
			} else if err != nil {
				return fmt.Errorf("更新批注失败: %w", err)
			}
		}
		if input.Resolved != nil {
			params := db.SetAnnotationResolvedParams{
				ID: uuidParam(annotationID), TargetID: uuidParam(docID),
			}
			if *input.Resolved {
				params.ResolvedAt = pgtype.Timestamptz{Time: s.now().UTC(), Valid: true}
				params.ResolvedBy = uuidParam(userID)
			}
			if _, err := q.SetAnnotationResolved(ctx, params); errors.Is(err, pgx.ErrNoRows) {
				return ErrAnnotationNotFound
			} else if err != nil {
				return fmt.Errorf("更新批注状态失败: %w", err)
			}
		}
		updatedID = uuidParam(annotationID)
		return nil
	})
	if err != nil {
		return Annotation{}, err
	}
	row, err := s.queries.GetAnnotation(ctx, db.GetAnnotationParams{
		ID: updatedID, TargetID: uuidParam(docID),
	})
	if err != nil {
		return Annotation{}, fmt.Errorf("查询批注失败: %w", err)
	}
	return annotationFromRow(row)
}

// normalizeAnchor 校验并规范化批注锚点。
// 必须是 JSON 对象（前端写入 Yjs 相对位置等结构），空值或非对象一律拒绝。
func normalizeAnchor(raw json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, fmt.Errorf("%w: anchor 不能为空", ErrValidation)
	}
	if len(trimmed) > 8192 {
		return nil, fmt.Errorf("%w: anchor 过大", ErrValidation)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil, fmt.Errorf("%w: anchor 必须是 JSON 对象", ErrValidation)
	}
	return []byte(trimmed), nil
}

func annotationFromRow(row db.GetAnnotationRow) (Annotation, error) {
	anchor := json.RawMessage(row.Anchor)
	if len(anchor) == 0 {
		anchor = json.RawMessage("null")
	}
	annotation := Annotation{
		ID:         row.ID.Bytes,
		Author:     UserRef{ID: row.AuthorID.Bytes, Name: row.AuthorName, Handle: row.AuthorHandle},
		Anchor:     anchor,
		QuotedText: stringValue(row.QuotedText),
		Content:    row.Content,
		Resolved:   row.ResolvedAt.Valid,
		CreatedAt:  row.CreatedAt.Time.UTC(),
	}
	if row.ResolvedAt.Valid {
		resolvedAt := row.ResolvedAt.Time.UTC()
		annotation.ResolvedAt = &resolvedAt
	}
	if row.ResolvedBy.Valid {
		annotation.ResolvedBy = &UserRef{ID: row.ResolvedBy.Bytes}
	}
	return annotation, nil
}

// annotationFromListRow 与 annotationFromRow 投影逻辑一致，只是行类型不同。
func annotationFromListRow(row db.ListAnnotationsRow) (Annotation, error) {
	return annotationFromRow(db.GetAnnotationRow(row))
}
