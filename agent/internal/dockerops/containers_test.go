// Тесты разбора счётчиков контейнера (ТЗ 3.4.7): память без файлового кэша,
// сетевые счётчики из /proc/<pid>/net/dev, перевод дельты в скорость.
//
// Ошибка здесь не роняет агента, а тихо показывает неверную цифру, по
// которой решают, какой сервис «съел» ноду.
package dockerops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// write кладёт файл cgroup/procfs во временный каталог теста.
func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestReadMemV2SubtractsInactiveFile: used = memory.current − inactive_file,
// как в `docker stats`. 900 МБ − 600 МБ кэша = 300 МБ.
func TestReadMemV2SubtractsInactiveFile(t *testing.T) {
	const mb = 1 << 20
	dir := t.TempDir()
	write(t, dir, "memory.current", "943718400\n")                              // 900 МБ
	write(t, dir, "memory.max", "1073741824\n")                                 // 1 ГБ
	write(t, dir, "memory.stat", "anon 100\ninactive_file 629145600\nfile 1\n") // 600 МБ

	used, limit := readMem(dir)
	if used != 300*mb {
		t.Fatalf("used: want %d, got %d", 300*mb, used)
	}
	if limit != 1024*mb {
		t.Fatalf("limit: want %d, got %d", 1024*mb, limit)
	}
}

// TestReadMemV2NoLimit: memory.max = "max" означает «без лимита» → 0.
func TestReadMemV2NoLimit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "memory.current", "1000\n")
	write(t, dir, "memory.max", "max\n")

	used, limit := readMem(dir)
	if used != 1000 || limit != 0 {
		t.Fatalf("want used=1000 limit=0, got used=%d limit=%d", used, limit)
	}
}

// TestReadMemV1: в cgroup v1 другие имена файлов и ключ total_inactive_file,
// а «без лимита» — гигантское число.
func TestReadMemV1(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "memory.usage_in_bytes", "5000\n")
	write(t, dir, "memory.limit_in_bytes", "9223372036854771712\n")
	write(t, dir, "memory.stat", "inactive_file 1\ntotal_inactive_file 2000\n")

	used, limit := readMem(dir)
	if used != 3000 {
		t.Fatalf("used: want 3000, got %d", used)
	}
	if limit != 0 {
		t.Fatalf("v1 unlimited must be 0, got %d", limit)
	}
}

// TestSubCacheNeverNegative: счётчики сняты в разные моменты, кэш может
// оказаться больше usage — uint64 не должен «перевернуться» в экзабайты.
func TestSubCacheNeverNegative(t *testing.T) {
	if got := subCache(100, 500); got != 100 {
		t.Fatalf("want 100, got %d", got)
	}
	if got := subCache(500, 100); got != 400 {
		t.Fatalf("want 400, got %d", got)
	}
}

// netDevSample — настоящий вывод /proc/<pid>/net/dev контейнера в overlay-сети.
const netDevSample = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:    9999      10    0    0    0     0          0         0     9999      10    0    0    0     0       0          0
  eth0: 1000000     700    0    0    0     0          0         0   250000     300    0    0    0     0       0          0
  eth1:    5000      20    0    0    0     0          0         0     1000      10    0    0    0     0       0          0
`

// TestParseNetDev: суммируются все интерфейсы, кроме lo; шапка не мешает.
func TestParseNetDev(t *testing.T) {
	rx, tx := parseNetDev(netDevSample)
	if rx != 1005000 {
		t.Fatalf("rx: want 1005000, got %d", rx)
	}
	if tx != 251000 {
		t.Fatalf("tx: want 251000, got %d", tx)
	}
}

// TestParseNetDevGarbage: обрезанный или пустой файл не должен паниковать.
func TestParseNetDevGarbage(t *testing.T) {
	for _, in := range []string{"", "eth0:", "eth0: 1 2 3", "no colon here", "eth0: a b c d e f g h i"} {
		if rx, tx := parseNetDev(in); rx != 0 || tx != 0 {
			t.Fatalf("%q: want 0/0, got %d/%d", in, rx, tx)
		}
	}
}

// TestRatePerSec: дельта за интервал → байт/с; сброс счётчика → 0.
func TestRatePerSec(t *testing.T) {
	// 3 000 000 байт за 3 с = 1 000 000 байт/с.
	if got := ratePerSec(4_000_000, 1_000_000, 3*time.Second); got != 1_000_000 {
		t.Fatalf("want 1000000, got %d", got)
	}
	// Интерфейс пересоздан — счётчик меньше прошлого.
	if got := ratePerSec(10, 1_000_000, 3*time.Second); got != 0 {
		t.Fatalf("counter reset: want 0, got %d", got)
	}
	// Нулевой интервал — деления на ноль быть не должно.
	if got := ratePerSec(10, 5, 0); got != 0 {
		t.Fatalf("zero dt: want 0, got %d", got)
	}
}

// TestForgetNet: счётчики и PID умерших контейнеров удаляются.
func TestForgetNet(t *testing.T) {
	o := &Ops{
		pids:    map[string]int{"alive": 10, "dead": 11},
		prevNet: map[string]netSample{"alive": {}, "dead": {}},
	}
	o.forgetNet(map[string]bool{"alive": true})
	if _, ok := o.pids["dead"]; ok {
		t.Fatal("dead pid must be forgotten")
	}
	if _, ok := o.prevNet["dead"]; ok {
		t.Fatal("dead net sample must be forgotten")
	}
	if len(o.pids) != 1 || len(o.prevNet) != 1 {
		t.Fatalf("alive entries must stay: pids=%d prevNet=%d", len(o.pids), len(o.prevNet))
	}
}
