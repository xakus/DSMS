// Типы данных API (соответствуют ответам panel, разд. 4 ТЗ).

/** Снапшот метрик ноды (формат ingest, разд. 4.3). */
export interface Snapshot {
  node_id: string
  hostname: string
  ts: number
  cpu: { total_pct: number; per_core?: number[]; load?: number[] }
  mem: { total: number; used: number; available: number; cached: number }
  disk?: DiskMetric[]
  net?: NetMetric[]
  sys?: { uptime: number; containers: number }
  containers?: ContainerMetric[]
}

/** Метрики одной точки монтирования. */
export interface DiskMetric {
  mount: string
  total: number
  used: number
  read_bps: number
  write_bps: number
  read_iops: number
  write_iops: number
}

/** Метрики одного сетевого интерфейса. */
export interface NetMetric {
  iface: string
  rx_bps: number
  tx_bps: number
  rx_pps: number
  tx_pps: number
  errors: number
  drops: number
}

/** Per-container метрики (расширение FR-02). */
export interface ContainerMetric {
  id: string
  name: string
  service: string
  cpu_pct: number
  mem_used: number
  mem_limit: number
}

/** Нода в списке /nodes. */
export interface NodeInfo {
  id: string
  hostname: string
  role: 'manager' | 'worker'
  leader?: boolean
  state: string
  availability: string
  addr: string
  labels: Record<string, string>
  metrics?: Snapshot
}

/** Задача на ноде (из /nodes/{id}). */
export interface NodeTask {
  id: string
  service_id: string
  service: string
  slot: number
  state: string
  message: string
  created_at: string
}

/** Детали ноды (/nodes/{id}). */
export interface NodeDetail extends NodeInfo {
  engine: string
  os: string
  arch: string
  tasks: NodeTask[]
}

/** Ответ /swarm/join-tokens. */
export interface JoinTokens {
  worker: string
  manager: string
  manager_addr: string
}

/** Сводка кластера (/cluster). */
export interface ClusterSummary {
  nodes: number
  nodes_ready: number
  services: number
}

/** Сервис в таблице /services (3.4.1). */
export interface ServiceInfo {
  id: string
  name: string
  image: string
  mode: 'replicated' | 'global'
  running: number
  desired: number
  stack: string
  ports?: string[]
  labels: Record<string, string>
  update_state?: string
  stopped: boolean
}

/** Задача сервиса с историей (3.4.4). */
export interface ServiceTask {
  id: string
  slot: number
  node: string
  state: string
  desired: string
  message: string
  exit_code?: number
  created_at: string
}

/** Детали сервиса (/services/{id}). */
export interface ServiceDetail {
  id: string
  name: string
  stack: string
  mode: string
  spec: {
    image: string
    env?: string[]
    mounts?: string[]
    constraints?: string[]
    labels?: Record<string, string>
    limits?: { cpus?: number; memory?: number }
  }
  tasks: ServiceTask[]
  update?: { state: string; message: string } | null
}

/** Стек в списке /stacks (3.8.1). */
export interface StackInfo {
  name: string
  services: number
  running: number
  desired: number
}

/** Реестр (FR-13); пароль никогда не приходит с сервера. */
export interface RegistryInfo {
  id: number
  address: string
  username: string
  label: string
}
