// Запасной источник CPU/RAM контейнера — Docker API (как `docker stats`).
//
// Основной путь — чтение cgroup хоста: быстро и без нагрузки на daemon. Но
// раскладка cgroup бывает нестандартной (свой cgroup-parent, вложенная
// виртуализация, rootless), а /sys хоста может быть не примонтирован. Раньше
// такой контейнер молча пропускался, и в UI у сервисов ноды стояли прочерки.
// Теперь те же счётчики запрашиваются у Docker: он читает cgroup сам.
package dockerops

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/docker/docker/api/types/container"
)

// sample — показания контейнера за один тик.
type sample struct {
	// cpuUsec — CPU-время с момента старта контейнера, мкс; cpuOK — прочитано.
	cpuUsec uint64
	cpuOK   bool
	// memUsed — байты без неактивного файлового кэша; memLimit — байты, 0 — нет.
	memUsed, memLimit uint64
}

// readSample читает показания контейнера: из cgroup хоста, а если cgroup не
// найден — у Docker. false — контейнера уже нет.
func (o *Ops) readSample(ctx context.Context, base, id string) (sample, bool) {
	if dir := findCgroup(base, id); dir != "" {
		var s sample
		s.memUsed, s.memLimit = readMem(dir)
		s.cpuUsec, s.cpuOK = readCPUUsec(base, dir, id)
		return s, true
	}
	return o.apiSample(ctx, id)
}

// apiSample запрашивает показания контейнера у Docker (один запрос без
// ожидания второго замера: дельту CPU считаем сами между тиками).
func (o *Ops) apiSample(ctx context.Context, id string) (sample, bool) {
	if !o.warnedNoCgroup {
		// Один раз за запуск: путь рабочий, но медленнее — админу стоит знать.
		o.warnedNoCgroup = true
		slog.Warn("container cgroup not visible, falling back to Docker stats API",
			"cgroup_base", cgroupBase(), "container", id[:min(len(id), 12)])
	}
	resp, err := o.c.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return sample{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	var st container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return sample{}, false
	}
	// Лимит — из настроек контейнера: в ответе stats без лимита стоит вся
	// память ноды, и «из 8 GiB» выглядело бы как настоящий лимит.
	o.containerPID(ctx, id) // наполняет кэш лимитов (один inspect на контейнер)
	return statsSample(st, o.memLimits[id]), true
}

// statsSample переводит ответ Docker stats в показания контейнера.
// Память считается так же, как в `docker stats`: usage минус неактивный
// файловый кэш (ключ зависит от версии cgroup).
// Пример: usage 900 МБ, inactive_file 600 МБ → 300 МБ.
func statsSample(st container.StatsResponse, limit uint64) sample {
	mem := st.MemoryStats
	inactive, v1 := mem.Stats["total_inactive_file"] // cgroup v1
	if !v1 {
		inactive = mem.Stats["inactive_file"] // cgroup v2
	}
	return sample{
		cpuUsec:  st.CPUStats.CPUUsage.TotalUsage / nsecPerUsec,
		cpuOK:    true,
		memUsed:  subCache(mem.Usage, inactive),
		memLimit: limit,
	}
}
