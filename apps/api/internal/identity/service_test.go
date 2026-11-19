package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

type fakeQuerier struct {
	byEmail  map[string]db.User
	byHandle map[string]db.User
	byID     map[uuid.UUID]db.User
}

func (f *fakeQuerier) CreateUser(_ context.Context, params db.CreateUserParams) (db.User, error) {
	record := db.User{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Email: params.Email, PasswordHash: params.PasswordHash,
		Name: params.Name, Handle: params.Handle, Status: "active", CreatedAt: pgTime(time.Now())}
	f.byEmail[*params.Email] = record
	f.byHandle[params.Handle] = record
	f.byID[record.ID.Bytes] = record
	return record, nil
}
func (f *fakeQuerier) GetUserByEmail(_ context.Context, email *string) (db.User, error) {
	if email == nil {
		return db.User{}, pgx.ErrNoRows
	}
	record, ok := f.byEmail[*email]
	if !ok {
		return db.User{}, pgx.ErrNoRows
	}
	return record, nil
}
func (f *fakeQuerier) GetUserByHandle(_ context.Context, handle string) (db.User, error) {
	record, ok := f.byHandle[handle]
	if !ok {
		return db.User{}, pgx.ErrNoRows
	}
	return record, nil
}
func (f *fakeQuerier) GetUserByID(_ context.Context, id pgtype.UUID) (db.User, error) {
	record, ok := f.byID[id.Bytes]
	if !ok {
		return db.User{}, pgx.ErrNoRows
	}
	return record, nil
}

func pgTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func TestServiceRegisterAndLogin(t *testing.T) {
	querier := &fakeQuerier{byEmail: map[string]db.User{}, byHandle: map[string]db.User{}, byID: map[uuid.UUID]db.User{}}
	service := newService(querier, NewTokenManager("test-secret", time.Hour))
	user, token, err := service.Register(context.Background(), RegisterInput{
		Email: "A@Example.com", Password: "password123", Name: "测试用户", Handle: "test-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "a@example.com" || token == "" {
		t.Fatalf("注册返回不完整: %+v", user)
	}
	if _, _, err := service.Register(context.Background(), RegisterInput{
		Email: "a@example.com", Password: "password123", Name: "另一用户", Handle: "other-user",
	}); !errors.Is(err, EmailTaken) {
		t.Fatalf("重复邮箱 error = %v", err)
	}
	if _, _, err := service.Register(context.Background(), RegisterInput{
		Email: "b@example.com", Password: "password123", Name: "另一用户", Handle: "test-user",
	}); !errors.Is(err, HandleTaken) {
		t.Fatalf("重复句柄 error = %v", err)
	}
	if _, _, err := service.Login(context.Background(), LoginInput{Email: "a@example.com", Password: "password123"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Login(context.Background(), LoginInput{Email: "a@example.com", Password: "incorrect"}); !errors.Is(err, InvalidCredentials) {
		t.Fatalf("错误密码 error = %v", err)
	}
	if _, _, err := service.Login(context.Background(), LoginInput{Email: "missing@example.com", Password: "incorrect"}); !errors.Is(err, InvalidCredentials) {
		t.Fatalf("不存在账号 error = %v", err)
	}
	got, err := service.GetByID(context.Background(), user.ID)
	if err != nil || got.Handle != user.Handle {
		t.Fatalf("GetByID() = %+v, %v", got, err)
	}
}

func TestRegisterValidation(t *testing.T) {
	service := newService(&fakeQuerier{}, NewTokenManager("test-secret", time.Hour))
	valid := RegisterInput{Email: "a@example.com", Password: "password123", Name: "测试", Handle: "test-user"}
	cases := []RegisterInput{valid, valid, valid, valid}
	cases[0].Email = "invalid"
	cases[1].Password = "short"
	cases[2].Name = ""
	cases[3].Handle = "bad handle"
	for i, input := range cases {
		if _, _, err := service.Register(context.Background(), input); !errors.Is(err, ValidationError) {
			t.Errorf("用例 %d error = %v", i, err)
		}
	}
}
