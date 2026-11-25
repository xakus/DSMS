// Handlers стеков (FR-08): стек — не объект Docker, а агрегация сервисов
// по label com.docker.stack.namespace (3.8.1).
//
// Групповые действия (stop/start/redeploy/deploy/remove) не обрываются на
// первой ошибке: проходят все сервисы и отдают сводку «сделано / пропущено /
// упало» (3.8.6) — иначе непонятно, в каком состоянии остался стек.
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
)

// noStackGroup — псевдо-стек для сервисов без label стека. Скобки и пробел
// в настоящем имени стека невозможны (stackNameRe), пересечения не будет.
const noStackGroup = "(no stack)"

// maxStackNameLen — предел длины имени стека (ТЗ 3.14.13). Имя сервиса Swarm
// = «<стек>_<сервис>» и ограничено 63 символами; 50 оставляет место сервису.
const maxStackNameLen = 50

// stackNameRe — допустимое имя стека (ТЗ 3.14.13): латиница, цифры, «_» и
// «-», первая — буква или цифра. Те же символы Swarm разрешает в именах
// сервисов, которые из имени стека строятся.
var stackNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// validStackName проверяет имя стека по ТЗ 3.14.13.
func validStackName(name string) bool {
	return len(name) <= maxStackNameLen && stackNameRe.MatchString(name)
}

// Коды причин пропуска сервиса в групповой операции (ТЗ 3.8.6).
// Текст для пользователя — на фронте, по коду.
const (
	skipGlobal         = "global"          // global-сервис: scale 0 невозможен
	skipAlreadyStopped = "already_stopped" // реплик уже 0
	skipNotStopped     = "not_stopped"     // панель его не останавливала
)

// stackSkip — сервис, над которым действие не выполнялось, и почему.
type stackSkip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"` // один из skip*-кодов
}

// stackOpResult — сводка групповой операции над стеком (ТЗ 3.8.6).
type stackOpResult struct {
	Done    []string    `json:"done"`    // сервисы, над которыми действие выполнено
	Skipped []stackSkip `json:"skipped"` // пропущенные с кодом причины
	Failed  []stackFail `json:"failed"`  // ошибки Docker: имя + текст
	// Resources — удалённые сети/secrets/configs (только для remove).
	Resources []string `json:"resources"`
}

// newStackOpResult — сводка с пустыми срезами: в JSON уходят [], а не null.
func newStackOpResult() stackOpResult {
	return stackOpResult{
		Done: []string{}, Skipped: []stackSkip{}, Failed: []stackFail{}, Resources: []string{},
	}
}

// auditDetails — числа сводки для журнала действий (3.8.5).
func (r stackOpResult) auditDetails() string {
	return fmt.Sprintf(`{"done":%d,"skipped":%d,"failed":%d,"resources":%d}`,
		len(r.Done), len(r.Skipped), len(r.Failed), len(r.Resources))
}

// stackView — строка таблицы стеков (3.8.1) и шапка страницы стека.
type stackView struct {
	Name     string `json:"name"`
	Services int    `json:"services"`
	Running  int    `json:"running"`
	Desired  int    `json:"desired"`
	Managed  bool   `json:"managed"` // развёрнут из файла (FR-14): есть исходник в БД
	// Stopped — сколько сервисов остановлено панелью (их вернёт Start).
	Stopped int `json:"stopped"`
	// Stoppable — сколько сервисов можно остановить: replicated с репликами > 0.
	Stoppable int `json:"stoppable"`
	// Usage — суммарное потребление сервисов стека (3.4.7); nil — метрик нет.
	Usage *usageView `json:"usage,omitempty"`
}

// summarizeStack сворачивает сервисы стека в одну строку.
func summarizeStack(name string, services []serviceView) stackView {
	sv := stackView{Name: name}
	acc := &usageAcc{}
	for _, v := range services {
		sv.Services++
		sv.Running += v.Running
		sv.Desired += v.Desired
		if v.Stopped {
			sv.Stopped++
		}
		if v.Mode == "replicated" && v.Desired > 0 {
			sv.Stoppable++
		}
		acc.merge(v.Usage)
	}
	sv.Usage = acc.view()
	return sv
}

// stacks — GET /stacks: агрегированный список (3.8.1).
func (h *handlers) stacks(w http.ResponseWriter, r *http.Request) {
	views, err := h.buildServiceViews(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	groups := map[string][]serviceView{}
	for _, v := range views {
		name := v.Stack
		if name == "" {
			name = noStackGroup // сервисы вне стеков — отдельная группа
		}
		groups[name] = append(groups[name], v)
	}
	agg := map[string]*stackView{}
	for name, svcs := range groups {
		sv := summarizeStack(name, svcs)
		agg[name] = &sv
	}
	// Мёржим managed-стеки из БД: помечаем существующие и добавляем те, что
	// ещё без живых сервисов (только что сохранён исходник / деплой упал).
	if managed, err := h.Store.ManagedStacks(); err == nil {
		for _, m := range managed {
			if sv, ok := agg[m.Name]; ok {
				sv.Managed = true
			} else {
				agg[m.Name] = &stackView{Name: m.Name, Managed: true}
			}
		}
	}
	out := make([]stackView, 0, len(agg))
	for _, sv := range agg {
		out = append(out, *sv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

// isManagedStack — есть ли у стека сохранённый исходник (FR-14).
func (h *handlers) isManagedStack(name string) bool {
	_, err := h.Store.StackByName(name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("stack lookup failed", "stack", name, "err", err)
	}
	return err == nil
}

// stackResource — сеть/secret/config, задействованный стеком (3.8.2).
type stackResource struct {
	Name string `json:"name"`
	// External — объект не принадлежит стеку (нет label стека): при удалении
	// стека он останется.
	External bool `json:"external"`
}

// resourceList превращает «имя → принадлежит стеку» в отсортированный список.
func resourceList(owned map[string]bool) []stackResource {
	out := make([]stackResource, 0, len(owned))
	for name, own := range owned {
		out = append(out, stackResource{Name: name, External: !own})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// stackObjects — объекты кластера с label стека: то, что создано вместе со
// стеком и удаляется вместе с ним (3.8.3).
type stackObjects struct {
	networks map[string]string // имя → ID
	secrets  map[string]string
	configs  map[string]string
}

// empty — у стека нет собственных сетей, secrets и configs.
func (o stackObjects) empty() bool {
	return len(o.networks) == 0 && len(o.secrets) == 0 && len(o.configs) == 0
}

// stackOwned собирает сети, secrets и configs с label стека.
// Заодно отдаёт справочник «ID сети → имя» (в спеках сервисов лежат ID).
func (h *handlers) stackOwned(ctx context.Context, stack string) (stackObjects, map[string]string, error) {
	own := stackObjects{
		networks: map[string]string{}, secrets: map[string]string{}, configs: map[string]string{},
	}
	nets, err := h.Docker.Networks(ctx)
	if err != nil {
		return own, nil, err
	}
	netName := make(map[string]string, len(nets))
	for _, n := range nets {
		netName[n.ID] = n.Name
		if n.Labels[stackLabel] == stack {
			own.networks[n.Name] = n.ID
		}
	}
	secrets, err := h.Docker.Secrets(ctx)
	if err != nil {
		return own, nil, err
	}
	for _, s := range secrets {
		if s.Spec.Labels[stackLabel] == stack {
			own.secrets[s.Spec.Name] = s.ID
		}
	}
	configs, err := h.Docker.Configs(ctx)
	if err != nil {
		return own, nil, err
	}
	for _, c := range configs {
		if c.Spec.Labels[stackLabel] == stack {
			own.configs[c.Spec.Name] = c.ID
		}
	}
	return own, netName, nil
}

// stackDetail — GET /stacks/{name}: сводка, сервисы и задействованные
// сети/тома/секреты/конфиги (3.8.2).
func (h *handlers) stackDetail(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	views, raw, err := h.loadServices(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	services := make([]serviceView, 0)
	for _, v := range views {
		if v.Stack == name {
			services = append(services, v)
		}
	}
	own, netName, err := h.stackOwned(r.Context(), name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	managed := h.isManagedStack(name)
	if len(services) == 0 && own.empty() && !managed {
		writeErr(w, http.StatusNotFound, "stack not found")
		return
	}

	// Ресурсы: свои (с label стека) + те, на которые ссылаются сервисы.
	networks := map[string]bool{} // имя → принадлежит стеку
	secrets := map[string]bool{}
	configs := map[string]bool{}
	volumes := map[string]bool{}
	for n := range own.networks {
		networks[n] = true
	}
	for n := range own.secrets {
		secrets[n] = true
	}
	for n := range own.configs {
		configs[n] = true
	}
	for _, svc := range raw {
		if svc.Spec.Labels[stackLabel] != name {
			continue
		}
		for _, n := range svc.Spec.TaskTemplate.Networks {
			nn := netName[n.Target]
			if nn == "" {
				nn = n.Target
			}
			if _, seen := networks[nn]; !seen {
				networks[nn] = false
			}
		}
		cs := svc.Spec.TaskTemplate.ContainerSpec
		if cs == nil {
			continue
		}
		for _, ref := range cs.Secrets {
			if _, seen := secrets[ref.SecretName]; !seen {
				secrets[ref.SecretName] = false
			}
		}
		for _, ref := range cs.Configs {
			if _, seen := configs[ref.ConfigName]; !seen {
				configs[ref.ConfigName] = false
			}
		}
		for _, m := range cs.Mounts {
			if m.Type == mount.TypeVolume && m.Source != "" {
				volumes[m.Source] = true
			}
		}
	}

	sv := summarizeStack(name, services)
	sv.Managed = managed
	writeJSON(w, map[string]any{
		"name":      sv.Name,
		"managed":   sv.Managed,
		"running":   sv.Running,
		"desired":   sv.Desired,
		"stopped":   sv.Stopped,
		"stoppable": sv.Stoppable,
		"usage":     sv.Usage,
		"services":  services,
		"networks":  resourceList(networks),
		"secrets":   resourceList(secrets),
		"configs":   resourceList(configs),
		"volumes":   sortedKeys(volumes),
	})
}

// stackOp — общий каркас групповой операции над сервисами стека (3.8.6).
//
// fn вызывается для каждого сервиса по алфавиту и возвращает либо код
// причины пропуска (skip*), либо ошибку Docker, либо ("", nil) — «сделано».
// Ошибка одного сервиса остальные не останавливает. Итог пишется в audit
// (action = "stack.<op>") и отдаётся клиенту со статусом 200.
func (h *handlers) stackOp(w http.ResponseWriter, r *http.Request, op string,
	fn func(svc swarm.Service) (skip string, err error)) {

	name := chi.URLParam(r, "name")
	all, err := h.Docker.Services(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	own := stackServices(all, name)
	if len(own) == 0 {
		writeErr(w, http.StatusNotFound, "stack has no services")
		return
	}
	res := newStackOpResult()
	for _, svcName := range sortedServiceNames(own) {
		skip, err := fn(own[svcName])
		switch {
		case err != nil:
			slog.Warn("stack operation failed on service", "op", op, "stack", name, "service", svcName, "err", err)
			res.Failed = append(res.Failed, stackFail{Name: svcName, Error: err.Error()})
		case skip != "":
			res.Skipped = append(res.Skipped, stackSkip{Name: svcName, Reason: skip})
		default:
			res.Done = append(res.Done, svcName)
		}
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "stack."+op, "stack", name, res.auditDetails())
	writeJSON(w, res)
}

// sortedServiceNames — имена сервисов по алфавиту (стабильный порядок действий).
func sortedServiceNames(m map[string]swarm.Service) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// stackStop — POST /stacks/{name}/stop: Stop каждому replicated-сервису
// стека (3.8.3). Global-сервисы scale 0 не поддерживают — попадают в skipped.
func (h *handlers) stackStop(w http.ResponseWriter, r *http.Request) {
	h.stackOp(w, r, "stop", func(svc swarm.Service) (string, error) {
		err := h.stopService(r.Context(), svc)
		switch {
		case errors.Is(err, errGlobalService):
			return skipGlobal, nil
		case errors.Is(err, errAlreadyStopped):
			return skipAlreadyStopped, nil
		}
		return "", err
	})
}

// stackStart — POST /stacks/{name}/start: Start каждому сервису стека,
// остановленному панелью (3.8.3). Остальные не трогает: у работающих делать
// нечего, а у сервиса с 0 реплик «не от панели» прежнее число неизвестно.
func (h *handlers) stackStart(w http.ResponseWriter, r *http.Request) {
	h.stackOp(w, r, "start", func(svc swarm.Service) (string, error) {
		_, err := h.startService(r.Context(), svc)
		if errors.Is(err, errGlobalService) || errors.Is(err, errNotStopped) {
			return skipNotStopped, nil
		}
		return "", err
	})
}

// stackRedeploy — POST /stacks/{name}/redeploy: ForceUpdate++ каждому (3.8.3).
func (h *handlers) stackRedeploy(w http.ResponseWriter, r *http.Request) {
	h.stackOp(w, r, "redeploy", func(svc swarm.Service) (string, error) {
		spec := svc.Spec
		spec.TaskTemplate.ForceUpdate++
		_, err := h.Docker.ServiceUpdate(r.Context(), svc.ID, svc.Version, spec, "", "")
		return "", err
	})
}

// stackDeploy — POST /stacks/{name}/deploy: каждому сервису стека тянет
// свежий образ по тегу. Отличие от stackRedeploy: сбрасывается pinned digest,
// Swarm заново резолвит теги. Порядок отката — из UpdateConfig стека.
func (h *handlers) stackDeploy(w http.ResponseWriter, r *http.Request) {
	// Стек из файла с реестром FR-13 — приватные образы тянем с авторизацией.
	regAuth := ""
	if st, err := h.Store.StackByName(chi.URLParam(r, "name")); err == nil && st.RegistryID != nil {
		if regAuth, err = h.registryAuthHeader(*st.RegistryID); err != nil {
			writeErr(w, http.StatusBadRequest, "registry auth failed")
			return
		}
	}
	h.stackOp(w, r, "deploy", func(svc swarm.Service) (string, error) {
		return "", h.deployService(r, svc, regAuth)
	})
}

// stackRemove — DELETE /stacks/{name}: удаление объектов стека (3.8.3) в
// порядке `docker stack rm`: сервисы → secrets → configs → сети. Тома не
// удаляются — в них данные. Двойное подтверждение с вводом имени — в UI.
//
// Стек существует, если у него есть сервисы, объекты с label стека или
// запись исходника в БД: managed-стек без сервисов (деплой упал целиком)
// тоже должен удаляться, иначе он остаётся в списке навсегда.
func (h *handlers) stackRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")
	all, err := h.Docker.Services(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	services := stackServices(all, name)
	own, _, err := h.stackOwned(ctx, name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	managed := h.isManagedStack(name)
	if len(services) == 0 && own.empty() && !managed {
		writeErr(w, http.StatusNotFound, "stack not found")
		return
	}

	res := newStackOpResult()
	for _, svcName := range sortedServiceNames(services) {
		svc := services[svcName]
		if err := h.Docker.ServiceRemove(ctx, svc.ID); err != nil && !cerrdefs.IsNotFound(err) {
			res.Failed = append(res.Failed, stackFail{Name: svcName, Error: err.Error()})
			continue
		}
		_ = h.Store.DeleteServiceState(svc.ID)
		res.Done = append(res.Done, svcName)
	}

	// Сети/secrets/configs — только когда все сервисы стека удалены: живой
	// сервис их держит, Docker ответил бы ошибкой на каждый объект.
	if len(res.Failed) == 0 {
		h.removeStackObjects(ctx, own, &res)
	}
	// Запись исходника убираем только при полном успехе: пока что-то
	// осталось, стек должен быть виден в списке, чтобы удаление повторили.
	if len(res.Failed) == 0 {
		_ = h.Store.DeleteStack(name)
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "stack.remove", "stack", name, res.auditDetails())
	writeJSON(w, res)
}

// removeStackObjects удаляет secrets, configs и сети стека; ошибка одного
// объекта (например, secret занят чужим сервисом) остальные не останавливает.
func (h *handlers) removeStackObjects(ctx context.Context, own stackObjects, res *stackOpResult) {
	remove := func(kind string, objs map[string]string, rm func(context.Context, string) error) {
		for _, name := range sortedKeysOf(objs) {
			label := kind + " " + name
			// «Не найдено» — не сбой: объекта уже нет, цель достигнута. Так
			// бывает с overlay-сетями: Swarm убирает их не сразу, и сеть ещё
			// видна в списке, когда запись о ней уже удалена.
			if err := rm(ctx, objs[name]); err != nil && !cerrdefs.IsNotFound(err) {
				res.Failed = append(res.Failed, stackFail{Name: label, Error: err.Error()})
				continue
			}
			res.Resources = append(res.Resources, label)
		}
	}
	remove("secret", own.secrets, h.Docker.SecretRemove)
	remove("config", own.configs, h.Docker.ConfigRemove)
	remove("network", own.networks, h.Docker.NetworkRemove)
}

// sortedKeysOf — ключи карты «имя → ID» по алфавиту.
func sortedKeysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
