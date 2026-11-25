// Тесты выбора сетевых интерфейсов ноды.
//
// Цена ошибки: график «Сеть» ноды показывает трафик самого агента (десятки
// байт в секунду) или, наоборот, сумму по всем мостам и veth — в разы больше
// настоящего. По этому графику ищут, какая нода забита.
package collector

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// fakeSys собирает подобие /sys/class/net: имя → цель ссылки.
func fakeSys(t *testing.T, links map[string]string) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "class", "net")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

// keys — отсортированные имена выбранных интерфейсов.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// same сравнивает два списка имён.
func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestUplinkIfacesPhysical: на сервере с настоящими картами берём только их —
// мосты, veth и VPN не считаем (их трафик уже посчитан на карте).
func TestUplinkIfacesPhysical(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"lo":              "../../devices/virtual/net/lo",
		"enp0s31f6":       "../../devices/pci0000:00/0000:00:1f.6/net/enp0s31f6",
		"wlp4s0":          "../../devices/pci0000:00/0000:00:1c.7/0000:04:00.0/net/wlp4s0",
		"docker0":         "../../devices/virtual/net/docker0",
		"docker_gwbridge": "../../devices/virtual/net/docker_gwbridge",
		"veth99fb7c3":     "../../devices/virtual/net/veth99fb7c3",
		"wg0":             "../../devices/virtual/net/wg0",
	})
	got := keys(uplinkIfaces(
		[]string{"lo", "enp0s31f6", "wlp4s0", "docker0", "docker_gwbridge", "veth99fb7c3", "wg0"}, sys))
	want := []string{"enp0s31f6", "wlp4s0"}
	if !same(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// TestUplinkIfacesContainerNode: нода сама в контейнере (LXC, dind) — её
// eth0 виртуальный. Тогда берём всё, кроме заведомо докеровских интерфейсов.
func TestUplinkIfacesContainerNode(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"lo":              "../../devices/virtual/net/lo",
		"eth0":            "../../devices/virtual/net/eth0",
		"docker0":         "../../devices/virtual/net/docker0",
		"docker_gwbridge": "../../devices/virtual/net/docker_gwbridge",
		"veth84d8483":     "../../devices/virtual/net/veth84d8483",
		"br-b1ea068f86dd": "../../devices/virtual/net/br-b1ea068f86dd",
	})
	got := keys(uplinkIfaces(
		[]string{"lo", "eth0", "docker0", "docker_gwbridge", "veth84d8483", "br-b1ea068f86dd"}, sys))
	if want := []string{"eth0"}; !same(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// TestUplinkIfacesNoSysfs: sysfs недоступен — работаем по именам и не теряем
// сетевую карту с непривычным именем.
func TestUplinkIfacesNoSysfs(t *testing.T) {
	got := keys(uplinkIfaces([]string{"lo", "ens3", "veth1", "docker0"}, t.TempDir()))
	if want := []string{"ens3"}; !same(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// TestHostNetDevPath: в контейнере счётчики берутся из сетевого пространства
// первого процесса ноды, а не из self (это был бы сам агент).
func TestHostNetDevPath(t *testing.T) {
	t.Setenv("HOST_PROC", "/host/proc")
	if got := hostNetDevPath(); got != "/host/proc/1/net/dev" {
		t.Fatalf("in container: got %q", got)
	}
	t.Setenv("HOST_PROC", "")
	if got := hostNetDevPath(); got != "/proc/net/dev" {
		t.Fatalf("on host: got %q", got)
	}
}
