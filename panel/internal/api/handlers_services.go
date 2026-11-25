// Handlers сервисов (FR-04): список, детали, scale/stop/start,
// redeploy, update image (с registry auth), rollback, remove.
//
// Правила 3.4.3: Stop = scale 0 (реплики запоминаются в service_state),
// Start = scale обратно, Redeploy = ForceUpdate++.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"

	"github.com/xakus/DSMS/panel/internal/metrics"
)

// stackLabel — label группировки по стекам (3.4.2, FR-08).
const stackLabel = "com.docker.stack.namespace"

// serviceView — строка таблицы сервисов (3.4.1).
type serviceView struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Mode     string            `json:"mode"` // replicated | global
	Running  int               `json:"running"`
	Desired  int               `json:"desired"`
	Stack    string            `json:"stack"`
	Ports    []string          `json:"ports"`
	Labels   map[string]string `json:"labels"`
	UpdState string            `json:"update_state,omitempty"` // 3.4.5
	Stopped  bool              `json:"stopped"`                // scale 0 через панель
	// Nodes — hostname нод, где у сервиса есть running-задачи (3.4.1).
	Nodes []string `json:"nodes"`
	// Usage — суммарное потребление running-задач (3.4.7); nil — метрик нет.
	Usage *usageView `json:"usage,omitempty"`
}

// usageView — потребление ресурсов задачи, сервиса или стека (ТЗ 3.4.7).
type usageView struct {
	// CPUCores — занято ядер: 0.35 = 35% одного ядра.
	CPUCores float64 `json:"cpu_cores"`
	MemUsed  uint64  `json:"mem_used"` // байты
	// MemLimit — сумма лимитов задач в байтах; 0 — лимита нет хотя бы у одной.
	MemLimit uint64 `json:"mem_limit"`
	RxBps    uint64 `json:"rx_bps"` // приём, байт/с
	TxBps    uint64 `json:"tx_bps"` // отдача, байт/с
	// Tasks — сколько running-задач дали метрики. Меньше числа running —
	// сумма неполная (агент на какой-то ноде молчит).
	Tasks int `json:"tasks"`
}

// usageAcc складывает потребление нескольких задач в одну сводку.
type usageAcc struct {
	u       usageView
	noLimit bool // встретилась задача без лимита памяти
}

// add прибавляет метрики одной задачи.
func (a *usageAcc) add(m metrics.Task) {
	a.merge(&usageView{
		CPUCores: m.CPUCores, MemUsed: m.MemUsed, MemLimit: m.MemLimit,
		RxBps: m.RxBps, TxBps: m.TxBps, Tasks: 1,
	})
}

// merge прибавляет готовую сводку (сервис → стек). nil — метрик нет, пропуск.
func (a *usageAcc) merge(v *usageView) {
	if v == nil || v.Tasks == 0 {
		return
	}
	a.u.CPUCores += v.CPUCores
	a.u.MemUsed += v.MemUsed
	a.u.RxBps += v.RxBps
	a.u.TxBps += v.TxBps
	a.u.Tasks += v.Tasks
	// Общий лимит имеет смысл, только если он есть у всех: сумма «512 МБ +
	// без лимита» лимитом не является.
	if v.MemLimit == 0 {
		a.noLimit = true
	} else {
		a.u.MemLimit += v.MemLimit
	}
}

// view возвращает сводку или nil, если метрик не было ни по одной задаче
// (в UI это прочерк, а не ноль — ТЗ 3.4.7).
func (a *usageAcc) view() *usageView {
	if a.u.Tasks == 0 {
		return nil
	}
	out := a.u
	if a.noLimit {
		out.MemLimit = 0
	}
	// Сумма float копит хвосты вида 0.30000000000000004 — режем до 3 знаков.
	out.CPUCores = math.Round(out.CPUCores*1000) / 1000
	return &out
}

// taskMetrics возвращает свежие метрики задач: ID задачи → метрики.
// Хранилище не подключено (тесты) — пустая карта.
func (h *handlers) taskMetrics() map[string]metrics.Task {
	if h.TaskStats == nil {
		return map[string]metrics.Task{}
	}
	return h.TaskStats.ByTask()
}

// isRunning — задача реально работает и должна работать.
func isRunning(t swarm.Task) bool {
	return t.Status.State == swarm.TaskStateRunning && t.DesiredState == swarm.TaskStateRunning
}

// buildServiceViews агрегирует сервисы + задачи в представление таблицы.
func (h *handlers) buildServiceViews(r *http.Request) ([]serviceView, error) {
	views, _, err := h.loadServices(r)
	return views, err
}

// loadServices возвращает строки таблицы сервисов и сырые спецификации
// (нужны странице стека для списка сетей/томов/секретов) одним проходом.
func (h *handlers) loadServices(r *http.Request) ([]serviceView, []swarm.Service, error) {
	ctx := r.Context()
	services, err := h.Docker.Services(ctx)
	if err != nil {
		return nil, nil, err
	}
	tasks, err := h.Docker.Tasks(ctx)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := h.Docker.Nodes(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Для global-сервисов desired = количество активных ready-нод.
	activeNodes := 0
	hostname := make(map[string]string, len(nodes))
	for _, n := range nodes {
		hostname[n.ID] = n.Description.Hostname
		if n.Status.State == swarm.NodeStateReady && n.Spec.Availability == swarm.NodeAvailabilityActive {
			activeNodes++
		}
	}

	// По running-задачам: счётчик, ноды размещения и потребление (3.4.7).
	taskUsage := h.taskMetrics()
	running := map[string]int{}
	placed := map[string]map[string]bool{} // service_id → множество hostname
	usage := map[string]*usageAcc{}
	for _, t := range tasks {
		if !isRunning(t) {
			continue
		}
		running[t.ServiceID]++
		if placed[t.ServiceID] == nil {
			placed[t.ServiceID] = map[string]bool{}
			usage[t.ServiceID] = &usageAcc{}
		}
		if hn := hostname[t.NodeID]; hn != "" {
			placed[t.ServiceID][hn] = true
		}
		if m, ok := taskUsage[t.ID]; ok {
			usage[t.ServiceID].add(m)
		}
	}

	out := make([]serviceView, 0, len(services))
	for _, s := range services {
		v := serviceView{
			ID:      s.ID,
			Name:    s.Spec.Name,
			Running: running[s.ID],
			Stack:   s.Spec.Labels[stackLabel],
			Labels:  s.Spec.Labels,
			Nodes:   sortedKeys(placed[s.ID]),
		}
		if cs := s.Spec.TaskTemplate.ContainerSpec; cs != nil {
			v.Image = shortImage(cs.Image)
		}
		if acc := usage[s.ID]; acc != nil {
			v.Usage = acc.view()
		}
		if s.Spec.Mode.Replicated != nil {
			v.Mode = "replicated"
			v.Desired = replicasToInt(s.Spec.Mode.Replicated.Replicas)
		} else {
			v.Mode = "global"
			v.Desired = activeNodes
		}
		if s.UpdateStatus != nil {
			v.UpdState = string(s.UpdateStatus.State)
		}
		v.Ports = portStrings(s.Endpoint.Ports)
		// Сервис «остановлен панелью»: 0 desired + запись в service_state.
		if v.Mode == "replicated" && v.Desired == 0 {
			if _, ok, _ := h.Store.SavedServiceReplicas(s.ID); ok {
				v.Stopped = true
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, services, nil
}

// sortedKeys возвращает ключи множества по алфавиту; пустой срез вместо nil,
// чтобы в JSON уходил [] (фронт не проверяет на null).
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// portStrings форматирует опубликованные порты: «8080:80/tcp».
func portStrings(ports []swarm.PortConfig) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, fmt.Sprintf("%d:%d/%s", p.PublishedPort, p.TargetPort, p.Protocol))
	}
	return out
}

// shortImage отрезает digest от image-ссылки (читаемость таблицы).
func shortImage(img string) string {
	if i := strings.Index(img, "@"); i > 0 {
		return img[:i]
	}
	return img
}

// services — GET /services (3.4.1).
// replicasToInt безопасно приводит число реплик Swarm (uint64) к int для
// ответа API: реальное значение — десятки, но подставленный извне мусор
// не должен превратиться в отрицательное число на экране.
func replicasToInt(r *uint64) int {
	if r == nil || *r > math.MaxInt32 {
		return 0
	}
	return int(*r)
}

func (h *handlers) services(w http.ResponseWriter, r *http.Request) {
	views, err := h.buildServiceViews(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	writeJSON(w, views)
}

// taskView — задача сервиса в ответе /services/{id} (3.4.4).
type taskView struct {
	ID        string `json:"id"`
	Slot      int    `json:"slot"`
	Node      string `json:"node"`    // hostname ноды
	NodeID    string `json:"node_id"` // для ссылки на страницу ноды
	State     string `json:"state"`
	Desired   string `json:"desired"`
	Message   string `json:"message"`
	Image     string `json:"image"` // образ задачи: при обновлении видно, кто на какой версии
	ExitCode  *int   `json:"exit_code,omitempty"`
	CreatedAt string `json:"created_at"`
	// Usage — потребление running-задачи (3.4.7); nil — метрик нет.
	Usage *usageView `json:"usage,omitempty"`
}

// resourcesView — лимиты или резервы сервиса.
type resourcesView struct {
	CPUs   float64 `json:"cpus"`   // ядра; 0 — не задано
	Memory int64   `json:"memory"` // байты; 0 — не задано
}

// specView — спецификация сервиса read-only (3.4.4).
type specView struct {
	Image        string            `json:"image"`
	Digest       string            `json:"digest,omitempty"` // sha256, в который разрешился тег
	Command      []string          `json:"command"`
	Args         []string          `json:"args"`
	Env          []string          `json:"env"`
	Mounts       []string          `json:"mounts"`
	Networks     []string          `json:"networks"`
	Ports        []string          `json:"ports"`
	Constraints  []string          `json:"constraints"`
	Secrets      []string          `json:"secrets"`
	Configs      []string          `json:"configs"`
	Labels       map[string]string `json:"labels"`
	Limits       *resourcesView    `json:"limits,omitempty"`
	Reservations *resourcesView    `json:"reservations,omitempty"`
	Restart      *restartView      `json:"restart,omitempty"`
	Update       *updateCfgView    `json:"update_config,omitempty"`
	Healthcheck  []string          `json:"healthcheck"`
	User         string            `json:"user,omitempty"`
	Hostname     string            `json:"hostname,omitempty"`
}

// restartView — политика перезапуска задач.
type restartView struct {
	Condition   string  `json:"condition"`
	DelaySec    float64 `json:"delay_sec"`    // пауза между попытками, с
	MaxAttempts uint64  `json:"max_attempts"` // 0 — без ограничения
}

// updateCfgView — правила раскатки обновления (из compose `update_config`).
type updateCfgView struct {
	Parallelism   uint64  `json:"parallelism"` // сколько задач обновлять разом; 0 — все
	DelaySec      float64 `json:"delay_sec"`   // пауза между пачками, с
	Order         string  `json:"order"`       // stop-first | start-first
	FailureAction string  `json:"failure_action"`
}

// orEmpty заменяет nil пустым срезом — в JSON уходит [], а не null.
func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// imageDigest достаёт «sha256:…» из image-ссылки вида repo:tag@sha256:….
func imageDigest(img string) string {
	if i := strings.Index(img, "@"); i >= 0 {
		return img[i+1:]
	}
	return ""
}

// resources переводит лимиты/резервы Swarm в ядра и байты; nil — не заданы.
// NanoCPUs — миллиардные доли ядра: 500 000 000 = 0.5 ядра.
func resources(nanoCPUs, memBytes int64) *resourcesView {
	if nanoCPUs == 0 && memBytes == 0 {
		return nil
	}
	return &resourcesView{CPUs: float64(nanoCPUs) / 1e9, Memory: memBytes}
}

// buildSpecView собирает read-only спецификацию сервиса.
// netName — справочник «ID сети → имя» (в спеке сервиса лежат ID).
func buildSpecView(svc swarm.Service, netName map[string]string) specView {
	// ContainerSpec и Placement — указатели: у сервиса без deploy.placement
	// (а это норма) Placement == nil, и обращение к полю роняло обработчик.
	tt := svc.Spec.TaskTemplate
	cs := tt.ContainerSpec
	if cs == nil {
		cs = &swarm.ContainerSpec{}
	}
	v := specView{
		Image:       shortImage(cs.Image),
		Digest:      imageDigest(cs.Image),
		Command:     orEmpty(cs.Command),
		Args:        orEmpty(cs.Args),
		Env:         orEmpty(cs.Env),
		Mounts:      []string{},
		Networks:    []string{},
		Ports:       portStrings(svc.Endpoint.Ports),
		Constraints: []string{},
		Secrets:     []string{},
		Configs:     []string{},
		Labels:      svc.Spec.Labels,
		Healthcheck: []string{},
		User:        cs.User,
		Hostname:    cs.Hostname,
	}
	if p := tt.Placement; p != nil {
		v.Constraints = orEmpty(p.Constraints)
	}
	for _, m := range cs.Mounts {
		mode := "rw"
		if m.ReadOnly {
			mode = "ro"
		}
		v.Mounts = append(v.Mounts, fmt.Sprintf("%s:%s (%s, %s)", m.Source, m.Target, m.Type, mode))
	}
	for _, n := range tt.Networks {
		name := netName[n.Target]
		if name == "" {
			name = n.Target // сеть уже удалена или в спеке лежит имя
		}
		v.Networks = append(v.Networks, name)
	}
	for _, ref := range cs.Secrets {
		v.Secrets = append(v.Secrets, ref.SecretName)
	}
	for _, ref := range cs.Configs {
		v.Configs = append(v.Configs, ref.ConfigName)
	}
	if hc := cs.Healthcheck; hc != nil {
		v.Healthcheck = orEmpty(hc.Test)
	}
	if res := tt.Resources; res != nil {
		if l := res.Limits; l != nil {
			v.Limits = resources(l.NanoCPUs, l.MemoryBytes)
		}
		if rv := res.Reservations; rv != nil {
			v.Reservations = resources(rv.NanoCPUs, rv.MemoryBytes)
		}
	}
	if rp := tt.RestartPolicy; rp != nil {
		rvw := &restartView{Condition: string(rp.Condition)}
		if rp.Delay != nil {
			rvw.DelaySec = rp.Delay.Seconds()
		}
		if rp.MaxAttempts != nil {
			rvw.MaxAttempts = *rp.MaxAttempts
		}
		v.Restart = rvw
	}
	if uc := svc.Spec.UpdateConfig; uc != nil {
		v.Update = &updateCfgView{
			Parallelism:   uc.Parallelism,
			DelaySec:      uc.Delay.Seconds(),
			Order:         uc.Order,
			FailureAction: uc.FailureAction,
		}
	}
	return v
}

// serviceDetail — GET /services/{id}: сводка, спецификация, задачи с
// историей и потреблением (3.4.4, 3.4.7).
func (h *handlers) serviceDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	svc, err := h.Docker.ServiceInspect(ctx, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	tasks, err := h.Docker.ServiceTasks(ctx, id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	// Справочники имён: ноды (для задач) и сети (в спеке лежат ID).
	// Недоступность справочника не повод ронять страницу — покажем ID.
	nodeName := map[string]string{}
	activeNodes := 0
	if nodes, err := h.Docker.Nodes(ctx); err == nil {
		for _, n := range nodes {
			nodeName[n.ID] = n.Description.Hostname
			if n.Status.State == swarm.NodeStateReady && n.Spec.Availability == swarm.NodeAvailabilityActive {
				activeNodes++
			}
		}
	}
	netName := map[string]string{}
	if nets, err := h.Docker.Networks(ctx); err == nil {
		for _, n := range nets {
			netName[n.ID] = n.Name
		}
	}

	taskUsage := h.taskMetrics()
	total := &usageAcc{}
	placed := map[string]bool{}
	running := 0
	tv := make([]taskView, 0, len(tasks))
	for _, t := range tasks {
		item := taskView{
			ID:        t.ID,
			Slot:      t.Slot,
			Node:      nodeName[t.NodeID],
			NodeID:    t.NodeID,
			State:     string(t.Status.State),
			Desired:   string(t.DesiredState),
			Message:   t.Status.Message,
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		}
		if tcs := t.Spec.ContainerSpec; tcs != nil {
			item.Image = shortImage(tcs.Image)
		}
		if t.Status.ContainerStatus != nil && t.Status.State == swarm.TaskStateFailed {
			ec := t.Status.ContainerStatus.ExitCode
			item.ExitCode = &ec
		}
		if isRunning(t) {
			running++
			if item.Node != "" {
				placed[item.Node] = true
			}
			if m, ok := taskUsage[t.ID]; ok {
				one := &usageAcc{}
				one.add(m)
				item.Usage = one.view()
				total.add(m)
			}
		}
		tv = append(tv, item)
	}
	// Свежие сверху (история обновлений, 3.4.4).
	sort.Slice(tv, func(i, j int) bool { return tv[i].CreatedAt > tv[j].CreatedAt })

	view := map[string]any{
		"id":         svc.ID,
		"name":       svc.Spec.Name,
		"stack":      svc.Spec.Labels[stackLabel],
		"spec":       buildSpecView(svc, netName),
		"tasks":      tv,
		"running":    running,
		"nodes":      sortedKeys(placed),
		"usage":      total.view(),
		"stopped":    false,
		"created_at": svc.CreatedAt.Format(time.RFC3339),
		"updated_at": svc.UpdatedAt.Format(time.RFC3339),
		"update":     nil,
	}
	if rep := svc.Spec.Mode.Replicated; rep != nil {
		view["mode"] = "replicated"
		desired := replicasToInt(rep.Replicas)
		view["desired"] = desired
		if desired == 0 {
			if _, ok, _ := h.Store.SavedServiceReplicas(svc.ID); ok {
				view["stopped"] = true
			}
		}
	} else {
		view["mode"] = "global"
		view["desired"] = activeNodes
	}
	if svc.UpdateStatus != nil {
		view["update"] = map[string]any{
			"state":   string(svc.UpdateStatus.State),
			"message": svc.UpdateStatus.Message,
		}
	}
	writeJSON(w, view)
}

// Причины, по которым Stop/Start сервиса не выполняется. Для одного сервиса
// это ответ 409, для стека — запись в «пропущено» (ТЗ 3.8.6).
var (
	errGlobalService  = errors.New("cannot scale a global service")
	errAlreadyStopped = errors.New("service is already stopped")
	errNotStopped     = errors.New("service was not stopped via panel")
)

// withReplicas возвращает копию спецификации с новым числом реплик.
// ReplicatedService копируется, чтобы не править объект, общий с svc.Spec.
func withReplicas(spec swarm.ServiceSpec, n uint64) swarm.ServiceSpec {
	rep := *spec.Mode.Replicated
	rep.Replicas = &n
	spec.Mode.Replicated = &rep
	return spec
}

// stopService — Stop (3.4.3): scale 0 с запоминанием прежнего числа реплик.
// Общий путь для кнопки сервиса и для «Остановить стек».
//
// Ошибки: errGlobalService — у global-сервиса реплик нет; errAlreadyStopped —
// реплик уже 0 (запоминать нечего, прошлую запись не затираем нулём).
// Побочные эффекты: запись в service_state, ServiceUpdate в Docker.
func (h *handlers) stopService(ctx context.Context, svc swarm.Service) error {
	rep := svc.Spec.Mode.Replicated
	if rep == nil {
		return errGlobalService
	}
	if rep.Replicas == nil || *rep.Replicas == 0 {
		return errAlreadyStopped
	}
	if err := h.Store.SaveServiceReplicas(svc.ID, *rep.Replicas); err != nil {
		return fmt.Errorf("storage error: %w", err)
	}
	if _, err := h.Docker.ServiceUpdate(ctx, svc.ID, svc.Version, withReplicas(svc.Spec, 0), "", ""); err != nil {
		// Сервис остался работать — запись «остановлен» была бы ложью:
		// позже сервис с 0 реплик приняли бы за остановленный панелью.
		_ = h.Store.DeleteServiceState(svc.ID)
		return err
	}
	return nil
}

// startService — Start (3.4.3): возвращает число реплик, запомненное при Stop.
//
// Ошибки: errGlobalService; errNotStopped — панель этот сервис не
// останавливала (или он уже снова работает — тогда устаревшая запись
// удаляется, чтобы не «вернуть» ему старое число реплик поверх текущего).
func (h *handlers) startService(ctx context.Context, svc swarm.Service) (uint64, error) {
	rep := svc.Spec.Mode.Replicated
	if rep == nil {
		return 0, errGlobalService
	}
	saved, ok, err := h.Store.SavedServiceReplicas(svc.ID)
	if err != nil {
		return 0, fmt.Errorf("storage error: %w", err)
	}
	if !ok {
		return 0, errNotStopped
	}
	if rep.Replicas != nil && *rep.Replicas > 0 {
		_ = h.Store.DeleteServiceState(svc.ID)
		return 0, errNotStopped
	}
	if _, err := h.Docker.ServiceUpdate(ctx, svc.ID, svc.Version, withReplicas(svc.Spec, saved), "", ""); err != nil {
		return 0, err
	}
	_ = h.Store.DeleteServiceState(svc.ID)
	return saved, nil
}

// serviceScale — POST /services/{id}/scale {replicas} (3.4.3 Scale/Stop).
// replicas == 0 — это Stop: прежнее значение запоминается для Start.
func (h *handlers) serviceScale(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Replicas *uint64 `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Replicas == nil {
		writeErr(w, http.StatusBadRequest, "replicas required")
		return
	}
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		writeErr(w, http.StatusConflict, errGlobalService.Error())
		return
	}

	if *req.Replicas == 0 {
		// Повторный Stop — не ошибка: сервис уже в нужном состоянии.
		if err := h.stopService(r.Context(), svc); err != nil && !errors.Is(err, errAlreadyStopped) {
			slog.Error("service stop failed", "service", id, "err", err)
			writeErr(w, http.StatusBadGateway, "service update failed")
			return
		}
	} else {
		if _, err := h.Docker.ServiceUpdate(r.Context(), id, svc.Version,
			withReplicas(svc.Spec, *req.Replicas), "", ""); err != nil {
			writeErr(w, http.StatusBadGateway, "service update failed")
			return
		}
		_ = h.Store.DeleteServiceState(id) // сервис снова живой
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.scale", "service", id,
		fmt.Sprintf(`{"replicas":%d}`, *req.Replicas))
	writeJSON(w, map[string]string{"status": "ok"})
}

// serviceStart — POST /services/{id}/start: scale обратно к запомненному (3.4.3).
func (h *handlers) serviceStart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	saved, err := h.startService(r.Context(), svc)
	switch {
	case errors.Is(err, errGlobalService), errors.Is(err, errNotStopped):
		writeErr(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.Error("service start failed", "service", id, "err", err)
		writeErr(w, http.StatusBadGateway, "service update failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.start", "service", id,
		fmt.Sprintf(`{"replicas":%d}`, saved))
	writeJSON(w, map[string]string{"status": "ok"})
}

// serviceRedeploy — POST /services/{id}/redeploy: ForceUpdate++ (3.4.3).
func (h *handlers) serviceRedeploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.forceUpdate(r, id); err != nil {
		writeErr(w, http.StatusBadGateway, "redeploy failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.redeploy", "service", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// forceUpdate инкрементирует ForceUpdate спецификации — аналог
// docker service update --force.
func (h *handlers) forceUpdate(r *http.Request, id string) error {
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		return err
	}
	spec := svc.Spec
	spec.TaskTemplate.ForceUpdate++
	_, err = h.Docker.ServiceUpdate(r.Context(), id, svc.Version, spec, "", "")
	return err
}

// serviceDeploy — POST /services/{id}/deploy {registry_id?}: тянет свежую
// версию образа по текущему тегу и катит обновление по правилам стека.
// Отличие от redeploy: сбрасывается pinned digest и включается QueryRegistry,
// поэтому Swarm заново резолвит тег в реестре (аналог
// `docker service update --image repo:tag`). Порядок отката задач берётся из
// UpdateConfig сервиса (compose), панель его не переопределяет.
func (h *handlers) serviceDeploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// registry_id опционален — для приватных реестров (FR-13).
	var req struct {
		RegistryID *int64 `json:"registry_id"`
	}
	// Тело может отсутствовать — это не ошибка.
	_ = json.NewDecoder(r.Body).Decode(&req)

	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}

	regAuth := ""
	if req.RegistryID != nil {
		regAuth, err = h.registryAuthHeader(*req.RegistryID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "registry auth failed")
			return
		}
	}

	if err := h.deployService(r, svc, regAuth); err != nil {
		writeErr(w, http.StatusBadGateway, "deploy failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.deploy", "service", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// deployService катит один сервис на свежий образ: сбрасывает pinned
// digest и форсит перекат. Переиспользуется serviceDeploy и stackDeploy (FR-08).
//
// ВАЖНО: UpdateConfig (order/parallelism) НЕ трогаем — уважаем то, что задано
// в стеке/compose. Раньше здесь навязывался start-first, и это приводило к
// накоплению задач: на нодах с нехваткой памяти новая реплика не поднималась
// («insufficient resources»), update зависал в состоянии updating, старая
// задача не убивалась, а каждый повторный деплой добавлял ещё одну задачу
// (реплики росли как 8/1, 4/1). Поведение = `docker service update --image`.
func (h *handlers) deployService(r *http.Request, svc swarm.Service, regAuth string) error {
	spec := svc.Spec
	// Отрезаем @sha256:... — иначе Swarm дёрнет тот же самый образ.
	spec.TaskTemplate.ContainerSpec.Image = shortImage(spec.TaskTemplate.ContainerSpec.Image)
	// Форсим перекат, даже если digest не изменился.
	spec.TaskTemplate.ForceUpdate++

	_, err := h.Docker.ServiceDeploy(r.Context(), svc.ID, svc.Version, spec, regAuth)
	return err
}

// serviceImage — POST /services/{id}/image {image, registry_id?} (3.4.3, FR-13).
func (h *handlers) serviceImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Image      string `json:"image"`
		RegistryID *int64 `json:"registry_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Image == "" {
		writeErr(w, http.StatusBadRequest, "image required")
		return
	}
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}

	// Собрать X-Registry-Auth из сохранённого реестра (3.13.3).
	regAuth := ""
	if req.RegistryID != nil {
		regAuth, err = h.registryAuthHeader(*req.RegistryID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "registry auth failed")
			return
		}
	}

	spec := svc.Spec
	spec.TaskTemplate.ContainerSpec.Image = req.Image
	if _, err := h.Docker.ServiceUpdate(r.Context(), id, svc.Version, spec, regAuth, ""); err != nil {
		writeErr(w, http.StatusBadGateway, "image update failed")
		return
	}
	s := sessionFrom(r)
	// В audit — только имя образа; учётные данные не пишутся (3.13.4).
	_ = h.Store.AppendAudit(s.UserID, "service.image", "service", id,
		fmt.Sprintf(`{"image":%q}`, req.Image))
	writeJSON(w, map[string]string{"status": "ok"})
}

// registryAuthHeader строит base64 X-Registry-Auth из реестра FR-13.
func (h *handlers) registryAuthHeader(registryID int64) (string, error) {
	if h.Crypto == nil {
		return "", fmt.Errorf("encryption key not configured")
	}
	reg, err := h.Store.RegistryByID(registryID)
	if err != nil {
		return "", err
	}
	password, err := h.Crypto.Open(reg.PasswordEnc)
	if err != nil {
		return "", err
	}
	authJSON, err := json.Marshal(registry.AuthConfig{
		Username:      reg.Username,
		Password:      string(password),
		ServerAddress: reg.Address,
	})
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(authJSON), nil
}

// serviceRollback — POST /services/{id}/rollback (3.4.3).
func (h *handlers) serviceRollback(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if _, err := h.Docker.ServiceUpdate(r.Context(), id, svc.Version, svc.Spec, "", "previous"); err != nil {
		writeErr(w, http.StatusBadGateway, "rollback failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.rollback", "service", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// serviceRemove — DELETE /services/{id} (3.4.3 Remove; подтверждение — в UI).
func (h *handlers) serviceRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Docker.ServiceRemove(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadGateway, "remove failed")
		return
	}
	_ = h.Store.DeleteServiceState(id)
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.remove", "service", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}
