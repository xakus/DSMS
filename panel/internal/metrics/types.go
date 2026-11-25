// Package metrics — типы и live-хранилище метрик нод.
//
// Формат payload соответствует разделу 4.3 ТЗ (agent → panel).
// ВАЖНО: agent — отдельный проект и держит собственную копию этих типов
// (правило проекта: никакого общего кода между сервисами).
package metrics

// CPU — загрузка процессора ноды.
type CPU struct {
	TotalPct float64   `json:"total_pct"` // суммарная загрузка, %
	PerCore  []float64 `json:"per_core"`  // по ядрам, %
	Load     []float64 `json:"load"`      // load average 1/5/15
}

// Mem — использование памяти, байты.
type Mem struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
	Cached    uint64 `json:"cached"`
}

// Disk — одна точка монтирования: место + скорость I/O.
type Disk struct {
	Mount     string `json:"mount"`
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	ReadBps   uint64 `json:"read_bps"`
	WriteBps  uint64 `json:"write_bps"`
	ReadIOPS  uint64 `json:"read_iops"`
	WriteIOPS uint64 `json:"write_iops"`
}

// Net — один сетевой интерфейс: скорость и ошибки.
type Net struct {
	Iface  string `json:"iface"`
	RxBps  uint64 `json:"rx_bps"`
	TxBps  uint64 `json:"tx_bps"`
	RxPps  uint64 `json:"rx_pps"`
	TxPps  uint64 `json:"tx_pps"`
	Errors uint64 `json:"errors"`
	Drops  uint64 `json:"drops"`
}

// Sys — системная информация ноды.
type Sys struct {
	Uptime     uint64 `json:"uptime"`     // секунды
	Containers int    `json:"containers"` // кол-во контейнеров на ноде
}

// Container — per-container метрики (расширение FR-02, топ-N по нагрузке).
type Container struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Service  string  `json:"service"` // имя swarm-сервиса, если есть
	CPUPct   float64 `json:"cpu_pct"`
	MemUsed  uint64  `json:"mem_used"`
	MemLimit uint64  `json:"mem_limit"`
}

// Task — потребление одной задачи Swarm (FR-04 3.4.7, поле `tasks` батча).
// В отличие от Container приходит по ВСЕМ задачам ноды, без отсечки топ-N.
type Task struct {
	Task string `json:"task"` // ID задачи Swarm
	// CPUCores — занято ядер: 0.35 = 35% одного ядра (как в `docker stats`).
	// Не нормировано на ядра ноды — значения с разных нод складываются.
	CPUCores float64 `json:"cpu_cores"`
	MemUsed  uint64  `json:"mem_used"`  // байты, без неактивного файлового кэша
	MemLimit uint64  `json:"mem_limit"` // байты; 0 — без лимита
	RxBps    uint64  `json:"rx_bps"`    // приём, байт/с
	TxBps    uint64  `json:"tx_bps"`    // отдача, байт/с
}

// Snapshot — один батч метрик от агента (POST /api/v1/ingest, разд. 4.3).
type Snapshot struct {
	NodeID     string      `json:"node_id"`
	Hostname   string      `json:"hostname"`
	TS         int64       `json:"ts"` // unix seconds
	CPU        CPU         `json:"cpu"`
	Mem        Mem         `json:"mem"`
	Disk       []Disk      `json:"disk"`
	Net        []Net       `json:"net"`
	Sys        Sys         `json:"sys"`
	Containers []Container `json:"containers,omitempty"`
	// Tasks — потребление задач Swarm. Живёт только в TaskStats: ingest
	// вырезает поле до записи в буфер, БД и рассылки (NFR-2a).
	Tasks []Task `json:"tasks,omitempty"`
}
