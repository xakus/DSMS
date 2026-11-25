// Какие сетевые интерфейсы считать «сетью ноды».
//
// Две ловушки, из-за которых график «Сеть» ноды показывал не то:
//
//  1. Агент живёт в контейнере со своим сетевым пространством. Файл
//     $HOST_PROC/net/dev — это ссылка на self/net/dev, то есть счётчики
//     того, кто читает: агент видел интерфейсы СВОЕГО контейнера, а не ноды.
//     Счётчики ноды лежат в сетевом пространстве её первого процесса:
//     $HOST_PROC/1/net/dev.
//  2. На ноде с Docker десятки виртуальных интерфейсов (veth на каждый
//     контейнер, мосты docker0/docker_gwbridge/br-…). Один и тот же пакет
//     проходит через veth, мост и сетевую карту — сумма «по всем» завышает
//     трафик в разы. Считаем только аплинки: настоящие сетевые карты.
package collector

import (
	"os"
	"path/filepath"
	"strings"
)

// hostInitPID — PID первого процесса ноды: он живёт в её сетевом пространстве.
const hostInitPID = "1"

// virtualIfacePrefixes — имена виртуальных интерфейсов Docker/Linux, которые
// не являются аплинком ноды. Нужны там, где sysfs не помогает: если сама нода —
// контейнер (LXC, docker-in-docker), её «сетевая карта» тоже виртуальная.
var virtualIfacePrefixes = []string{
	"veth",   // пара «контейнер ↔ хост», по одной на контейнер
	"docker", // docker0, docker_gwbridge
	"br-",    // пользовательские bridge-сети Docker
	"vxlan",  // туннели overlay-сетей
	"vx-",    // то же, старое именование
	"virbr",  // мосты libvirt
	"cni",    // сети Kubernetes/CNI
	"flannel",
	"cali",
	"kube-",
	"dummy",
	"ifb",
	"tunl", // служебные туннели ядра: tunl0, ip6tnl0, sit0
	"ip6tnl",
	"sit",
}

// hostNetDevPath возвращает файл со счётчиками интерфейсов НОДЫ.
// HOST_PROC не задан — агент запущен прямо на хосте, годится обычный /proc.
func hostNetDevPath() string {
	if hp := os.Getenv("HOST_PROC"); hp != "" {
		return filepath.Join(hp, hostInitPID, "net", "dev")
	}
	return "/proc/net/dev"
}

// hostSysBase возвращает корень sysfs ноды.
func hostSysBase() string {
	if hs := os.Getenv("HOST_SYS"); hs != "" {
		return hs
	}
	return "/sys"
}

// isPhysicalIface — у интерфейса есть настоящее устройство. В sysfs
// class/net/<имя> — ссылка: у виртуальных она ведёт в devices/virtual/net/,
// у настоящих — в devices/pci…/. Ссылку не прочитать — считаем, что не знаем.
func isPhysicalIface(sysBase, name string) bool {
	target, err := os.Readlink(filepath.Join(sysBase, "class", "net", name))
	if err != nil {
		return false
	}
	return !strings.Contains(target, "/virtual/")
}

// hasVirtualName — имя интерфейса из списка заведомо виртуальных.
func hasVirtualName(name string) bool {
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// uplinkIfaces выбирает из имён интерфейсов ноды аплинки — то, что считать
// её сетевым трафиком.
//
// Правило:
//   - loopback не берём никогда;
//   - есть интерфейсы с настоящим устройством — берём только их (точно и
//     без двойного счёта: трафик VPN, VLAN и мостов идёт через них же);
//   - нет ни одного (нода сама в контейнере) — берём всё, кроме заведомо
//     виртуальных по имени.
//
// Пример: [lo eth0 docker0 veth1a2b] на сервере с сетевой картой eth0 → [eth0].
func uplinkIfaces(names []string, sysBase string) map[string]bool {
	physical := map[string]bool{}
	fallback := map[string]bool{}
	for _, name := range names {
		if name == "lo" {
			continue
		}
		if isPhysicalIface(sysBase, name) {
			physical[name] = true
		}
		if !hasVirtualName(name) {
			fallback[name] = true
		}
	}
	if len(physical) > 0 {
		return physical
	}
	return fallback
}
