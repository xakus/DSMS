// Методы service_state (3.4.3 Stop/Start) и registries (FR-13).
package store

import (
	"database/sql"
	"time"
)

// SaveServiceReplicas запоминает реплики перед Stop (scale 0),
// чтобы Start вернул прежнее количество.
func (s *Store) SaveServiceReplicas(serviceID string, replicas uint64) error {
	_, err := s.db.Exec(
		`INSERT INTO service_state (service_id, saved_replicas, stopped_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(service_id) DO UPDATE SET saved_replicas=excluded.saved_replicas,
		                                       stopped_at=excluded.stopped_at`,
		serviceID, replicas, time.Now().Unix(),
	)
	return err
}

// SavedServiceReplicas возвращает запомненные реплики; ok=false — записи нет.
func (s *Store) SavedServiceReplicas(serviceID string) (uint64, bool, error) {
	var n uint64
	err := s.db.QueryRow(
		`SELECT saved_replicas FROM service_state WHERE service_id = ?`, serviceID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

// DeleteServiceState чистит запись после Start или удаления сервиса.
func (s *Store) DeleteServiceState(serviceID string) error {
	_, err := s.db.Exec(`DELETE FROM service_state WHERE service_id = ?`, serviceID)
	return err
}

// Registry — учётные данные приватного реестра (FR-13).
// PasswordEnc — AES-GCM шифртекст; наружу пароль не отдаётся никогда.
type Registry struct {
	ID          int64
	Address     string
	Username    string
	PasswordEnc []byte
	Label       string
}

// CreateRegistry сохраняет реестр с уже зашифрованным паролем.
func (s *Store) CreateRegistry(address, username string, passwordEnc []byte, label string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO registries (address, username, password_enc, label, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		address, username, passwordEnc, label, time.Now().Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Registries возвращает все реестры (с шифртекстами — для внутреннего использования).
func (s *Store) Registries() ([]Registry, error) {
	rows, err := s.db.Query(`SELECT id, address, username, password_enc, label FROM registries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Registry
	for rows.Next() {
		var r Registry
		if err := rows.Scan(&r.ID, &r.Address, &r.Username, &r.PasswordEnc, &r.Label); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RegistryByID возвращает один реестр; sql.ErrNoRows если нет.
func (s *Store) RegistryByID(id int64) (*Registry, error) {
	r := &Registry{}
	err := s.db.QueryRow(
		`SELECT id, address, username, password_enc, label FROM registries WHERE id = ?`, id,
	).Scan(&r.ID, &r.Address, &r.Username, &r.PasswordEnc, &r.Label)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// UpdateRegistry обновляет реестр; passwordEnc==nil — пароль не меняется.
func (s *Store) UpdateRegistry(id int64, address, username string, passwordEnc []byte, label string) error {
	if passwordEnc == nil {
		_, err := s.db.Exec(
			`UPDATE registries SET address=?, username=?, label=? WHERE id=?`,
			address, username, label, id)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE registries SET address=?, username=?, password_enc=?, label=? WHERE id=?`,
		address, username, passwordEnc, label, id)
	return err
}

// DeleteRegistry удаляет реестр.
func (s *Store) DeleteRegistry(id int64) error {
	_, err := s.db.Exec(`DELETE FROM registries WHERE id=?`, id)
	return err
}
