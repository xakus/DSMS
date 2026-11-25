// Скорость сети контейнера (ТЗ 3.4.7).
//
// В cgroups сетевых счётчиков нет. Берём их из procfs хоста: файл
// /proc/<pid>/net/dev показывает интерфейсы сетевого пространства (netns)
// процесса <pid>, а не того, кто читает. PID главного процесса контейнера
// отдаёт Docker (один ContainerInspect на контейнер, дальше — из кэша).
// Так же собирает сеть cAdvisor.
package dockerops

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// netSample — предыдущее показание сетевых счётчиков контейнера.
type netSample struct {
	rx, tx uint64    // байты с момента создания интерфейсов
	at     time.Time // когда сняты
}

// Колонки /proc/<pid>/net/dev после имени интерфейса: 8 чисел приёма,
// затем 8 чисел отдачи. Байты — первая колонка в каждой группе.
const (
	netDevRxBytesCol = 0 // receive bytes
	netDevTxBytesCol = 8 // transmit bytes
	netDevMinCols    = 9 // меньше — строка обрезана, пропускаем
)

// procBase возвращает корень procfs хоста.
func procBase() string {
	if hp := os.Getenv("HOST_PROC"); hp != "" {
		return hp
	}
	return "/proc"
}

// parseNetDev суммирует принятые и отправленные байты по всем интерфейсам
// из содержимого /proc/<pid>/net/dev, кроме loopback (трафик внутри
// контейнера нагрузкой на сеть не является).
//
// Формат строки: «  eth0: 1234 10 0 0 0 0 0 0 5678 12 0 0 0 0 0 0».
// Первые две строки файла — шапка без двоеточия в нужном месте, они
// отсеиваются проверкой числа колонок.
func parseNetDev(data string) (rx, tx uint64) {
	for line := range strings.SplitSeq(data, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		cols := strings.Fields(rest)
		if len(cols) < netDevMinCols {
			continue
		}
		r, errR := strconv.ParseUint(cols[netDevRxBytesCol], 10, 64)
		t, errT := strconv.ParseUint(cols[netDevTxBytesCol], 10, 64)
		if errR != nil || errT != nil {
			continue // шапка («bytes packets …») или мусор
		}
		rx += r
		tx += t
	}
	return rx, tx
}

// containerPID возвращает PID главного процесса контейнера в PID-пространстве
// хоста. Успешный ответ кэшируется: пока контейнер жив, PID не меняется.
// Ошибку не кэшируем — контейнер мог ещё не успеть стартовать.
func (o *Ops) containerPID(ctx context.Context, id string) (int, bool) {
	if pid, ok := o.pids[id]; ok {
		return pid, true
	}
	info, err := o.c.ContainerInspect(ctx, id)
	if err != nil || info.ContainerJSONBase == nil || info.State == nil || info.State.Pid <= 0 {
		return 0, false
	}
	o.pids[id] = info.State.Pid
	// Заодно запоминаем лимит памяти из настроек контейнера: он нужен, когда
	// cgroup не виден и цифры берутся у Docker (см. stats.go).
	if info.HostConfig != nil {
		// Memory — int64: 0 — лимита нет, отрицательных значений Docker не отдаёт.
		if limit := info.HostConfig.Memory; limit > 0 {
			o.memLimits[id] = uint64(limit)
		}
	}
	return info.State.Pid, true
}

// netRate возвращает скорость приёма и отдачи контейнера в байт/с —
// дельту счётчиков к прошлому тику. Первый тик и любые сбои чтения дают 0:
// скорость без предыдущей точки посчитать не из чего.
func (o *Ops) netRate(ctx context.Context, id string, now time.Time) (rxBps, txBps uint64) {
	pid, ok := o.containerPID(ctx, id)
	if !ok {
		return 0, 0
	}
	data, err := os.ReadFile(filepath.Join(procBase(), strconv.Itoa(pid), "net", "dev"))
	if err != nil {
		// Процесс умер (контейнер перезапущен) — PID устарел, спросим заново.
		delete(o.pids, id)
		delete(o.prevNet, id)
		return 0, 0
	}
	rx, tx := parseNetDev(string(data))
	prev, had := o.prevNet[id]
	o.prevNet[id] = netSample{rx: rx, tx: tx, at: now}
	if !had {
		return 0, 0
	}
	return ratePerSec(rx, prev.rx, now.Sub(prev.at)), ratePerSec(tx, prev.tx, now.Sub(prev.at))
}

// ratePerSec переводит дельту счётчика в «единиц в секунду».
// Счётчик пошёл назад (интерфейс пересоздан) или время не сдвинулось → 0.
// Пример: было 1 000 000 байт, стало 4 000 000 за 3 с → 1 000 000 байт/с.
func ratePerSec(cur, prev uint64, dt time.Duration) uint64 {
	sec := dt.Seconds()
	if cur < prev || sec <= 0 {
		return 0
	}
	return uint64(float64(cur-prev) / sec)
}

// forgetNet чистит кэш PID, лимитов и счётчики контейнеров, которых больше нет
// (alive — ID запущенных сейчас). Без этого карты росли бы с каждым
// перезапуском задач.
func (o *Ops) forgetNet(alive map[string]bool) {
	for id := range o.pids {
		if !alive[id] {
			delete(o.pids, id)
		}
	}
	for id := range o.prevNet {
		if !alive[id] {
			delete(o.prevNet, id)
		}
	}
	for id := range o.memLimits {
		if !alive[id] {
			delete(o.memLimits, id)
		}
	}
}
