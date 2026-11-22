package paper

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// fakeQuerier 只实现 SetPDFFile 用到的查询，其余方法 panic，
// 这样测试若意外走到别的路径会立刻暴露。
type fakeQuerier struct {
	Querier

	paper     db.Paper
	paperErr  error
	file      db.File
	fileErr   error
	user      db.User
	userErr   error
	updated   db.Paper
	updateErr error

	setCalls int
	lastSet  db.SetPaperPDFFileParams
}

func (f *fakeQuerier) GetPaperByID(context.Context, pgtype.UUID) (db.Paper, error) {
	return f.paper, f.paperErr
}

func (f *fakeQuerier) GetFileByID(context.Context, pgtype.UUID) (db.File, error) {
	return f.file, f.fileErr
}

func (f *fakeQuerier) GetUserByID(context.Context, pgtype.UUID) (db.User, error) {
	return f.user, f.userErr
}

func (f *fakeQuerier) SetPaperPDFFile(_ context.Context, arg db.SetPaperPDFFileParams) (db.Paper, error) {
	f.setCalls++
	f.lastSet = arg
	return f.updated, f.updateErr
}

func uuidOf(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

func TestSetPDFFileByCreator(t *testing.T) {
	creator := uuid.Must(uuid.NewV7())
	paperID := uuid.Must(uuid.NewV7())
	fileID := uuid.Must(uuid.NewV7())

	q := &fakeQuerier{
		paper: db.Paper{ID: uuidOf(paperID), CreatedBy: uuidOf(creator)},
		file:  db.File{ID: uuidOf(fileID), OwnerID: uuidOf(creator), Status: db.FileStatusReady},
		updated: db.Paper{
			ID: uuidOf(paperID), CreatedBy: uuidOf(creator), PdfFileID: uuidOf(fileID),
		},
	}

	got, err := NewServiceWithQuerier(q, nil).SetPDFFile(context.Background(), creator, paperID, &fileID)
	if err != nil {
		t.Fatalf("SetPDFFile: %v", err)
	}
	if got.PDFFileID == nil || *got.PDFFileID != fileID {
		t.Fatalf("返回的 pdfFileId = %v, want %v", got.PDFFileID, fileID)
	}
	if q.lastSet.PdfFileID.Bytes != fileID || !q.lastSet.PdfFileID.Valid {
		t.Fatalf("写入的文件 ID 不对：%+v", q.lastSet.PdfFileID)
	}
}

// TestSetPDFFileRejectsNonCreator 验证非创建者、非管理员不能挂载。
func TestSetPDFFileRejectsNonCreator(t *testing.T) {
	creator := uuid.Must(uuid.NewV7())
	other := uuid.Must(uuid.NewV7())
	paperID := uuid.Must(uuid.NewV7())
	fileID := uuid.Must(uuid.NewV7())

	q := &fakeQuerier{
		paper: db.Paper{ID: uuidOf(paperID), CreatedBy: uuidOf(creator)},
		user:  db.User{ID: uuidOf(other), Role: db.UserRoleUser},
	}

	if _, err := NewServiceWithQuerier(q, nil).SetPDFFile(context.Background(), other, paperID, &fileID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if q.setCalls != 0 {
		t.Fatal("无权时不应写库")
	}
}

// TestSetPDFFileAdminAllowed 验证管理员可以给别人的论文挂全文。
func TestSetPDFFileAdminAllowed(t *testing.T) {
	creator := uuid.Must(uuid.NewV7())
	admin := uuid.Must(uuid.NewV7())
	paperID := uuid.Must(uuid.NewV7())
	fileID := uuid.Must(uuid.NewV7())

	q := &fakeQuerier{
		paper:   db.Paper{ID: uuidOf(paperID), CreatedBy: uuidOf(creator)},
		user:    db.User{ID: uuidOf(admin), Role: db.UserRoleAdmin},
		file:    db.File{ID: uuidOf(fileID), OwnerID: uuidOf(creator), Status: db.FileStatusReady},
		updated: db.Paper{ID: uuidOf(paperID), PdfFileID: uuidOf(fileID)},
	}

	if _, err := NewServiceWithQuerier(q, nil).SetPDFFile(context.Background(), admin, paperID, &fileID); err != nil {
		t.Fatalf("管理员挂载应当成功，实际：%v", err)
	}
}

// TestSetPDFFileRejectsForeignFile 验证不能把别人的文件挪到自己的论文上。
func TestSetPDFFileRejectsForeignFile(t *testing.T) {
	creator := uuid.Must(uuid.NewV7())
	stranger := uuid.Must(uuid.NewV7())
	paperID := uuid.Must(uuid.NewV7())
	fileID := uuid.Must(uuid.NewV7())

	q := &fakeQuerier{
		paper: db.Paper{ID: uuidOf(paperID), CreatedBy: uuidOf(creator)},
		user:  db.User{ID: uuidOf(creator), Role: db.UserRoleUser},
		file:  db.File{ID: uuidOf(fileID), OwnerID: uuidOf(stranger), Status: db.FileStatusReady},
	}

	if _, err := NewServiceWithQuerier(q, nil).SetPDFFile(context.Background(), creator, paperID, &fileID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

// TestSetPDFFileRejectsFailedFile 验证失败/被拒的文件不会被挂上。
func TestSetPDFFileRejectsFailedFile(t *testing.T) {
	creator := uuid.Must(uuid.NewV7())
	paperID := uuid.Must(uuid.NewV7())
	fileID := uuid.Must(uuid.NewV7())

	for _, status := range []db.FileStatus{db.FileStatusFailed, db.FileStatusRejected} {
		q := &fakeQuerier{
			paper: db.Paper{ID: uuidOf(paperID), CreatedBy: uuidOf(creator)},
			file:  db.File{ID: uuidOf(fileID), OwnerID: uuidOf(creator), Status: status},
		}
		if _, err := NewServiceWithQuerier(q, nil).SetPDFFile(context.Background(), creator, paperID, &fileID); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("status=%s err = %v, want ErrInvalidInput", status, err)
		}
	}
}

// TestSetPDFFileDetach 验证传 nil 可以卸载全文。
func TestSetPDFFileDetach(t *testing.T) {
	creator := uuid.Must(uuid.NewV7())
	paperID := uuid.Must(uuid.NewV7())
	fileID := uuid.Must(uuid.NewV7())

	q := &fakeQuerier{
		paper:   db.Paper{ID: uuidOf(paperID), CreatedBy: uuidOf(creator), PdfFileID: uuidOf(fileID)},
		updated: db.Paper{ID: uuidOf(paperID), CreatedBy: uuidOf(creator)},
	}

	got, err := NewServiceWithQuerier(q, nil).SetPDFFile(context.Background(), creator, paperID, nil)
	if err != nil {
		t.Fatalf("卸载失败：%v", err)
	}
	if got.PDFFileID != nil {
		t.Fatalf("卸载后 pdfFileId 应为 nil，实际 %v", got.PDFFileID)
	}
	if q.lastSet.PdfFileID.Valid {
		t.Fatal("卸载时应写 NULL")
	}
}

// TestSetPDFFilePaperNotFound 验证论文不存在时返回 ErrNotFound。
func TestSetPDFFilePaperNotFound(t *testing.T) {
	q := &fakeQuerier{paperErr: pgx.ErrNoRows}
	userID := uuid.Must(uuid.NewV7())
	paperID := uuid.Must(uuid.NewV7())

	if _, err := NewServiceWithQuerier(q, nil).SetPDFFile(context.Background(), userID, paperID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
