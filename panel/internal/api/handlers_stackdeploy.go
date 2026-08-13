// Деплой стека из compose-файла (FR-14): валидация, разворачивание
// (сети → сервисы → prune), сохранение исходника в БД, отдача для редактирования.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
	"github.com/xakus/DSMS/panel/internal/stackspec"
)

// stackDeployReq — тело для validate/create/update.
type stackDeployReq struct {
	Name        string            `json:"name"`
	ComposeYAML string            `json:"compose_yaml"`
	DotEnv      string            `json:"env"`      // содержимое .env-файла
	EnvVars     map[string]string `json:"env_vars"` // ручные пары (перекрывают .env)
	Prune       bool              `json:"prune"`    // удалять сервисы, ушедшие из файла
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
	return stackspec.Convert(name, proj)
}

// ── Превью / валидация ───────────────────────────────────────────────────────

type stackPreviewSvc struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Mode     string `json:"mode"`
	Replicas uint64 `json:"replicas"`
}
type stackPreview struct {
	Name     string            `json:"name"`
	Services []stackPreviewSvc `json:"services"`
	Networks []string          `json:"networks"`
}

// stackValidate — POST /stacks/validate: dry-run парсинг, ничего не создаёт.
func (h *handlers) stackValidate(w http.ResponseWriter, r *http.Request) {
	var req stackDeployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ComposeYAML == "" {
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
	writeJSON(w, buildPreview(name, conv))
}

func buildPreview(name string, conv *stackspec.Converted) stackPreview {
	p := stackPreview{Name: name}
	for _, n := range conv.Networks {
		p.Networks = append(p.Networks, n.Name)
	}
	for _, s := range conv.Services {
		sv := stackPreviewSvc{
			Name:  s.Spec.Annotations.Name,
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
type stackResult struct {
	Created []string    `json:"created"`
	Updated []string    `json:"updated"`
	Removed []string    `json:"removed"`
	Failed  []stackFail `json:"failed"`
}

// applyStack разворачивает стек: сети → создать/обновить сервисы → prune.
// Ошибка одного объекта не роняет весь деплой — собираем сводку.
func (h *handlers) applyStack(ctx context.Context, stack string, conv *stackspec.Converted, prune bool) (stackResult, error) {
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
		if haveNet[n.Name] {
			continue
		}
		if _, err := h.Docker.NetworkCreate(ctx, n.Name, n.Driver, n.Attachable, ""); err != nil {
			res.Failed = append(res.Failed, stackFail{"network " + n.Name, err.Error()})
		}
	}

	// 2. Сервисы: diff по имени <stack>_<svc> среди сервисов этого стека.
	services, err := h.Docker.Services(ctx)
	if err != nil {
		return res, err
	}
	existing := map[string]swarm.Service{}
	for _, s := range services {
		if s.Spec.Annotations.Labels[stackspec.StackLabel] == stack {
			existing[s.Spec.Annotations.Name] = s
		}
	}
	desired := map[string]bool{}
	for _, svc := range conv.Services {
		desired[svc.Name] = true
		if cur, ok := existing[svc.Name]; ok {
			if _, err := h.Docker.ServiceUpdate(ctx, cur.ID, cur.Version, svc.Spec, "", ""); err != nil {
				res.Failed = append(res.Failed, stackFail{svc.Name, err.Error()})
			} else {
				res.Updated = append(res.Updated, svc.Name)
			}
			continue
		}
		if _, err := h.Docker.ServiceCreate(ctx, svc.Spec, ""); err != nil {
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

// ── Create / Update / Source ─────────────────────────────────────────────────

// stackCreate — POST /stacks: сохранить исходник в БД + развернуть.
func (h *handlers) stackCreate(w http.ResponseWriter, r *http.Request) {
	var req stackDeployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ComposeYAML == "" {
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
	if err := h.Store.SaveStack(name, req.ComposeYAML, envEnc, s.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}

	res, err := h.applyStack(r.Context(), name, conv, req.Prune)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error: "+err.Error())
		return
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
