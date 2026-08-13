// Хранение managed-стеков (FR-14): исходный compose-файл + зашифрованный env.
package store

import "time"

// Stack — сохранённый стек: файл, env (шифртекст) и метаданные.
type Stack struct {
	Name        string
	ComposeYAML string
	EnvEnc      []byte // AES-GCM; nil, если env не задавался
	CreatedAt   int64
	UpdatedAt   int64
	UserID      int64
}

// SaveStack сохраняет или обновляет стек по имени (upsert). created_at
// проставляется один раз, updated_at — на каждое сохранение.
func (s *Store) SaveStack(name, composeYAML string, envEnc []byte, userID int64) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO stacks (name, compose_yaml, env_enc, created_at, updated_at, user_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		     compose_yaml = excluded.compose_yaml,
		     env_enc      = excluded.env_enc,
		     updated_at   = excluded.updated_at,
		     user_id      = excluded.user_id`,
		name, composeYAML, envEnc, now, now, userID,
	)
	return err
}

// StackByName возвращает сохранённый стек; sql.ErrNoRows, если его нет.
func (s *Store) StackByName(name string) (*Stack, error) {
	st := &Stack{}
	var userID *int64
	err := s.db.QueryRow(
		`SELECT name, compose_yaml, env_enc, created_at, updated_at, user_id
		 FROM stacks WHERE name = ?`, name,
	).Scan(&st.Name, &st.ComposeYAML, &st.EnvEnc, &st.CreatedAt, &st.UpdatedAt, &userID)
	if err != nil {
		return nil, err
	}
	if userID != nil {
		st.UserID = *userID
	}
	return st, nil
}

// ManagedStacks возвращает список managed-стеков без тела файла и env —
// только метаданные для списка/пометки «управляется из файла».
func (s *Store) ManagedStacks() ([]Stack, error) {
	rows, err := s.db.Query(
		`SELECT name, created_at, updated_at FROM stacks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stack
	for rows.Next() {
		var st Stack
		if err := rows.Scan(&st.Name, &st.CreatedAt, &st.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// DeleteStack удаляет запись стека (сервисы удаляются отдельно, в Docker).
func (s *Store) DeleteStack(name string) error {
	_, err := s.db.Exec(`DELETE FROM stacks WHERE name = ?`, name)
	return err
}
