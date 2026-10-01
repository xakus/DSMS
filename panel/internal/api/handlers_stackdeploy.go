// Деплой стека из compose-файла (FR-14): валидация, разворачивание
// (сети → сервисы → prune), сохранение исходника в БД, отдача для редактирования.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"

	"github.com/xakus/DSMS/panel/internal/dockerapi"
	"github.com/xakus/DSMS/panel/internal/stackspec"
)

// stackDeployReq — тело для validate/create/update.
type stackDeployReq struct {
	Name        string            `json:"name"`
	ComposeYAML string            `json:"compose_yaml"`
	DotEnv      string            `json:"env"`      // содержимое .env-файла
	EnvVars     map[string]string `json:"env_vars"` // ручные пары (перекрывают .env)
	Prune       bool              `json:"prune"`    // удалять сервисы, ушедшие из файла
	// RegistryID — реестр FR-13 для приватных образов (X-Registry-Auth при
	// create/update сервисов); запоминается у стека и используется «Деплоем стека».
	RegistryID *int64 `json:"registry_id"`
}

// storedEnv — то, что кладём в БД (шифруется целиком): исходный .env + ручные пары.
type storedEnv struct {
	DotEnv string            `json:"dotenv"`
	Vars   map[string]string `json:"vars"`
}

// buildStack: env → парсинг → конвертация в swarm-спеки.
func buildStack(name, yaml, dotenv string, manual map[string]string) (*stackspec.Converted, error) {
	env, err := stackspec.ParseEnv(dotenv, manual)
	if err != nil {
		return nil, err
	}
	proj, err := stackspec.Load(name, []byte(yaml), env)
	if err != nil {
		return nil, err
	}
	return stackspec.Convert(name, proj, env)
}

// maxStackBody — лимит на тело запросов со стеком (ТЗ 3.14.11): compose-файл
// плюс .env. Больше мегабайта — это не стек, а попытка занять память панели.
const maxStackBody = 1 << 20

// decodeStackReq читает тело с ограничением размера и внятной ошибкой 413.
func decodeStackReq(w http.ResponseWriter, r *http.Request, req *stackDeployReq) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxStackBody)).Decode(req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "файл слишком большой (лимит 1 МБ)")
			return false
		}
		writeErr(w, http.StatusBadRequest, "bad request")
		return false
	}
	return true
}

// ── Превью / валидация ───────────────────────────────────────────────────────

type stackPreviewSvc struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Mode     string `json:"mode"`
	Replicas uint64 `json:"replicas"`
}
type stackPreview struct {
	Name     string              `json:"name"`
	Services []stackPreviewSvc   `json:"services"`
	Networks []string            `json:"networks"`
	Secrets  []string            `json:"secrets"`
	Configs  []string            `json:"configs"`
	Warnings []stackspec.Warning `json:"warnings"`
	// Missing — external-ресурсы, которых нет в кластере (ТЗ 3.14.5).
	Missing []string `json:"missing"`
	// Changes — что произойдёт при деплое: create/update/remove (A-25).
	Changes *stackChanges `json:"changes,omitempty"`
}

// stackChanges — превью изменений относительно текущего состояния кластера.
type stackChanges struct {
	Create []string `json:"create"`
	Update []string `json:"update"`
	Remove []string `json:"remove"`
}

// stackValidate — POST /stacks/validate: dry-run парсинг, ничего не создаёт.
func (h *handlers) stackValidate(w http.ResponseWriter, r *http.Request) {
	var req stackDeployReq
	if !decodeStackReq(w, r, &req) {
		return
	}
	if req.ComposeYAML == "" {
		writeErr(w, http.StatusBadRequest, "compose_yaml required")
		return
	}
	name := req.Name
	if name == "" {
		name = "preview"
	}
	conv, err := buildStack(name, req.ComposeYAML, req.DotEnv, req.EnvVars)
	if err != nil {
		// Ошибка парсинга/интерполяции — это ввод пользователя, 400 с текстом.
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p := buildPreview(name, conv)
	// Проверки против живого кластера: отсутствующие external-ресурсы и diff.
	// Если Docker недоступен — превью всё равно отдаём, просто без этой части.
	if missing, changes, err := h.inspectAgainstCluster(r.Context(), name, conv); err == nil {
		p.Missing, p.Changes = missing, changes
	}
	writeJSON(w, p)
}

func buildPreview(name string, conv *stackspec.Converted) stackPreview {
	p := stackPreview{Name: name, Warnings: conv.Warnings}
	if p.Warnings == nil {
		p.Warnings = []stackspec.Warning{}
	}
	for _, n := range conv.Networks {
		p.Networks = append(p.Networks, n.Name)
	}
	for _, s := range conv.Secrets {
		p.Secrets = append(p.Secrets, s.Name)
	}
	for _, c := range conv.Configs {
		p.Configs = append(p.Configs, c.Name)
	}
	for _, s := range conv.Services {
		sv := stackPreviewSvc{
			Name:  s.Spec.Name,
			Image: s.Spec.TaskTemplate.ContainerSpec.Image,
			Mode:  "replicated",
		}
		if s.Spec.Mode.Global != nil {
			sv.Mode = "global"
		} else if s.Spec.Mode.Replicated != nil && s.Spec.Mode.Replicated.Replicas != nil {
			sv.Replicas = *s.Spec.Mode.Replicated.Replicas
		}
		p.Services = append(p.Services, sv)
	}
	return p
}

// ── Оркестрация деплоя ───────────────────────────────────────────────────────

type stackFail struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// stackResult — сводка деплоя. Сервисы и вспомогательные объекты разведены:
// «создано 3 сервиса» и «создана сеть» — разные сообщения для пользователя.
type stackResult struct {
	Created   []string            `json:"created"`   // сервисы
	Updated   []string            `json:"updated"`   // сервисы
	Removed   []string            `json:"removed"`   // сервисы
	Resources []string            `json:"resources"` // созданные сети/secrets/configs
	Failed    []stackFail         `json:"failed"`
	Warnings  []stackspec.Warning `json:"warnings"`
}

// inspectAgainstCluster сверяет разобранный стек с живым кластером:
// какие external-ресурсы отсутствуют и что изменится при деплое (A-05, A-25).
func (h *handlers) inspectAgainstCluster(ctx context.Context, stack string,
	conv *stackspec.Converted) ([]string, *stackChanges, error) {

	nets, err := h.Docker.Networks(ctx)
	if err != nil {
		return nil, nil, err
	}
	haveNet := map[string]bool{}
	for _, n := range nets {
		haveNet[n.Name] = true
	}

	secrets, err := h.Docker.Secrets(ctx)
	if err != nil {
		return nil, nil, err
	}
	haveSecret := map[string]bool{}
	for _, s := range secrets {
		haveSecret[s.Spec.Name] = true
	}

	configs, err := h.Docker.Configs(ctx)
	if err != nil {
		return nil, nil, err
	}
	haveConfig := map[string]bool{}
	for _, c := range configs {
		haveConfig[c.Spec.Name] = true
	}

	// external-ресурсы панель не создаёт — они обязаны существовать заранее.
	var missing []string
	for _, n := range conv.Networks {
		if n.External && !haveNet[n.Name] {
			missing = append(missing, "network "+n.Name)
		}
	}
	for _, s := range conv.Secrets {
		if s.External && !haveSecret[s.Name] {
			missing = append(missing, "secret "+s.Name)
		}
	}
	for _, c := range conv.Configs {
		if c.External && !haveConfig[c.Name] {
			missing = append(missing, "config "+c.Name)
		}
	}

	services, err := h.Docker.Services(ctx)
	if err != nil {
		return missing, nil, err
	}
	existing := stackServices(services, stack)
	changes := &stackChanges{Create: []string{}, Update: []string{}, Remove: []string{}}
	desired := map[string]bool{}
	for _, svc := range conv.Services {
		desired[svc.Name] = true
		if _, ok := existing[svc.Name]; ok {
			changes.Update = append(changes.Update, svc.Name)
		} else {
			changes.Create = append(changes.Create, svc.Name)
		}
	}
	for name := range existing {
		if !desired[name] {
			changes.Remove = append(changes.Remove, name)
		}
	}
	sort.Strings(changes.Create)
	sort.Strings(changes.Update)
	sort.Strings(changes.Remove)
	return missing, changes, nil
}

// stackServices отбирает сервисы, принадлежащие стеку, по label namespace.
func stackServices(services []swarm.Service, stack string) map[string]swarm.Service {
	out := map[string]swarm.Service{}
	for _, s := range services {
		if s.Spec.Labels[stackspec.StackLabel] == stack {
			out[s.Spec.Name] = s
		}
	}
	return out
}

// applyStack разворачивает стек: сети → secrets/configs → сервисы → prune.
// Ошибка одного объекта не роняет весь деплой — собираем сводку.
// regAuth — base64 X-Registry-Auth для приватных образов ("" — без авторизации).
func (h *handlers) applyStack(ctx context.Context, stack string, conv *stackspec.Converted,
	prune bool, regAuth string) (stackResult, error) {
	var res stackResult

	// 1. Сети: создаём недостающие (existing не трогаем).
	nets, err := h.Docker.Networks(ctx)
	if err != nil {
		return res, err
	}
	haveNet := map[string]bool{}
	for _, n := range nets {
		haveNet[n.Name] = true
	}
	for _, n := range conv.Networks {
		if n.External {
			// external панель не создаёт; отсутствие — ошибка деплоя,
			// иначе сервис упадёт с невнятным сообщением от Docker.
			if !haveNet[n.Name] {
				res.Failed = append(res.Failed, stackFail{"network " + n.Name,
					"external-сеть не найдена в кластере — создайте её заранее"})
			}
			continue
		}
		if haveNet[n.Name] {
			continue
		}
		if _, err := h.Docker.NetworkCreate(ctx, n.Name, dockerapi.NetworkOptions{
			Driver:     n.Driver,
			Attachable: n.Attachable,
			Internal:   n.Internal,
			EnableIPv6: n.EnableIPv6,
			Subnet:     n.Subnet,
			Options:    n.Options,
			Labels:     n.Labels,
		}); err != nil {
			res.Failed = append(res.Failed, stackFail{"network " + n.Name, err.Error()})
		} else {
			res.Resources = append(res.Resources, "network "+n.Name)
		}
	}

	// 2. Secrets и configs: создаём недостающие. Существующие не трогаем —
	// содержимое swarm-объекта неизменяемо, обновление = пересоздание с
	// новым именем, что ломает работающие сервисы.
	secretIDs, err := h.applyFileObjects(ctx, conv.Secrets, &res, "secret")
	if err != nil {
		return res, err
	}
	configIDs, err := h.applyFileObjects(ctx, conv.Configs, &res, "config")
	if err != nil {
		return res, err
	}

	// 3. Сервисы: diff по имени <stack>_<svc> среди сервисов этого стека.
	services, err := h.Docker.Services(ctx)
	if err != nil {
		return res, err
	}
	existing := stackServices(services, stack)
	desired := map[string]bool{}
	for _, svc := range conv.Services {
		desired[svc.Name] = true
		// Ссылки на secrets/configs требуют ID — до этого места он неизвестен.
		if err := resolveRefs(&svc.Spec, secretIDs, configIDs); err != nil {
			res.Failed = append(res.Failed, stackFail{svc.Name, err.Error()})
			continue
		}
		if cur, ok := existing[svc.Name]; ok {
			if _, err := h.Docker.ServiceUpdate(ctx, cur.ID, cur.Version, svc.Spec, regAuth, ""); err != nil {
				res.Failed = append(res.Failed, stackFail{svc.Name, err.Error()})
			} else {
				res.Updated = append(res.Updated, svc.Name)
			}
			continue
		}
		if _, err := h.Docker.ServiceCreate(ctx, svc.Spec, regAuth); err != nil {
			res.Failed = append(res.Failed, stackFail{svc.Name, err.Error()})
		} else {
			res.Created = append(res.Created, svc.Name)
		}
	}

	// 3. Prune: сервисы стека, ушедшие из файла (только по явному флагу).
	if prune {
		for name, s := range existing {
			if desired[name] {
				continue
			}
			if err := h.Docker.ServiceRemove(ctx, s.ID); err != nil {
				res.Failed = append(res.Failed, stackFail{name, err.Error()})
			} else {
				res.Removed = append(res.Removed, name)
			}
		}
	}
	return res, nil
}

// applyFileObjects создаёт недостающие secrets/configs стека и возвращает
// карту «имя → ID» для подстановки в ссылки сервисов.
//
// Swarm-объекты неизменяемы: существующий объект переиспользуется как есть.
// Если содержимое в файле поменялось — нужно новое имя (обычная практика
// версионирования секретов: app_jwt_v2), иначе сервисы продолжат работать
// со старым значением. Это состояние отражается в предупреждениях UI.
func (h *handlers) applyFileObjects(ctx context.Context, objs []stackspec.FileObject,
	res *stackResult, kind string) (map[string]string, error) {

	ids := map[string]string{}
	if len(objs) == 0 {
		return ids, nil
	}

	// Существующие объекты кластера: имя → ID.
	switch kind {
	case "secret":
		list, err := h.Docker.Secrets(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			ids[s.Spec.Name] = s.ID
		}
	default:
		list, err := h.Docker.Configs(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			ids[c.Spec.Name] = c.ID
		}
	}

	for _, o := range objs {
		if _, ok := ids[o.Name]; ok {
			continue // уже есть — переиспользуем
		}
		if o.External {
			res.Failed = append(res.Failed, stackFail{kind + " " + o.Name,
				"external-объект не найден в кластере — создайте его заранее"})
			continue
		}
		var (
			id  string
			err error
		)
		if kind == "secret" {
			id, err = h.Docker.SecretCreate(ctx, o.Name, o.Data, o.Labels)
		} else {
			id, err = h.Docker.ConfigCreate(ctx, o.Name, o.Data, o.Labels)
		}
		if err != nil {
			res.Failed = append(res.Failed, stackFail{kind + " " + o.Name, err.Error()})
			continue
		}
		ids[o.Name] = id
		res.Resources = append(res.Resources, kind+" "+o.Name)
	}
	return ids, nil
}

// resolveRefs проставляет ID секретов/конфигов в ссылках сервиса.
// Docker принимает ссылку только с корректным ID, поэтому отсутствие —
// ошибка до вызова API, а не «сервис создан, но не стартует».
func resolveRefs(spec *swarm.ServiceSpec, secretIDs, configIDs map[string]string) error {
	cs := spec.TaskTemplate.ContainerSpec
	if cs == nil {
		return nil
	}
	for _, ref := range cs.Secrets {
		id, ok := secretIDs[ref.SecretName]
		if !ok {
			return fmt.Errorf("secret %q не найден в кластере", ref.SecretName)
		}
		ref.SecretID = id
	}
	for _, ref := range cs.Configs {
		id, ok := configIDs[ref.ConfigName]
		if !ok {
			return fmt.Errorf("config %q не найден в кластере", ref.ConfigName)
		}
		ref.ConfigID = id
	}
	return nil
}

// ── Create / Update / Source ─────────────────────────────────────────────────

// stackCreate — POST /stacks: сохранить исходник в БД + развернуть.
func (h *handlers) stackCreate(w http.ResponseWriter, r *http.Request) {
	var req stackDeployReq
	if !decodeStackReq(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || strings.ContainsAny(name, " \t/") || req.ComposeYAML == "" {
		writeErr(w, http.StatusBadRequest, "valid name and compose_yaml required")
		return
	}
	h.deployAndStore(w, r, name, req)
}

// stackUpdate — PUT /stacks/{name}: обновить исходник + передеплой (diff).
func (h *handlers) stackUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req stackDeployReq
	if !decodeStackReq(w, r, &req) {
		return
	}
	if req.ComposeYAML == "" {
		writeErr(w, http.StatusBadRequest, "compose_yaml required")
		return
	}
	h.deployAndStore(w, r, name, req)
}

// deployAndStore — общий путь create/update: конвертация → шифрование env →
// сохранение исходника → разворачивание → audit → сводка.
func (h *handlers) deployAndStore(w http.ResponseWriter, r *http.Request, name string, req stackDeployReq) {
	conv, err := buildStack(name, req.ComposeYAML, req.DotEnv, req.EnvVars)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Учётка реестра для приватных образов — проверяем ДО сохранения:
	// несуществующий реестр не должен попасть в исходник стека.
	regAuth := ""
	if req.RegistryID != nil {
		regAuth, err = h.registryAuthHeader(*req.RegistryID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "registry auth failed")
			return
		}
	}

	// Шифруем env целиком (в нём бывают секреты). Если env пуст — не храним.
	var envEnc []byte
	if req.DotEnv != "" || len(req.EnvVars) > 0 {
		if !h.requireCrypto(w) {
			return
		}
		blob, _ := json.Marshal(storedEnv{DotEnv: req.DotEnv, Vars: req.EnvVars})
		envEnc, err = h.Crypto.Seal(blob)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "encryption failed")
			return
		}
	}

	s := sessionFrom(r)
	// Сохраняем исходник ДО деплоя — источник правды переживёт частичный сбой.
	if err := h.Store.SaveStack(name, req.ComposeYAML, envEnc, req.RegistryID, s.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}

	res, err := h.applyStack(r.Context(), name, conv, req.Prune, regAuth)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error: "+err.Error())
		return
	}
	// Предупреждения конвертера едут вместе со сводкой — пользователь должен
	// видеть, что часть файла не перенесена (3.14.10).
	res.Warnings = conv.Warnings
	if res.Warnings == nil {
		res.Warnings = []stackspec.Warning{}
	}
	// В audit — только имя стека и сводка чисел; yaml/env НЕ логируем (3.14.7).
	_ = h.Store.AppendAudit(s.UserID, "stack.deploy_file", "stack", name, "")
	writeJSON(w, res)
}

// stackSource — GET /stacks/{name}/source: исходный yaml + env для редактирования.
func (h *handlers) stackSource(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	st, err := h.Store.StackByName(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, "stack source not found")
		return
	}
	out := map[string]any{
		"name":         st.Name,
		"compose_yaml": st.ComposeYAML,
		"env":          "",
		"env_vars":     map[string]string{},
		"registry_id":  st.RegistryID,
		"updated_at":   st.UpdatedAt,
	}
	if len(st.EnvEnc) > 0 {
		if !h.requireCrypto(w) {
			return
		}
		blob, err := h.Crypto.Open(st.EnvEnc)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "decryption failed")
			return
		}
		var se storedEnv
		if json.Unmarshal(blob, &se) == nil {
			out["env"] = se.DotEnv
			if se.Vars != nil {
				out["env_vars"] = se.Vars
			}
		}
	}
	writeJSON(w, out)
}
