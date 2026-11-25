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

/**
 * Потребление ресурсов задачи, сервиса или стека (ТЗ 3.4.7).
 * Поля нет в ответе (undefined/null) — метрик нет: в UI прочерк, а не ноль.
 */
export interface Usage {
  /** Занято ядер: 0.35 = 35% одного ядра (как в `docker stats`). */
  cpu_cores: number
  /** Память, байты (без неактивного файлового кэша). */
  mem_used: number
  /** Сумма лимитов памяти, байты; 0 — лимита нет (или есть не у всех задач). */
  mem_limit: number
  /** Скорость приёма, байт/с. */
  rx_bps: number
  /** Скорость отдачи, байт/с. */
  tx_bps: number
  /** Сколько running-задач дали метрики; меньше running — сумма неполная. */
  tasks: number
}

/**
 * Почему у работающих задач на ноде нет метрик (ТЗ 3.4.7):
 * silent — агент ноды не присылает батчи; outdated — агент старой версии;
 * failed — агент не смог опросить Docker; unseen — агент работает, но эту
 * задачу не прислал (она только что запущена).
 */
export type NoDataReason = 'silent' | 'outdated' | 'failed' | 'unseen'

/** Нода, с которой нет метрик задач сервиса, и причина. */
export interface NoData {
  /** Hostname ноды (ID, если имя неизвестно). */
  node: string
  reason: NoDataReason
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
  /** Hostname нод, где у сервиса есть running-задачи. */
  nodes: string[]
  usage?: Usage | null
  /** С каких нод нет метрик и почему; поля нет — метрики есть по всем задачам. */
  no_data?: NoData[]
}

/** Задача сервиса с историей (3.4.4). */
export interface ServiceTask {
  id: string
  slot: number
  /** Hostname ноды. */
  node: string
  /** ID ноды — для ссылки на её страницу. */
  node_id: string
  state: string
  desired: string
  message: string
  /** Образ задачи: при обновлении видно, кто на какой версии. */
  image: string
  exit_code?: number
  created_at: string
  /** Потребление running-задачи (3.4.7). */
  usage?: Usage | null
  /** Почему у running-задачи нет метрик; поля нет — метрики есть. */
  no_data?: NoDataReason
}

/** Лимиты или резервы сервиса. */
export interface ServiceResources {
  /** Ядра; 0 — не задано. */
  cpus: number
  /** Байты; 0 — не задано. */
  memory: number
}

/** Спецификация сервиса read-only (3.4.4). */
export interface ServiceSpec {
  image: string
  /** sha256, в который разрешился тег. */
  digest?: string
  command: string[]
  args: string[]
  env: string[]
  mounts: string[]
  networks: string[]
  ports: string[]
  constraints: string[]
  secrets: string[]
  configs: string[]
  labels?: Record<string, string>
  limits?: ServiceResources
  reservations?: ServiceResources
  restart?: { condition: string; delay_sec: number; max_attempts: number }
  update_config?: { parallelism: number; delay_sec: number; order: string; failure_action: string }
  healthcheck: string[]
  user?: string
  hostname?: string
}

/** Детали сервиса (/services/{id}). */
export interface ServiceDetail {
  id: string
  name: string
  stack: string
  mode: 'replicated' | 'global'
  running: number
  desired: number
  /** Остановлен панелью (scale 0 с запомненными репликами). */
  stopped: boolean
  nodes: string[]
  usage?: Usage | null
  /** С каких нод нет метрик и почему. */
  no_data?: NoData[] | null
  created_at: string
  updated_at: string
  spec: ServiceSpec
  tasks: ServiceTask[]
  update?: { state: string; message: string } | null
}

/** Стек в списке /stacks (3.8.1). */
export interface StackInfo {
  name: string
  services: number
  running: number
  desired: number
  managed?: boolean // развёрнут из файла (FR-14): есть сохранённый исходник
  /** Сколько сервисов остановлено панелью (их вернёт «Запустить»). */
  stopped: number
  /** Сколько сервисов можно остановить (replicated с репликами > 0). */
  stoppable: number
  usage?: Usage | null
}

/** Сеть/secret/config, задействованный стеком (3.8.2). */
export interface StackResource {
  name: string
  /** Объект не принадлежит стеку: при удалении стека останется. */
  external: boolean
}

/** Страница стека (GET /stacks/{name}, 3.8.2). */
export interface StackDetail {
  name: string
  managed: boolean
  running: number
  desired: number
  stopped: number
  stoppable: number
  usage?: Usage | null
  services: ServiceInfo[]
  networks: StackResource[]
  secrets: StackResource[]
  configs: StackResource[]
  volumes: string[]
}

/** Причина пропуска сервиса в групповой операции (ТЗ 3.8.6). */
export type StackSkipReason = 'global' | 'already_stopped' | 'not_stopped'

/** Сводка групповой операции над стеком (ТЗ 3.8.6). */
export interface StackOpResult {
  /** Сервисы, над которыми действие выполнено. */
  done: string[]
  skipped: { name: string; reason: StackSkipReason }[]
  failed: { name: string; error: string }[]
  /** Удалённые сети/secrets/configs (только при удалении стека). */
  resources: string[]
}

/** Превью валидации стека (POST /stacks/validate). */
export interface StackPreview {
  name: string
  networks: string[]
  secrets: string[] | null
  configs: string[] | null
  services: { name: string; image: string; mode: string; replicas: number }[]
  /** Поля compose, которые панель не переносит в Swarm (ТЗ 3.14.10). */
  warnings: StackWarning[] | null
  /** external-ресурсы, которых нет в кластере (ТЗ 3.14.5). */
  missing: string[] | null
  /** Что изменится при деплое относительно текущего состояния. */
  changes: { create: string[]; update: string[]; remove: string[] } | null
}

/** Предупреждение конвертера: поле сервиса, которое не попало в спеку. */
export interface StackWarning {
  service?: string
  field: string
  message: string
}

/** Результат деплоя стека (POST /stacks, PUT /stacks/{name}). */
export interface StackDeployResult {
  created: string[] | null
  updated: string[] | null
  removed: string[] | null
  /** Созданные сети/secrets/configs (не сервисы). */
  resources: string[] | null
  failed: { name: string; error: string }[] | null
  warnings: StackWarning[] | null
}

/** Сохранённый исходник стека (GET /stacks/{name}/source). */
export interface StackSource {
  name: string
  compose_yaml: string
  env: string
  env_vars: Record<string, string>
  /** реестр FR-13 для приватных образов; null — образы публичные */
  registry_id: number | null
}

/** Реестр (FR-13); пароль никогда не приходит с сервера. */
export interface RegistryInfo {
  id: number
  address: string
  username: string
  label: string
}
