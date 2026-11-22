package paper

import (
	"context"
	"encoding/json"
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

	// 手动补录（P-04）
	createCalls int
	lastCreate  db.CreatePaperParams
	createErr   error
	doiErr      error
	arxivErr    error
	dedupeErr   error
	dedupePaper db.Paper
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

/* -------------------------------------------------------------------------- */
/* 手动补录（P-04）                                                            */
/* -------------------------------------------------------------------------- */

func (f *fakeQuerier) CreatePaper(_ context.Context, arg db.CreatePaperParams) (db.Paper, error) {
	f.createCalls++
	f.lastCreate = arg
	if f.createErr != nil {
		return db.Paper{}, f.createErr
	}
	return db.Paper{
		ID: uuidOf(uuid.Must(uuid.NewV7())), Title: arg.Title, Authors: arg.Authors,
		Doi: arg.Doi, ArxivID: arg.ArxivID, MetaSource: arg.MetaSource,
	}, nil
}

func (f *fakeQuerier) GetPaperByDOI(context.Context, *string) (db.Paper, error) {
	if f.doiErr != nil {
		return db.Paper{}, f.doiErr
	}
	return f.paper, nil
}

func (f *fakeQuerier) GetPaperByArxivID(context.Context, *string) (db.Paper, error) {
	if f.arxivErr != nil {
		return db.Paper{}, f.arxivErr
	}
	return f.paper, nil
}

func (f *fakeQuerier) GetPaperByDedupeHash(context.Context, *string) (db.Paper, error) {
	if f.dedupeErr != nil {
		return db.Paper{}, f.dedupeErr
	}
	return f.dedupePaper, nil
}

func TestCreateManualRequiresTitle(t *testing.T) {
	q := &fakeQuerier{}
	_, err := NewServiceWithQuerier(q, nil).CreateManual(context.Background(), uuid.Must(uuid.NewV7()), CreateManualInput{
		Title:   "   ",
		Authors: []Author{{Name: "张三"}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if q.createCalls != 0 {
		t.Fatal("校验失败时不应写库")
	}
}

func TestCreateManualRequiresAuthor(t *testing.T) {
	q := &fakeQuerier{}
	for _, authors := range [][]Author{
		nil,
		{{Name: ""}},
		{{Name: "   "}},
	} {
		_, err := NewServiceWithQuerier(q, nil).CreateManual(context.Background(), uuid.Must(uuid.NewV7()), CreateManualInput{
			Title: "标题", Authors: authors,
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("authors=%v err = %v, want ErrInvalidInput", authors, err)
		}
	}
}

// TestCreateManualWritesManualSource 验证来源被正确标记，
// 运营才能区分「抓来的」与「人填的」。
func TestCreateManualWritesManualSource(t *testing.T) {
	q := &fakeQuerier{doiErr: pgx.ErrNoRows, arxivErr: pgx.ErrNoRows, dedupeErr: pgx.ErrNoRows}
	got, err := NewServiceWithQuerier(q, nil).CreateManual(context.Background(), uuid.Must(uuid.NewV7()), CreateManualInput{
		Title:   "无 DOI 的专著",
		Authors: []Author{{Name: "张三"}, {Name: "  "}}, // 空白作者被丢弃
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if got.Existing {
		t.Fatal("应当是新建")
	}
	if q.lastCreate.MetaSource == nil || *q.lastCreate.MetaSource != string(SourceManual) {
		t.Fatalf("metaSource = %v, want manual", q.lastCreate.MetaSource)
	}
	var authors []Author
	if err := json.Unmarshal(q.lastCreate.Authors, &authors); err != nil {
		t.Fatalf("解析作者: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "张三" {
		t.Fatalf("空白作者应被丢弃，实际 %+v", authors)
	}
}

// TestCreateManualNormalizesIdentifiers 验证 DOI / arXiv 会被规范化，
// 否则「粘贴整条链接」与「裸编号」会被当成两篇不同论文。
func TestCreateManualNormalizesIdentifiers(t *testing.T) {
	q := &fakeQuerier{doiErr: pgx.ErrNoRows, arxivErr: pgx.ErrNoRows}
	_, err := NewServiceWithQuerier(q, nil).CreateManual(context.Background(), uuid.Must(uuid.NewV7()), CreateManualInput{
		Title:   "带标识的论文",
		Authors: []Author{{Name: "张三"}},
		DOI:     "https://doi.org/10.1145/ABC123",
		ArxivID: "https://arxiv.org/abs/2405.13412v3",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if q.lastCreate.Doi == nil || *q.lastCreate.Doi != "10.1145/abc123" {
		t.Fatalf("DOI 未规范化：%v", q.lastCreate.Doi)
	}
	if q.lastCreate.ArxivID == nil || *q.lastCreate.ArxivID != "2405.13412" {
		t.Fatalf("arXiv 未规范化：%v", q.lastCreate.ArxivID)
	}
}

// TestCreateManualDedupesWithoutIdentifiers 验证无标识的论文按软哈希去重。
func TestCreateManualDedupesWithoutIdentifiers(t *testing.T) {
	existingID := uuid.Must(uuid.NewV7())
	q := &fakeQuerier{
		dedupePaper: db.Paper{ID: uuidOf(existingID), Title: "已有论文"},
	}
	got, err := NewServiceWithQuerier(q, nil).CreateManual(context.Background(), uuid.Must(uuid.NewV7()), CreateManualInput{
		Title:         "已有论文",
		Authors:       []Author{{Name: "张三"}},
		PublishedYear: 2020,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if !got.Existing || got.Paper.ID != existingID {
		t.Fatalf("应命中已有记录，实际 existing=%v id=%v", got.Existing, got.Paper.ID)
	}
	if q.createCalls != 0 {
		t.Fatal("命中已有记录时不应写库")
	}
}

// TestNormalizeArxiv 验证各种写法的 arXiv 编号归一到裸编号。
func TestNormalizeArxiv(t *testing.T) {
	tests := map[string]string{
		"2405.13412":                             "2405.13412",
		"2405.13412v3":                           "2405.13412",
		"https://arxiv.org/abs/2405.13412":       "2405.13412",
		"https://arxiv.org/pdf/2405.13412v2.pdf": "2405.13412",
		"arxiv:2405.13412":                       "2405.13412",
	}
	for input, want := range tests {
		if got := normalizeArxiv(input); got != want {
			t.Fatalf("normalizeArxiv(%q) = %q, want %q", input, got, want)
		}
	}
}
