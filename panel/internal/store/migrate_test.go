// Тесты версионирования схемы (NFR-9) и резервной копии (NFR-10).
//
// Что здесь проверяется по существу: апгрейд сборки не требует ручных
// действий, откат на старую сборку не портит данные молча, а бэкап —
// это открываемая база с реальным содержимым, а не пустой файл.
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openRaw открывает БД напрямую, без миграций — для подготовки состояний.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestMigrateFreshDB: чистая БД доводится до текущей версии схемы.
func TestMigrateFreshDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	v, err := st.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != schemaVersion() {
		t.Fatalf("версия схемы: want %d, got %d", schemaVersion(), v)
	}
	// Схема реально создана.
	if _, err := st.CountUsers(); err != nil {
		t.Fatalf("таблица users не создана: %v", err)
	}
}

// TestMigrateLegacyDB: база от сборки без миграций (user_version = 0, схема
// уже есть) поднимается до текущей версии и НЕ теряет данные.
func TestMigrateLegacyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Готовим «старую» БД: схема применена напрямую, версия не проставлена.
	db := openRaw(t, path)
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("подготовка схемы: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (username, password_hash, role, created_at) VALUES ('murad','hash','admin',1)`,
	); err != nil {
		t.Fatalf("подготовка данных: %v", err)
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 0 {
		t.Fatalf("подготовка: ожидалась версия 0, got %d (%v)", v, err)
	}
	_ = db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open старой БД: %v", err)
	}
	defer st.Close()

	got, err := st.SchemaVersion()
	if err != nil || got != schemaVersion() {
		t.Fatalf("версия после апгрейда: want %d, got %d (%v)", schemaVersion(), got, err)
	}
	// Данные на месте — это главное требование апгрейда.
	u, err := st.UserByUsername("murad")
	if err != nil || u.Username != "murad" {
		t.Fatalf("данные потеряны при миграции: %+v (%v)", u, err)
	}
}

// TestMigrateRejectsNewerDB: БД от более новой сборки — отказ со внятным
// текстом, а не тихая работа с неизвестной схемой.
func TestMigrateRejectsNewerDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")

	db := openRaw(t, path)
	if _, err := db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatalf("подготовка версии: %v", err)
	}
	_ = db.Close()

	_, err := Open(path)
	if err == nil {
		t.Fatal("панель не должна открывать БД более новой версии")
	}
	if !strings.Contains(err.Error(), "999") {
		t.Fatalf("ошибка должна называть версию БД: %v", err)
	}
}

// TestMigrateIsIdempotent: повторный старт на той же БД ничего не ломает.
func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	for i := 0; i < 3; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("открытие #%d: %v", i+1, err)
		}
		if _, err := st.SchemaVersion(); err != nil {
			t.Fatalf("версия #%d: %v", i+1, err)
		}
		st.Close()
	}
}

// TestBackupTo: снимок открывается как обычная БД и содержит данные.
func TestBackupTo(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if _, err := st.CreateUser("murad", "hash", "admin"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.SaveStack("app", "services:\n  x:\n    image: alpine\n", []byte("enc"), nil, 1); err != nil {
		t.Fatalf("SaveStack: %v", err)
	}

	backup := filepath.Join(dir, "backup.db")
	if err := st.BackupTo(backup); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}
	info, err := os.Stat(backup)
	if err != nil || info.Size() == 0 {
		t.Fatalf("файл резервной копии пуст: %+v (%v)", info, err)
	}

	// Копия — рабочая БД с теми же данными (главная ценность: исходники стеков).
	restored, err := Open(backup)
	if err != nil {
		t.Fatalf("копия не открывается: %v", err)
	}
	defer restored.Close()

	if u, err := restored.UserByUsername("murad"); err != nil || u.Username != "murad" {
		t.Fatalf("пользователь не попал в копию: %+v (%v)", u, err)
	}
	stack, err := restored.StackByName("app")
	if err != nil || !strings.Contains(stack.ComposeYAML, "alpine") {
		t.Fatalf("исходник стека не попал в копию: %+v (%v)", stack, err)
	}
	// Работа с живой БД после снимка не нарушена.
	if _, err := st.CountUsers(); err != nil {
		t.Fatalf("живая БД сломалась после бэкапа: %v", err)
	}
}
