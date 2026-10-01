// Версионирование схемы БД (NFR-9).
//
// Версия хранится в `PRAGMA user_version` — это поле заголовка файла SQLite,
// его не нужно заводить таблицей и оно переживает VACUUM. При старте панель
// применяет все шаги новее текущей версии, каждый в своей транзакции.
//
// Правила добавления миграции:
//  1. Новый шаг дописывается В КОНЕЦ списка с версией +1. Уже выпущенные шаги
//     не редактируются — на проде они давно применены, правка их не догонит.
//  2. Шаг должен быть идемпотентным там, где это дёшево (IF NOT EXISTS):
//     панель могла упасть между Exec и коммитом на старых версиях SQLite.
//  3. Изменение схемы в `schema` без нового шага — ошибка: новые БД получат
//     поле, старые нет.
package store

import (
	"database/sql"
	"fmt"
)

// migration — один шаг эволюции схемы.
type migration struct {
	version int
	name    string
	stmts   []string
}

// migrations — упорядоченный список шагов. Версия 1 — полная схема v1+FR-14:
// она идемпотентна (CREATE TABLE IF NOT EXISTS), поэтому безопасно
// применяется и к чистой БД, и к уже существующей с `user_version = 0`.
var migrations = []migration{
	{version: 1, name: "initial schema", stmts: []string{schema}},
	// FR-13 × FR-14: стек помнит реестр, из которого тянуть приватные образы.
	// Без FOREIGN KEY: SQLite не добавляет его через ALTER; при удалении
	// реестра ссылку обнуляет DeleteRegistry.
	{version: 2, name: "stacks.registry_id", stmts: []string{
		`ALTER TABLE stacks ADD COLUMN registry_id INTEGER`,
	}},
}

// schemaVersion — версия схемы, которую понимает эта сборка панели.
func schemaVersion() int {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].version
}

// migrate доводит схему БД до версии этой сборки.
//
// БД новее сборки — отказ: старая панель не знает о новых полях и может
// затереть данные. Это осознанный запрет на понижение версии (NFR-9).
func migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	latest := schemaVersion()
	if current > latest {
		return fmt.Errorf(
			"база данных имеет версию схемы %d, а эта сборка панели понимает только %d — "+
				"запустите новую версию панели или восстановите БД из резервной копии",
			current, latest)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return fmt.Errorf("миграция %d (%s): %w", m.version, m.name, err)
		}
	}
	return nil
}

// applyMigration выполняет шаг и поднимает user_version в одной транзакции:
// либо схема и версия меняются вместе, либо не меняется ничего.
func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op после успешного Commit

	for _, stmt := range m.stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	// PRAGMA не принимает плейсхолдеры — версия здесь только из кода, не из ввода.
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil {
		return err
	}
	return tx.Commit()
}

// SchemaVersion возвращает версию схемы открытой БД — для /healthz и диагностики.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}
