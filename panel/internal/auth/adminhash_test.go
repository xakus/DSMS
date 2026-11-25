// Тесты восстановления доступа через ADMIN_PASSWORD_HASH (ТЗ 3.7.1).
package auth

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/xakus/DSMS/panel/internal/store"
)

// fakeAdminStore — пользователи в памяти.
type fakeAdminStore struct {
	users []store.User
}

func (f *fakeAdminStore) CountUsers() (int, error) { return len(f.users), nil }

func (f *fakeAdminStore) CreateUser(username, hash, role string) (int64, error) {
	id := int64(len(f.users) + 1)
	f.users = append(f.users, store.User{ID: id, Username: username, PasswordHash: hash, Role: role})
	return id, nil
}

func (f *fakeAdminStore) FirstAdmin() (*store.User, error) {
	for i := range f.users {
		if f.users[i].Role == roleAdmin {
			return &f.users[i], nil
		}
	}
	return nil, sql.ErrNoRows
}

func (f *fakeAdminStore) UpdatePassword(id int64, hash string) error {
	for i := range f.users {
		if f.users[i].ID == id {
			f.users[i].PasswordHash = hash
		}
	}
	return nil
}

// mustHash — bcrypt-хэш пароля для теста.
func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Переменная не задана — пользователи не трогаются.
func TestApplyAdminHashSkipped(t *testing.T) {
	st := &fakeAdminStore{}
	action, _, err := ApplyAdminHash(st, "  ")
	if err != nil || action != AdminHashSkipped || len(st.users) != 0 {
		t.Fatalf("action %q err %v users %d, ожидалось skipped без изменений", action, err, len(st.users))
	}
}

// Пустая панель: администратор создаётся сразу, без setup-экрана.
func TestApplyAdminHashCreatesAdmin(t *testing.T) {
	st := &fakeAdminStore{}
	hash := mustHash(t, "first-password")
	action, name, err := ApplyAdminHash(st, hash)
	if err != nil || action != AdminHashCreated || name != DefaultAdminUsername {
		t.Fatalf("action %q name %q err %v", action, name, err)
	}
	if !CheckPassword(st.users[0].PasswordHash, "first-password") {
		t.Fatal("созданный администратор не входит с заданным паролем")
	}
}

// Забытый пароль: хэш из переменной заменяет пароль существующего
// администратора, имя учётной записи остаётся прежним.
func TestApplyAdminHashResetsPassword(t *testing.T) {
	st := &fakeAdminStore{users: []store.User{
		{ID: 1, Username: "murad", PasswordHash: mustHash(t, "forgotten-one"), Role: roleAdmin},
	}}
	action, name, err := ApplyAdminHash(st, mustHash(t, "new-password"))
	if err != nil || action != AdminHashUpdated || name != "murad" {
		t.Fatalf("action %q name %q err %v", action, name, err)
	}
	if !CheckPassword(st.users[0].PasswordHash, "new-password") {
		t.Fatal("новый пароль не подошёл")
	}
	if CheckPassword(st.users[0].PasswordHash, "forgotten-one") {
		t.Fatal("старый пароль всё ещё подходит")
	}

	// Повторный старт с той же переменной ничего не меняет.
	again, _, err := ApplyAdminHash(st, st.users[0].PasswordHash)
	if err != nil || again != AdminHashUnchanged {
		t.Fatalf("повтор: action %q err %v, ожидалось unchanged", again, err)
	}
}

// В переменной не bcrypt-хэш (например, сам пароль или хэш с обрезанными
// знаками «$» из compose-файла) — отказ с понятной ошибкой, пользователи целы.
func TestApplyAdminHashRejectsGarbage(t *testing.T) {
	st := &fakeAdminStore{users: []store.User{{ID: 1, Username: "admin", PasswordHash: "old", Role: roleAdmin}}}
	for _, bad := range []string{"plain-password", "2a12abcdef", "$2a$04$abcdefghijklmnopqrstuuJ7Q3YpN0m9bq0y8w1Zl2E3r4T5y6U7i"} {
		_, _, err := ApplyAdminHash(st, bad)
		if !errors.Is(err, ErrBadAdminHash) {
			t.Fatalf("%q: err %v, ожидалась ErrBadAdminHash", bad, err)
		}
		if strings.Contains(err.Error(), bad) {
			t.Fatal("значение переменной попало в текст ошибки")
		}
	}
	if st.users[0].PasswordHash != "old" {
		t.Fatal("пароль изменён, хотя хэш отклонён")
	}
}

// Команда выдачи хэша: пароль читается из потока, хэш подходит к паролю.
func TestHashFromReader(t *testing.T) {
	hash, err := HashFromReader(strings.NewReader("correct-horse\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct-horse") {
		t.Fatal("хэш не подходит к паролю")
	}
	if _, _, err := ApplyAdminHash(&fakeAdminStore{}, hash); err != nil {
		t.Fatalf("выданный хэш не принят ApplyAdminHash: %v", err)
	}

	// Короткий пароль отклоняется (как на экране первой настройки).
	if _, err := HashFromReader(strings.NewReader("short\n")); err == nil {
		t.Fatal("пароль короче 8 символов должен быть отклонён")
	}
}
