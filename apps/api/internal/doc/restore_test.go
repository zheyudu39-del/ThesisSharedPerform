package doc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// restoreFake 实现 Querier 中回滚链路用到的查询，其余方法用不到。
// 只嵌入接口而不是逐方法实现，是为了让测试在 Querier 扩展时仍能编译。
type restoreFake struct {
	Querier
	facts       db.GetDocumentAccessRow
	snapshots   map[int32]db.DocSnapshot
	inserted    []db.InsertSnapshotParams
	bumpTo      int32
	insertErr   error
	snapshotErr error
}

func (f *restoreFake) GetDocumentAccess(context.Context, db.GetDocumentAccessParams) (db.GetDocumentAccessRow, error) {
	return f.facts, nil
}

func (f *restoreFake) BumpDocumentSnapshot(context.Context, pgtype.UUID) (int32, error) {
	f.bumpTo++
	return f.bumpTo, nil
}

func (f *restoreFake) InsertSnapshot(_ context.Context, arg db.InsertSnapshotParams) (db.DocSnapshot, error) {
	if f.insertErr != nil {
		return db.DocSnapshot{}, f.insertErr
	}
	f.inserted = append(f.inserted, arg)
	return db.DocSnapshot{
		DocID: arg.DocID, Version: arg.Version, StorageKey: arg.StorageKey,
		SizeBytes: arg.SizeBytes, Label: arg.Label, CreatedBy: arg.CreatedBy,
	}, nil
}

// snapshotFake 是 snapshotQuerier 的替身。
type snapshotFake struct {
	rows map[int32]db.DocSnapshot
	err  error
}

func (f *snapshotFake) GetSnapshotByVersion(_ context.Context, _ pgtype.UUID, version int32) (db.DocSnapshot, error) {
	if f.err != nil {
		return db.DocSnapshot{}, f.err
	}
	row, ok := f.rows[version]
	if !ok {
		return db.DocSnapshot{}, pgx.ErrNoRows
	}
	return row, nil
}

func newRestoreFixture(t *testing.T) (*Service, *restoreFake, *snapshotFake) {
	t.Helper()
	actor := uuid.New()
	docID := uuid.New()
	groupID := uuid.New()

	fake := &restoreFake{
		facts: db.GetDocumentAccessRow{
			DocID:        uuidParam(docID),
			GroupID:      uuidParam(groupID),
			DefaultRole:  db.DocRoleEditor,
			CreatedBy:    uuidParam(actor),
			GroupOwnerID: uuidParam(uuid.New()),
			GroupRole:    db.NullGroupRole{GroupRole: db.GroupRoleMember, Valid: true},
		},
		bumpTo: 6, // 已有 6 个版本，回滚产生 v7
	}
	snaps := &snapshotFake{rows: map[int32]db.DocSnapshot{
		3: {DocID: uuidParam(docID), Version: 3, StorageKey: "doc-snapshot/x/3.bin", SizeBytes: 1024},
	}}

	svc := newServiceWithQuerier(fake)
	svc.snapshots = snaps
	return svc, fake, snaps
}

func TestRestoreSnapshot_CreatesNewVersionPointingAtOldObject(t *testing.T) {
	svc, fake, _ := newRestoreFixture(t)
	docID := fake.facts.DocID.Bytes

	result, err := svc.RestoreSnapshot(context.Background(), fake.facts.CreatedBy.Bytes, docID, 3)
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}

	if result.RestoredFrom != 3 || result.NewVersion != 7 {
		t.Fatalf("结果 = %+v, want from=3 to=7", result)
	}
	// 关键语义：必须告诉前端需要重连，不能假装即时生效。
	if !result.RequiresReconnect {
		t.Fatal("回滚必须要求前端重连协同服务")
	}
	if len(fake.inserted) != 1 {
		t.Fatalf("应插入一条快照记录, got %d", len(fake.inserted))
	}
	got := fake.inserted[0]
	// 不复制二进制：新记录直接指向旧快照的对象键。
	if got.StorageKey != "doc-snapshot/x/3.bin" {
		t.Fatalf("storage_key = %q, 应复用旧快照的键", got.StorageKey)
	}
	if got.SizeBytes != 1024 {
		t.Fatalf("size_bytes = %d, 应沿用旧快照大小", got.SizeBytes)
	}
	if got.Label == nil || *got.Label != "回滚到 v3" {
		t.Fatalf("label = %v, want 回滚到 v3", got.Label)
	}
}

func TestRestoreSnapshot_MissingVersionReturnsNotFound(t *testing.T) {
	svc, fake, _ := newRestoreFixture(t)
	_, err := svc.RestoreSnapshot(context.Background(), fake.facts.CreatedBy.Bytes, fake.facts.DocID.Bytes, 99)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的版本应返回 ErrNotFound, got %v", err)
	}
	if len(fake.inserted) != 0 {
		t.Fatal("失败时不应插入快照记录")
	}
}

func TestRestoreSnapshot_RejectsNonPositiveVersion(t *testing.T) {
	svc, fake, _ := newRestoreFixture(t)
	for _, v := range []int32{0, -1} {
		if _, err := svc.RestoreSnapshot(context.Background(), fake.facts.CreatedBy.Bytes, fake.facts.DocID.Bytes, v); !errors.Is(err, ErrValidation) {
			t.Fatalf("版本 %d 应返回 ErrValidation, got %v", v, err)
		}
	}
}

func TestRestoreSnapshot_ViewerForbidden(t *testing.T) {
	svc, fake, _ := newRestoreFixture(t)
	// 群成员 + 文档默认 viewer：能看不能回滚。
	fake.facts.DefaultRole = db.DocRoleViewer

	// 创建者隐含 owner，必须换成「群成员但只有 viewer」的其他用户。
	other := uuid.New()
	_, err := svc.RestoreSnapshot(context.Background(), other, fake.facts.DocID.Bytes, 3)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer 回滚应返回 ErrForbidden, got %v", err)
	}
}

func TestSnapshotForDownload_RequiresEdit(t *testing.T) {
	svc, fake, _ := newRestoreFixture(t)
	fake.facts.DefaultRole = db.DocRoleViewer

	other := uuid.New()
	if _, err := svc.SnapshotForDownload(context.Background(), other, fake.facts.DocID.Bytes, 3); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer 下载快照应返回 ErrForbidden, got %v", err)
	}
}

func TestSnapshotForDownload_ReturnsStorageKey(t *testing.T) {
	svc, fake, _ := newRestoreFixture(t)
	got, err := svc.SnapshotForDownload(context.Background(), fake.facts.CreatedBy.Bytes, fake.facts.DocID.Bytes, 3)
	if err != nil {
		t.Fatalf("下载快照失败: %v", err)
	}
	if got.Version != 3 || got.Key != "doc-snapshot/x/3.bin" {
		t.Fatalf("结果 = %+v", got)
	}
}
