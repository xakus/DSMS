// Package store — SQLite-хранилище PANEL (раздел 5 ТЗ).
//
// Драйвер modernc.org/sqlite — чистый Go без cgo, чтобы финальный образ
// собирался в scratch. Схема применяется идемпотентно при старте.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // регистрация драйвера "sqlite"
)

// schema — полная схема БД из раздела 5 ТЗ.
// users.role — задел под RBAC (FR-07 3.7.8): в v1 всегда 'admin'.
// audit ссылается на user_id, а не на строку-имя — тоже задел под RBAC.
const schema = `
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'admin',
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS metrics_1m (
    node_id   TEXT    NOT NULL,
    ts        INTEGER NOT NULL,
    cpu_pct   REAL,
    mem_used  INTEGER,
    mem_total INTEGER,
    disk_json TEXT,
    net_json  TEXT,
    PRIMARY KEY (node_id, ts)
);

CREATE TABLE IF NOT EXISTS audit (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    ts           INTEGER NOT NULL,
    user_id      INTEGER NOT NULL,
    action       TEXT NOT NULL,
    object_type  TEXT NOT NULL,
    object_id    TEXT,
    details_json TEXT
);

CREATE TABLE IF NOT EXISTS service_state (
    service_id     TEXT PRIMARY KEY,
    saved_replicas INTEGER NOT NULL,
    stopped_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS registries (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    address      TEXT NOT NULL,
    username     TEXT NOT NULL,
    password_enc BLOB NOT NULL,
    label        TEXT,
    created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS alerts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_opened   INTEGER NOT NULL,
    ts_resolved INTEGER,
    rule        TEXT NOT NULL,
    severity    TEXT NOT NULL,
    object_type TEXT,
    object_id   TEXT,
    message     TEXT
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT
);
`

// Store — обёртка над *sql.DB с методами доменного уровня.
type Store struct {
	db *sql.DB
}

// User — строка таблицы users.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
}

// Open открывает (создаёт) файл БД и применяет схему.
func Open(path string) (*Store, error) {
	// _pragma: WAL для конкурентного чтения, busy_timeout от блокировок.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite: одно соединение на запись, иначе SQLITE_BUSY под нагрузкой.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close закрывает соединение с БД.
func (s *Store) Close() error { return s.db.Close() }

// CountUsers возвращает число пользователей — 0 означает «нужен setup-экран» (3.7.1).
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser создаёт пользователя; в v1 роль всегда 'admin'.
func (s *Store) CreateUser(username, passwordHash, role string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, role, time.Now().Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UserByUsername ищет пользователя по имени; sql.ErrNoRows если нет.
func (s *Store) UserByUsername(username string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, role FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// AppendAudit пишет запись в журнал действий (FR-06 3.6.2).
// В details — только безопасные данные: никаких паролей/токенов (3.7.9, 3.9.4).
func (s *Store) AppendAudit(userID int64, action, objectType, objectID, detailsJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO audit (ts, user_id, action, object_type, object_id, details_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().Unix(), userID, action, objectType, objectID, detailsJSON,
	)
	return err
}
