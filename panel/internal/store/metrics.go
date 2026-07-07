// Методы metrics_1m (минутные агрегаты, разд. 5 ТЗ) и настроек.
package store

// InsertMetrics1m пишет минутную точку ноды (idempotent по PK node_id+ts).
func (s *Store) InsertMetrics1m(nodeID string, ts int64, cpuPct float64,
	memUsed, memTotal uint64, diskJSON, netJSON string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO metrics_1m
		 (node_id, ts, cpu_pct, mem_used, mem_total, disk_json, net_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nodeID, ts, cpuPct, memUsed, memTotal, diskJSON, netJSON,
	)
	return err
}

// MetricPoint — точка истории для графиков (FR-02 3.2.1).
type MetricPoint struct {
	TS       int64   `json:"ts"`
	CPUPct   float64 `json:"cpu_pct"`
	MemUsed  uint64  `json:"mem_used"`
	MemTotal uint64  `json:"mem_total"`
	DiskJSON string  `json:"disk_json"`
	NetJSON  string  `json:"net_json"`
}

// MetricsRange возвращает точки ноды в интервале [from, to], старые первыми.
func (s *Store) MetricsRange(nodeID string, from, to int64) ([]MetricPoint, error) {
	rows, err := s.db.Query(
		`SELECT ts, cpu_pct, mem_used, mem_total,
		        COALESCE(disk_json,''), COALESCE(net_json,'')
		 FROM metrics_1m WHERE node_id = ? AND ts BETWEEN ? AND ? ORDER BY ts`,
		nodeID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricPoint{}
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.TS, &p.CPUPct, &p.MemUsed, &p.MemTotal, &p.DiskJSON, &p.NetJSON); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CleanupMetrics удаляет точки старше cutoff (ретенция 7 дней).
func (s *Store) CleanupMetrics(cutoff int64) error {
	_, err := s.db.Exec(`DELETE FROM metrics_1m WHERE ts < ?`, cutoff)
	return err
}

// LiveRow — сырая строка metrics_live (JSON снапшота) для прогрева буфера.
// store не импортирует пакет metrics — десериализация на стороне вызывающего.
type LiveRow struct {
	NodeID   string
	TS       int64
	Snapshot []byte
}

// InsertMetricsLive пишет живую точку ноды (шаг 3с, idempotent по PK node_id+ts).
// snapshot — JSON снапшота метрик.
func (s *Store) InsertMetricsLive(nodeID string, ts int64, snapshot []byte) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO metrics_live (node_id, ts, snapshot)
		 VALUES (?, ?, ?)`, nodeID, ts, snapshot)
	return err
}

// RecentMetricsLive возвращает все живые точки с ts >= cutoff, старые первыми —
// для прогрева кольцевого буфера при старте панели.
func (s *Store) RecentMetricsLive(cutoff int64) ([]LiveRow, error) {
	rows, err := s.db.Query(
		`SELECT node_id, ts, snapshot FROM metrics_live
		 WHERE ts >= ? ORDER BY ts`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveRow{}
	for rows.Next() {
		var r LiveRow
		if err := rows.Scan(&r.NodeID, &r.TS, &r.Snapshot); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CleanupMetricsLive удаляет живые точки старше cutoff (короткая ретенция ~20 мин).
func (s *Store) CleanupMetricsLive(cutoff int64) error {
	_, err := s.db.Exec(`DELETE FROM metrics_live WHERE ts < ?`, cutoff)
	return err
}

// Vacuum сжимает файл БД (разд. 10 ТЗ: рост SQLite).
func (s *Store) Vacuum() error {
	_, err := s.db.Exec(`VACUUM`)
	return err
}

// SetSetting сохраняет настройку (пороги алертов, ретенция и пр.).
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetSetting возвращает значение настройки ("" если нет).
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return "", nil // отсутствие ключа — не ошибка
	}
	return v, nil
}

// UpdatePassword меняет хэш пароля пользователя (Settings, 6.2 экран 12).
func (s *Store) UpdatePassword(userID int64, passwordHash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	return err
}

// UserByID возвращает пользователя по id.
func (s *Store) UserByID(id int64) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, role FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		return nil, err
	}
	return u, nil
}
