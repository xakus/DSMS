// Тесты хранилища: пользователи, audit, запомненные реплики, реестры,
// настройки и managed-стеки.
//
// Что здесь по-настоящему важно: Stop сервиса обязан вернуть ровно то число
// реплик, что было (иначе Start поднимет не тот масштаб), а пароли реестров
// и env стеков не должны утечь в открытом виде.
package store

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestUsers: создание, поиск, уникальность имени, смена пароля.
func TestUsers(t *testing.T) {
	st := open(t)

	if n, err := st.CountUsers(); err != nil || n != 0 {
		t.Fatalf("чистая БД должна быть без пользователей: %d (%v)", n, err)
	}

	id, err := st.CreateUser("murad", "hash-1", "admin")
	if err != nil || id == 0 {
		t.Fatalf("CreateUser: %v", err)
	}
	if n, _ := st.CountUsers(); n != 1 {
		t.Fatalf("после setup должен быть один пользователь, а не %d", n)
	}

	u, err := st.UserByUsername("murad")
	if err != nil || u.ID != id || u.PasswordHash != "hash-1" || u.Role != "admin" {
		t.Fatalf("пользователь прочитан неверно: %+v (%v)", u, err)
	}
	if _, err := st.UserByUsername("нет-такого"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("для отсутствующего пользователя ожидается ErrNoRows: %v", err)
	}

	// Второй пользователь с тем же именем — ошибка (UNIQUE).
	if _, err := st.CreateUser("murad", "hash-2", "admin"); err == nil {
		t.Fatal("дубликат имени пользователя принят")
	}

	if err := st.UpdatePassword(id, "hash-new"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	u, _ = st.UserByID(id)
	if u.PasswordHash != "hash-new" {
		t.Fatalf("пароль не обновлён: %+v", u)
	}
}

// TestAudit: журнал отдаёт свежие записи первыми, с именем пользователя.
func TestAudit(t *testing.T) {
	st := open(t)
	uid, _ := st.CreateUser("murad", "h", "admin")

	for _, a := range []string{"login", "service.scale", "stack.deploy_file"} {
		if err := st.AppendAudit(uid, a, "service", "svc-1", ""); err != nil {
			t.Fatalf("AppendAudit(%s): %v", a, err)
		}
	}

	entries, err := st.AuditEntries(10, 0)
	if err != nil {
		t.Fatalf("AuditEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ожидалось 3 записи, получено %d", len(entries))
	}
	if entries[0].Action != "stack.deploy_file" {
		t.Fatalf("свежие записи должны идти первыми: %+v", entries)
	}
	if entries[0].Username != "murad" {
		t.Fatalf("имя пользователя не подтянулось: %+v", entries[0])
	}

	// Пагинация.
	page, err := st.AuditEntries(1, 1)
	if err != nil || len(page) != 1 || page[0].Action != "service.scale" {
		t.Fatalf("пагинация журнала неверна: %+v (%v)", page, err)
	}
}

// TestServiceState: Stop запоминает реплики, Start их возвращает и запись
// удаляется — иначе следующий Start поднимет устаревшее число.
func TestServiceState(t *testing.T) {
	st := open(t)

	if _, ok, err := st.SavedServiceReplicas("svc-1"); err != nil || ok {
		t.Fatalf("для неостановленного сервиса записи быть не должно: %v (%v)", ok, err)
	}

	if err := st.SaveServiceReplicas("svc-1", 5); err != nil {
		t.Fatalf("SaveServiceReplicas: %v", err)
	}
	n, ok, err := st.SavedServiceReplicas("svc-1")
	if err != nil || !ok || n != 5 {
		t.Fatalf("реплики не запомнились: %d, %v (%v)", n, ok, err)
	}

	// Повторный Stop перезаписывает значение (сервис успели отмасштабировать).
	if err := st.SaveServiceReplicas("svc-1", 2); err != nil {
		t.Fatalf("повторный SaveServiceReplicas: %v", err)
	}
	if n, _, _ := st.SavedServiceReplicas("svc-1"); n != 2 {
		t.Fatalf("значение не перезаписано: %d", n)
	}

	if err := st.DeleteServiceState("svc-1"); err != nil {
		t.Fatalf("DeleteServiceState: %v", err)
	}
	if _, ok, _ := st.SavedServiceReplicas("svc-1"); ok {
		t.Fatal("запись не удалена после Start")
	}
}

// TestRegistries: CRUD реестров; пароль хранится как переданный шифртекст.
func TestRegistries(t *testing.T) {
	st := open(t)
	enc := []byte{0x01, 0x02, 0x03}

	id, err := st.CreateRegistry("ghcr.io", "xakus", enc, "основной")
	if err != nil {
		t.Fatalf("CreateRegistry: %v", err)
	}

	list, err := st.Registries()
	if err != nil || len(list) != 1 {
		t.Fatalf("Registries: %+v (%v)", list, err)
	}
	if list[0].Address != "ghcr.io" || list[0].Username != "xakus" || list[0].Label != "основной" {
		t.Fatalf("реестр сохранён неверно: %+v", list[0])
	}

	r, err := st.RegistryByID(id)
	if err != nil {
		t.Fatalf("RegistryByID: %v", err)
	}
	if !bytes.Equal(r.PasswordEnc, enc) {
		t.Fatalf("шифртекст пароля искажён: %v", r.PasswordEnc)
	}

	newEnc := []byte{0x09}
	if err := st.UpdateRegistry(id, "registry.local", "user2", newEnc, "второй"); err != nil {
		t.Fatalf("UpdateRegistry: %v", err)
	}
	r, _ = st.RegistryByID(id)
	if r.Address != "registry.local" || r.Username != "user2" || !bytes.Equal(r.PasswordEnc, newEnc) {
		t.Fatalf("реестр не обновлён: %+v", r)
	}

	if err := st.DeleteRegistry(id); err != nil {
		t.Fatalf("DeleteRegistry: %v", err)
	}
	if list, _ := st.Registries(); len(list) != 0 {
		t.Fatalf("реестр не удалён: %+v", list)
	}
	if _, err := st.RegistryByID(id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("удалённый реестр всё ещё читается: %v", err)
	}
}

// TestSettings: значения перезаписываются, отсутствующий ключ — пустая строка.
func TestSettings(t *testing.T) {
	st := open(t)

	if v, err := st.GetSetting("нет.такого"); err != nil || v != "" {
		t.Fatalf("отсутствующий ключ должен давать пустую строку: %q (%v)", v, err)
	}
	if err := st.SetSetting("alert.disk_pct", "85"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if v, _ := st.GetSetting("alert.disk_pct"); v != "85" {
		t.Fatalf("значение не сохранилось: %q", v)
	}
	if err := st.SetSetting("alert.disk_pct", "95"); err != nil {
		t.Fatalf("перезапись: %v", err)
	}
	if v, _ := st.GetSetting("alert.disk_pct"); v != "95" {
		t.Fatalf("значение не перезаписано: %q", v)
	}
}

// TestAlertsJournal: алерт открывается, закрывается и виден в истории.
func TestAlertsJournal(t *testing.T) {
	st := open(t)

	id, err := st.InsertAlert("disk_high", "critical", "node", "node-1", "диск заполнен", 1000)
	if err != nil {
		t.Fatalf("InsertAlert: %v", err)
	}
	if err := st.ResolveAlert(id, 2000); err != nil {
		t.Fatalf("ResolveAlert: %v", err)
	}

	hist, err := st.AlertHistory(10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("AlertHistory: %+v (%v)", hist, err)
	}
	a := hist[0]
	if a.Rule != "disk_high" || a.OpenedAt != 1000 || a.ResolvedAt != 2000 {
		t.Fatalf("алерт записан неверно: %+v", a)
	}
}

// TestStacksCRUD: managed-стек сохраняется, обновляется и удаляется;
// created_at при обновлении не меняется.
func TestStacksCRUD(t *testing.T) {
	st := open(t)
	uid, _ := st.CreateUser("murad", "h", "admin")

	if err := st.SaveStack("app", "services: {}", []byte("enc-1"), nil, uid); err != nil {
		t.Fatalf("SaveStack: %v", err)
	}
	first, err := st.StackByName("app")
	if err != nil {
		t.Fatalf("StackByName: %v", err)
	}
	if first.ComposeYAML != "services: {}" || !bytes.Equal(first.EnvEnc, []byte("enc-1")) {
		t.Fatalf("стек сохранён неверно: %+v", first)
	}

	if err := st.SaveStack("app", "services: {web: {}}", []byte("enc-2"), nil, uid); err != nil {
		t.Fatalf("повторный SaveStack: %v", err)
	}
	second, _ := st.StackByName("app")
	if second.ComposeYAML == first.ComposeYAML {
		t.Fatal("исходник стека не обновился")
	}
	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("created_at не должен меняться при обновлении: %d → %d",
			first.CreatedAt, second.CreatedAt)
	}

	managed, err := st.ManagedStacks()
	if err != nil || len(managed) != 1 || managed[0].Name != "app" {
		t.Fatalf("ManagedStacks: %+v (%v)", managed, err)
	}
	// Список метаданных не тащит тело файла и env наружу.
	if managed[0].ComposeYAML != "" || managed[0].EnvEnc != nil {
		t.Fatalf("список стеков не должен содержать yaml/env: %+v", managed[0])
	}

	if err := st.DeleteStack("app"); err != nil {
		t.Fatalf("DeleteStack: %v", err)
	}
	if _, err := st.StackByName("app"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("стек не удалён: %v", err)
	}
}

// TestMetricsRangeFiltersByNodeAndTime: график ноды не должен показывать
// точки соседней ноды или вне запрошенного окна.
func TestMetricsRangeFiltersByNodeAndTime(t *testing.T) {
	st := open(t)

	for _, p := range []struct {
		node string
		ts   int64
	}{
		{"node-1", 100}, {"node-1", 200}, {"node-1", 300}, {"node-2", 200},
	} {
		if err := st.InsertMetrics1m(p.node, p.ts, 10, 1, 2, "{}", "{}"); err != nil {
			t.Fatalf("InsertMetrics1m: %v", err)
		}
	}

	points, err := st.MetricsRange("node-1", 150, 250)
	if err != nil {
		t.Fatalf("MetricsRange: %v", err)
	}
	if len(points) != 1 || points[0].TS != 200 {
		t.Fatalf("выборка по окну неверна: %+v", points)
	}

	// Повторная вставка той же точки (node_id, ts) не должна плодить дубли.
	if err := st.InsertMetrics1m("node-1", 200, 55, 1, 2, "{}", "{}"); err != nil {
		t.Fatalf("повторная вставка: %v", err)
	}
	points, _ = st.MetricsRange("node-1", 150, 250)
	if len(points) != 1 {
		t.Fatalf("дубль минутной точки: %+v", points)
	}
}

// TestStackRegistry: стек помнит реестр приватных образов (FR-13 × FR-14);
// удаление реестра обнуляет ссылку, а не оставляет висячий ID.
func TestStackRegistry(t *testing.T) {
	st := open(t)
	uid, _ := st.CreateUser("murad", "h", "admin")
	regID, err := st.CreateRegistry("docker.io", "bot", []byte("enc"), "Docker Hub")
	if err != nil {
		t.Fatalf("CreateRegistry: %v", err)
	}

	if err := st.SaveStack("pay", "services: {}", nil, &regID, uid); err != nil {
		t.Fatalf("SaveStack: %v", err)
	}
	got, _ := st.StackByName("pay")
	if got.RegistryID == nil || *got.RegistryID != regID {
		t.Fatalf("registry_id не сохранён: %+v", got.RegistryID)
	}

	// Повторное сохранение без реестра — ссылка снимается.
	if err := st.SaveStack("pay", "services: {}", nil, nil, uid); err != nil {
		t.Fatalf("SaveStack без реестра: %v", err)
	}
	if got, _ = st.StackByName("pay"); got.RegistryID != nil {
		t.Fatalf("registry_id должен сняться: %v", *got.RegistryID)
	}

	// Удаление реестра обнуляет ссылку у стека.
	_ = st.SaveStack("pay", "services: {}", nil, &regID, uid)
	if err := st.DeleteRegistry(regID); err != nil {
		t.Fatalf("DeleteRegistry: %v", err)
	}
	if got, _ = st.StackByName("pay"); got.RegistryID != nil {
		t.Fatalf("после удаления реестра ссылка должна обнулиться: %v", *got.RegistryID)
	}
}
