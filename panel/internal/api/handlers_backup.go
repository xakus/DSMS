// Резервная копия БД (NFR-10).
//
// После FR-14 в базе лежат исходники стеков и зашифрованный env — потеря
// файла невосполнима, а в кластере этих данных нет (панель не держит своего
// состояния в Swarm, NFR-6). Поэтому снимок можно забрать одной кнопкой.
package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// backupCreate — POST /backup: онлайн-снимок БД, отдаётся файлом.
//
// Снимок делается во временный каталог (VACUUM INTO требует путь на диске)
// и удаляется сразу после отдачи: держать копии БД на сервере панели —
// лишний способ утечь данными.
func (h *handlers) backupCreate(w http.ResponseWriter, r *http.Request) {
	tmpDir, err := os.MkdirTemp("", "dsms-backup-")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "backup failed")
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	name := fmt.Sprintf("dsms-backup-%s.db", time.Now().UTC().Format("20060102-150405"))
	path := filepath.Join(tmpDir, name)

	if err := h.Store.BackupTo(path); err != nil {
		writeErr(w, http.StatusInternalServerError, "backup failed: "+err.Error())
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "backup failed")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "backup failed")
		return
	}

	s := sessionFrom(r)
	// В audit — сам факт снятия копии: это выгрузка всех данных панели наружу.
	_ = h.Store.AppendAudit(s.UserID, "backup.create", "database", name, "")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	// Ошибку копирования логировать бессмысленно: заголовки уже ушли клиенту,
	// он увидит оборванную загрузку.
	_, _ = io.Copy(w, f)
}
