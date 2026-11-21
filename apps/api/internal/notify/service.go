package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

// likeAggWindow 是点赞聚合的时间窗：同一帖子在窗口内的多个点赞合并为一条通知。
// 窗口从「最近一次点赞」起算（合并时会刷新 created_at），因此连续点赞会持续
// 推迟窗口结束，直到静默 10 分钟才产生新的一条。
const likeAggWindow = 10 * time.Minute

// likeAggMaxActors 是 payload 中保留的最近点赞者数量，避免 payload 无限增长。
const likeAggMaxActors = 3

// docEditWindow 是文档编辑通知的聚合窗口。
//
// 比点赞窗口短：协同编辑时连续保存很常见，若用 10 分钟会把一整场协作压成一条，
// 收件人会误以为文档只被改过一次。5 分钟既能压住「保存风暴」，
// 又能在协作告一段落后给出新的提醒。
const docEditWindow = 5 * time.Minute

// emailDigestValues 是偏好里邮件摘要频率的合法取值。
// 邮件通道本身属于 P2，这里只做存取与校验，不发信。
var emailDigestValues = map[string]struct{}{
	"daily":  {},
	"weekly": {},
	"never":  {},
}

// Actor 是通知触发者的摘要，供列表直接渲染头像与昵称。
type Actor struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Handle    string    `json:"handle"`
	AvatarURL *string   `json:"avatarUrl"`
}

// Notification 是通知列表的统一 DTO。
// Payload 原样透传业务附加信息（如点赞聚合的 count 与 actors），
// 前端按 type 决定如何展示。
type Notification struct {
	ID         uuid.UUID      `json:"id"`
	Type       string         `json:"type"`
	Actor      *Actor         `json:"actor"`
	TargetType *string        `json:"targetType"`
	TargetID   *uuid.UUID     `json:"targetId"`
	Payload    map[string]any `json:"payload"`
	Read       bool           `json:"read"`
	ReadAt     *time.Time     `json:"readAt"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// ListResult 是通知列表的一页。
type ListResult struct {
	Notifications []Notification `json:"notifications"`
	NextCursor    *string        `json:"nextCursor"`
}

// Prefs 是通知偏好 DTO。未设置过偏好的用户返回默认值（不静音、每日摘要）。
type Prefs struct {
	MutedTypes  []string `json:"mutedTypes"`
	EmailDigest string   `json:"emailDigest"`
}

// CreateInput 是 Notify 的入参，供评论、点赞、入群申请等模块调用。
// TargetType / TargetID 可为零值（例如系统通知没有具体目标）；
// ActorID 为零值表示系统产生，不关联具体用户。
type CreateInput struct {
	UserID     uuid.UUID
	Type       string
	ActorID    uuid.UUID
	TargetType string
	TargetID   uuid.UUID
	Payload    map[string]any
}

// Querier 限定通知服务使用的生成查询，便于单元测试注入替身。
type Querier interface {
	CreateNotification(context.Context, db.CreateNotificationParams) (db.Notification, error)
	ListNotifications(context.Context, db.ListNotificationsParams) ([]db.ListNotificationsRow, error)
	CountUnread(context.Context, pgtype.UUID) (int64, error)
	MarkRead(context.Context, db.MarkReadParams) (int64, error)
	MarkAllRead(context.Context, pgtype.UUID) (int64, error)
	GetNotificationPrefs(context.Context, pgtype.UUID) (db.NotificationPref, error)
	UpsertNotificationPrefs(context.Context, db.UpsertNotificationPrefsParams) (db.NotificationPref, error)
	GetUnreadLikeAggNotification(context.Context, db.GetUnreadLikeAggNotificationParams) (db.Notification, error)
	UpdateLikeAggNotification(context.Context, db.UpdateLikeAggNotificationParams) (db.Notification, error)
}

// Service 实现通知领域逻辑。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	// docEdits 负责 doc_edit 的窗口聚合。
	//
	// 单独抽成接口而不是塞进 Querier：Querier 被单测替身完整实现，
	// 往里加方法会让所有既有替身编译失败；而聚合查询属于「可选增强」，
	// 未注入时退化为直发通知，不影响其余类型。
	docEdits docEditQuerier
	now      func() time.Time
}

// docEditQuerier 是文档编辑聚合用到的最小查询集合。
//
// 单独抽成接口而不是塞进 Querier：Querier 被单测替身完整实现，
// 往里加方法会让所有既有替身编译失败；而聚合查询属于「可选增强」，
// 未注入时退化为直发通知，不影响其余类型。
type docEditQuerier interface {
	GetUnreadDocEditNotification(context.Context, db.GetUnreadDocEditNotificationParams) (db.Notification, error)
	UpdateDocEditNotification(context.Context, db.UpdateDocEditNotificationParams) (db.Notification, error)
}

// dbDocEditQuerier 把生成查询桥接到 docEditQuerier。
// 事务内用 db.New(tx) 构造，FOR UPDATE 因此能真正加锁。
type dbDocEditQuerier struct{ queries *db.Queries }

func (q dbDocEditQuerier) GetUnreadDocEditNotification(ctx context.Context, arg db.GetUnreadDocEditNotificationParams) (db.Notification, error) {
	return q.queries.GetUnreadDocEditNotification(ctx, arg)
}

func (q dbDocEditQuerier) UpdateDocEditNotification(ctx context.Context, arg db.UpdateDocEditNotificationParams) (db.Notification, error) {
	return q.queries.UpdateDocEditNotification(ctx, arg)
}

// NewService 构造服务。pool 为 nil 时读写都走注入的替身（单元测试场景）。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		pool:     pool,
		queries:  db.New(pool),
		docEdits: dbDocEditQuerier{queries: db.New(pool)},
		now:      time.Now,
	}
}

// newServiceWithQuerier 用于测试注入替身。
func newServiceWithQuerier(queries Querier) *Service {
	return &Service{queries: queries, now: time.Now}
}

// withTx 在事务中执行 fn；没有连接池（单元测试替身）时直接执行。
//
// fn 同时拿到事务内的 Querier 与 docEditQuerier：前者承载点赞聚合，
// 后者承载 doc_edit 聚合，两者共用同一个事务，FOR UPDATE 才真正加锁。
// 单测替身没有事务，两个参数都退回注入的替身（s.queries / s.docEdits）。
func (s *Service) withTx(ctx context.Context, fn func(Querier, docEditQuerier) error) error {
	if s.pool == nil {
		return fn(s.queries, s.docEdits)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if err := fn(q, dbDocEditQuerier{queries: q}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 生成
// ---------------------------------------------------------------------------

// Notify 生成一条通知。
//
// 规则：
//   - 自己给自己的动作不发通知（actorID == userID 直接返回 nil）；
//   - like_agg 在时间窗内按「同用户 + 同目标」聚合为一条，刷新 payload 与 created_at；
//   - 静音只影响读取，不影响落库——用户取消静音后仍能看到历史通知。
func (s *Service) Notify(ctx context.Context, input CreateInput) error {
	if input.UserID == uuid.Nil {
		return ErrValidation
	}
	if !db.NotifyType(input.Type).Valid() {
		return ErrValidation
	}
	// 自己的动作不打扰自己。注意必须在任何写库之前判断。
	if input.ActorID != uuid.Nil && input.ActorID == input.UserID {
		return nil
	}
	payload, err := encodePayload(input.Payload)
	if err != nil {
		return ErrValidation
	}
	if input.Type == string(db.NotifyTypeLikeAgg) {
		return s.notifyLikeAgg(ctx, input, payload)
	}
	if input.Type == string(db.NotifyTypeDocEdit) {
		return s.notifyDocEdit(ctx, input, payload)
	}
	_, err = s.queries.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:     uuidParam(input.UserID),
		Type:       db.NotifyType(input.Type),
		ActorID:    uuidParam(input.ActorID),
		TargetType: optionalString(input.TargetType),
		TargetID:   uuidParam(input.TargetID),
		Payload:    payload,
	})
	if err != nil {
		return fmt.Errorf("创建通知失败: %w", err)
	}
	return nil
}

// notifyLikeAgg 实现点赞聚合：窗口内已有未读的 like_agg 通知时合并，否则新建。
//
// 整个「查—改」在事务内完成，查询带 FOR UPDATE，避免两个并发点赞读到同一份
// 旧 payload 后互相覆盖导致计数丢失。
func (s *Service) notifyLikeAgg(ctx context.Context, input CreateInput, payload []byte) error {
	// 没有目标就无法判断「同一帖子」，退化为普通通知。
	if input.TargetType == "" || input.TargetID == uuid.Nil {
		_, err := s.queries.CreateNotification(ctx, db.CreateNotificationParams{
			UserID:  uuidParam(input.UserID),
			Type:    db.NotifyTypeLikeAgg,
			ActorID: uuidParam(input.ActorID),
			Payload: seedLikeAggPayload(payload, input.ActorID, likeAggMaxActors),
		})
		if err != nil {
			return fmt.Errorf("创建通知失败: %w", err)
		}
		return nil
	}
	return s.withTx(ctx, func(q Querier, _ docEditQuerier) error {
		existing, err := q.GetUnreadLikeAggNotification(ctx, db.GetUnreadLikeAggNotificationParams{
			UserID:     uuidParam(input.UserID),
			TargetType: optionalString(input.TargetType),
			TargetID:   uuidParam(input.TargetID),
			CreatedAt:  pgtype.Timestamptz{Time: s.now().UTC().Add(-likeAggWindow), Valid: true},
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			_, err := q.CreateNotification(ctx, db.CreateNotificationParams{
				UserID:     uuidParam(input.UserID),
				Type:       db.NotifyTypeLikeAgg,
				ActorID:    uuidParam(input.ActorID),
				TargetType: optionalString(input.TargetType),
				TargetID:   uuidParam(input.TargetID),
				Payload:    seedLikeAggPayload(payload, input.ActorID, likeAggMaxActors),
			})
			if err != nil {
				return fmt.Errorf("创建通知失败: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("查询点赞聚合通知失败: %w", err)
		}
		merged := mergeLikeAggPayload(existing.Payload, input.ActorID, likeAggMaxActors)
		_, err = q.UpdateLikeAggNotification(ctx, db.UpdateLikeAggNotificationParams{
			ID:      existing.ID,
			Payload: merged,
			ActorID: uuidParam(input.ActorID),
			UserID:  uuidParam(input.UserID),
		})
		if err != nil {
			return fmt.Errorf("更新点赞聚合通知失败: %w", err)
		}
		return nil
	})
}

// notifyDocEdit 实现文档编辑通知的 5 分钟窗口聚合。
//
// 与点赞聚合同构：窗口内已有未读的 doc_edit 通知就合并（刷新 payload 与
// created_at），否则新建。payload 里的 count 记录窗口内的编辑次数，
// actors 保留最近编辑者，供前端展示「张三等 3 人编辑过」。
// 没有目标（缺文档 ID）时退化为普通直发，避免聚合键不完整。
func (s *Service) notifyDocEdit(ctx context.Context, input CreateInput, payload []byte) error {
	if input.TargetType == "" || input.TargetID == uuid.Nil || s.docEdits == nil {
		_, err := s.queries.CreateNotification(ctx, db.CreateNotificationParams{
			UserID:     uuidParam(input.UserID),
			Type:       db.NotifyTypeDocEdit,
			ActorID:    uuidParam(input.ActorID),
			TargetType: optionalString(input.TargetType),
			TargetID:   uuidParam(input.TargetID),
			Payload:    seedLikeAggPayload(payload, input.ActorID, likeAggMaxActors),
		})
		if err != nil {
			return fmt.Errorf("创建通知失败: %w", err)
		}
		return nil
	}
	return s.withTx(ctx, func(q Querier, docEdits docEditQuerier) error {
		existing, err := docEdits.GetUnreadDocEditNotification(ctx, db.GetUnreadDocEditNotificationParams{
			UserID:     uuidParam(input.UserID),
			TargetType: optionalString(input.TargetType),
			TargetID:   uuidParam(input.TargetID),
			CreatedAt:  pgtype.Timestamptz{Time: s.now().UTC().Add(-docEditWindow), Valid: true},
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// 新建与更新都走同一事务：FOR UPDATE 锁住的是「已有通知」，
			// 没有已有通知时靠 type + 目标的唯一语义由应用层保证（同一次保存只调一次）。
			_, err := q.CreateNotification(ctx, db.CreateNotificationParams{
				UserID:     uuidParam(input.UserID),
				Type:       db.NotifyTypeDocEdit,
				ActorID:    uuidParam(input.ActorID),
				TargetType: optionalString(input.TargetType),
				TargetID:   uuidParam(input.TargetID),
				Payload:    seedLikeAggPayload(payload, input.ActorID, likeAggMaxActors),
			})
			if err != nil {
				return fmt.Errorf("创建通知失败: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("查询文档编辑聚合通知失败: %w", err)
		}
		merged := mergeLikeAggPayload(existing.Payload, input.ActorID, likeAggMaxActors)
		_, err = docEdits.UpdateDocEditNotification(ctx, db.UpdateDocEditNotificationParams{
			ID:      existing.ID,
			Payload: merged,
			ActorID: uuidParam(input.ActorID),
			UserID:  uuidParam(input.UserID),
		})
		if err != nil {
			return fmt.Errorf("更新文档编辑聚合通知失败: %w", err)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// 读取
// ---------------------------------------------------------------------------

// List 返回通知列表，按 (created_at, id) 倒序游标翻页，并排除静音类型。
func (s *Service) List(ctx context.Context, userID uuid.UUID, rawCursor string, limit int32) (ListResult, error) {
	limit = normalizeLimit(limit)
	createdAt, id, err := decodeCursor(rawCursor, "notifications")
	if err != nil {
		return ListResult{}, err
	}
	rows, err := s.queries.ListNotifications(ctx, db.ListNotificationsParams{
		UserID:  uuidParam(userID),
		Column2: timeCursor(createdAt, rawCursor),
		Column3: uuidParam(id),
		Limit:   limit + 1,
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("查询通知列表失败: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]Notification, 0, len(rows))
	for _, row := range rows {
		items = append(items, notificationFromRow(row))
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor("notifications", last.CreatedAt.Time, last.ID.Bytes)
		if err != nil {
			return ListResult{}, err
		}
		next = &encoded
	}
	return ListResult{Notifications: items, NextCursor: next}, nil
}

// UnreadCount 返回未读数，口径与 List 一致（排除静音类型）。
func (s *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	count, err := s.queries.CountUnread(ctx, uuidParam(userID))
	if err != nil {
		return 0, fmt.Errorf("统计未读通知失败: %w", err)
	}
	return int(count), nil
}

// MarkRead 将单条通知标记为已读；不存在或不属于本人时返回 ErrNotificationNotFound。
func (s *Service) MarkRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	if notificationID == uuid.Nil {
		return ErrNotificationNotFound
	}
	rows, err := s.queries.MarkRead(ctx, db.MarkReadParams{
		ID:     uuidParam(notificationID),
		UserID: uuidParam(userID),
	})
	if err != nil {
		return fmt.Errorf("标记通知已读失败: %w", err)
	}
	if rows == 0 {
		return ErrNotificationNotFound
	}
	return nil
}

// MarkAllRead 将当前用户的全部未读通知标记为已读。
// 静音类型也一并标记，否则取消静音后会冒出一批「早已过去」的未读。
func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	if _, err := s.queries.MarkAllRead(ctx, uuidParam(userID)); err != nil {
		return fmt.Errorf("标记全部已读失败: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 偏好
// ---------------------------------------------------------------------------

// GetPrefs 返回通知偏好；从未设置过的用户得到默认值。
func (s *Service) GetPrefs(ctx context.Context, userID uuid.UUID) (Prefs, error) {
	record, err := s.queries.GetNotificationPrefs(ctx, uuidParam(userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultPrefs(), nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("查询通知偏好失败: %w", err)
	}
	return prefsFromDB(record), nil
}

// UpdatePrefs 更新通知偏好。
//
// mutedTypes 为 nil 表示「本次不改动」，空切片表示「清空静音」；
// emailDigest 为空串同理保持原值。这样 PATCH 语义可以只提交想改的字段。
func (s *Service) UpdatePrefs(ctx context.Context, userID uuid.UUID, mutedTypes []string, emailDigest string) error {
	current, err := s.GetPrefs(ctx, userID)
	if err != nil {
		return err
	}
	next := current
	if mutedTypes != nil {
		types, err := validateMutedTypes(mutedTypes)
		if err != nil {
			return err
		}
		next.MutedTypes = types
	}
	if emailDigest != "" {
		if _, ok := emailDigestValues[emailDigest]; !ok {
			return ErrValidation
		}
		next.EmailDigest = emailDigest
	}
	_, err = s.queries.UpsertNotificationPrefs(ctx, db.UpsertNotificationPrefsParams{
		UserID:      uuidParam(userID),
		MutedTypes:  toDBTypes(next.MutedTypes),
		EmailDigest: next.EmailDigest,
	})
	if err != nil {
		return fmt.Errorf("保存通知偏好失败: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 内部辅助
// ---------------------------------------------------------------------------

// validateMutedTypes 校验并去重静音类型；顺序保持调用方给定顺序，便于前端回显。
func validateMutedTypes(types []string) ([]string, error) {
	seen := make(map[string]struct{}, len(types))
	result := make([]string, 0, len(types))
	for _, raw := range types {
		if !db.NotifyType(raw).Valid() {
			return nil, ErrValidation
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		result = append(result, raw)
	}
	return result, nil
}

func defaultPrefs() Prefs {
	return Prefs{MutedTypes: []string{}, EmailDigest: "daily"}
}

func prefsFromDB(record db.NotificationPref) Prefs {
	prefs := Prefs{MutedTypes: make([]string, 0, len(record.MutedTypes)), EmailDigest: record.EmailDigest}
	for _, t := range record.MutedTypes {
		prefs.MutedTypes = append(prefs.MutedTypes, string(t))
	}
	if prefs.EmailDigest == "" {
		prefs.EmailDigest = "daily"
	}
	return prefs
}

func toDBTypes(types []string) []db.NotifyType {
	result := make([]db.NotifyType, 0, len(types))
	for _, t := range types {
		result = append(result, db.NotifyType(t))
	}
	return result
}

// notificationFromRow 把列表行转换为 DTO。payload 解析失败时降级为空对象，
// 不让一条脏数据拖垮整页列表。
func notificationFromRow(row db.ListNotificationsRow) Notification {
	item := Notification{
		ID:         row.ID.Bytes,
		Type:       string(row.Type),
		TargetType: row.TargetType,
		Payload:    decodePayload(row.Payload),
		Read:       row.ReadAt.Valid,
		CreatedAt:  row.CreatedAt.Time.UTC(),
	}
	if row.TargetID.Valid {
		targetID := uuid.UUID(row.TargetID.Bytes)
		item.TargetID = &targetID
	}
	if row.ReadAt.Valid {
		readAt := row.ReadAt.Time.UTC()
		item.ReadAt = &readAt
	}
	if row.ActorID.Valid && row.ActorName != nil {
		item.Actor = &Actor{
			ID:        row.ActorID.Bytes,
			Name:      *row.ActorName,
			Handle:    stringValue(row.ActorHandle),
			AvatarURL: row.ActorAvatarKey,
		}
	}
	return item
}

// encodePayload 序列化业务附加信息；nil 编码为 {}，与表默认值保持一致。
func encodePayload(payload map[string]any) ([]byte, error) {
	if payload == nil {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func decodePayload(raw []byte) map[string]any {
	payload := map[string]any{}
	if len(raw) == 0 {
		return payload
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return map[string]any{}
	}
	return payload
}

// seedLikeAggPayload 为新建的点赞聚合通知补齐 count 与 actors。
// 调用方若已显式给出这两个字段（例如回填历史计数），保留其值；
// 否则以首个点赞者初始化，后续合并才能在此基础上递增。
func seedLikeAggPayload(payload []byte, actorID uuid.UUID, maxActors int) []byte {
	decoded := decodePayload(payload)
	if _, ok := decoded["count"]; !ok {
		decoded["count"] = 1
	}
	if _, ok := decoded["actors"]; !ok {
		actors := make([]string, 0, maxActors)
		if actorID != uuid.Nil {
			actors = append(actors, actorID.String())
		}
		decoded["actors"] = actors
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return payload
	}
	return encoded
}

// mergeLikeAggPayload 把一次新点赞合并进已有 payload：
// count 递增（同一 actor 已在最近列表中则不重复计数），actors 保留最近 N 个。
func mergeLikeAggPayload(raw []byte, actorID uuid.UUID, maxActors int) []byte {
	payload := decodePayload(raw)
	count := payloadInt(payload["count"])
	actors := payloadActors(payload["actors"])

	actor := actorID.String()
	already := false
	for _, a := range actors {
		if a == actor {
			already = true
			break
		}
	}
	if !already {
		count++
	}
	// 最新点赞者置顶，其余去重后按原顺序补齐。
	next := []string{actor}
	for _, a := range actors {
		if a == actor || len(next) >= maxActors {
			continue
		}
		next = append(next, a)
	}
	payload["count"] = count
	payload["actors"] = next
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return encoded
}

// payloadInt 读取 payload 里的计数字段；JSON 数字解码后是 float64。
func payloadInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// payloadActors 读取 payload 里的 actor 列表，忽略格式不符的元素。
func payloadActors(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	actors := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			actors = append(actors, s)
		}
	}
	return actors
}

func normalizeLimit(limit int32) int32 {
	if limit <= 0 {
		return 20
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func timeCursor(value time.Time, raw string) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: raw != ""}
}

func uuidParam(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
