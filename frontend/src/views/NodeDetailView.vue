<script setup lang="ts">
// Страница ноды (FR-02) + управление (FR-03):
// живые графики CPU/RAM/Disk I/O/Net (15 мин), таблицы дисков/сети/задач,
// labels-редактор, promote/demote, availability, remove.
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCard, NSpace, NButton, NTag, NDataTable, NProgress, NGrid, NGi,
  NDynamicInput, NModal, NRadioGroup, NRadioButton, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { DocumentTextOutline, RefreshOutline } from '@vicons/ionicons5'
import type uPlot from 'uplot'
import AppLayout from '../components/AppLayout.vue'
import UPlotChart from '../components/UPlotChart.vue'
import { rowActions } from '../utils/actions'
import { api, ApiError } from '../api/client'
import { useMetricsStore } from '../stores/metrics'
import { fmtBps, fmtBytes, fmtPct, fmtUptime } from '../utils/format'
import type { NodeDetail, NodeTask, Snapshot } from '../types'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const metrics = useMetricsStore()

const nodeId = route.params.id as string
const node = ref<NodeDetail | null>(null)
const showLabels = ref(false)
/** Пары ключ=значение для NDynamicInput labels-редактора. */
const labelPairs = ref<{ key: string; value: string }[]>([])

let refreshTimer: number | null = null

/** Загрузить детали ноды + live-окно метрик из буфера панели. */
async function load() {
  try {
    node.value = await api<NodeDetail>(`/nodes/${nodeId}`)
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('common.loadFailed'))
    return
  }
  const win = await api<Snapshot[]>(`/metrics/nodes/${nodeId}`)
  if (win?.length) metrics.preload(nodeId, win)
}

onMounted(() => {
  metrics.start()
  load()
  refreshTimer = window.setInterval(load, 15000) // состав задач/labels
})
onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
})

/** Живое окно метрик ноды. */
const win = computed(() => metrics.windows.get(nodeId) ?? [])
const snap = computed(() => metrics.latest.get(nodeId) ?? node.value?.metrics)

// --- выбор окна графиков: 15m live / история из SQLite (3.2.1) ---

/** Минутная точка истории (metrics_1m). */
interface HistPoint {
  ts: number
  cpu_pct: number
  mem_used: number
  mem_total: number
  disk_json: string
  net_json: string
}

const windowSel = ref<'15m' | '1h' | '6h' | '24h' | '7d'>('15m')
const hist = ref<HistPoint[]>([])
const isLive = computed(() => windowSel.value === '15m')

/** Загрузка истории при смене окна. */
watch(windowSel, async (w) => {
  if (w === '15m') return
  hist.value = await api<HistPoint[]>(`/metrics/nodes/${nodeId}?window=${w}`)
})

/** Распарсить *_json точки истории (агрегированные скорости). */
function histAgg(p: HistPoint): { read: number; write: number; rx: number; tx: number } {
  let read = 0, write = 0, rx = 0, tx = 0
  try {
    const d = JSON.parse(p.disk_json || '{}')
    read = d.read_bps ?? 0
    write = d.write_bps ?? 0
  } catch { /* пустой json */ }
  try {
    const n = JSON.parse(p.net_json || '{}')
    rx = n.rx_bps ?? 0
    tx = n.tx_bps ?? 0
  } catch { /* пустой json */ }
  return { read, write, rx, tx }
}

// --- данные графиков: live-буфер или история ---
const cpuData = computed<uPlot.AlignedData>(() =>
  isLive.value
    ? [win.value.map((s) => s.ts), win.value.map((s) => s.cpu.total_pct)]
    : [hist.value.map((p) => p.ts), hist.value.map((p) => p.cpu_pct)])
const memData = computed<uPlot.AlignedData>(() =>
  isLive.value
    ? [win.value.map((s) => s.ts), win.value.map((s) => s.mem.used)]
    : [hist.value.map((p) => p.ts), hist.value.map((p) => p.mem_used)])
const diskIOData = computed<uPlot.AlignedData>(() =>
  isLive.value
    ? [
        win.value.map((s) => s.ts),
        win.value.map((s) => (s.disk ?? []).reduce((a, d) => a + d.read_bps, 0)),
        win.value.map((s) => (s.disk ?? []).reduce((a, d) => a + d.write_bps, 0)),
      ]
    : [
        hist.value.map((p) => p.ts),
        hist.value.map((p) => histAgg(p).read),
        hist.value.map((p) => histAgg(p).write),
      ])
const netData = computed<uPlot.AlignedData>(() =>
  isLive.value
    ? [
        win.value.map((s) => s.ts),
        win.value.map((s) => (s.net ?? []).reduce((a, n) => a + n.rx_bps, 0)),
        win.value.map((s) => (s.net ?? []).reduce((a, n) => a + n.tx_bps, 0)),
      ]
    : [
        hist.value.map((p) => p.ts),
        hist.value.map((p) => histAgg(p).rx),
        hist.value.map((p) => histAgg(p).tx),
      ])

// --- таблицы ---
/** Диски: подсветка > 85% (3.2.2). */
const diskColumns = computed<DataTableColumns<Record<string, unknown>>>(() => [
  { title: t('nodes.mount'), key: 'mount' },
  {
    title: t('nodes.used'), key: 'used',
    render: (row) => {
      const pct = Number(row.pct)
      return h(NProgress, {
        type: 'line', percentage: Math.round(pct),
        status: pct > 85 ? 'error' : 'success', showIndicator: true,
      })
    },
  },
  { title: t('nodes.size'), key: 'size' },
  { title: 'I/O', key: 'io' },
])
const diskRows = computed(() =>
  (snap.value?.disk ?? []).map((d) => ({
    mount: d.mount,
    pct: d.total ? (d.used / d.total) * 100 : 0,
    size: `${fmtBytes(d.used)} / ${fmtBytes(d.total)}`,
    io: `R ${fmtBps(d.read_bps)} · W ${fmtBps(d.write_bps)}`,
  })),
)

const netColumns = computed<DataTableColumns<Record<string, unknown>>>(() => [
  { title: t('nodes.iface'), key: 'iface' },
  { title: 'RX', key: 'rx' },
  { title: 'TX', key: 'tx' },
  { title: 'Err/Drop', key: 'errs' },
])
const netRows = computed(() =>
  (snap.value?.net ?? []).map((n) => ({
    iface: n.iface,
    rx: fmtBps(n.rx_bps),
    tx: fmtBps(n.tx_bps),
    errs: `${n.errors}/${n.drops}`,
  })),
)

/** Контейнеры ноды с per-container метриками (3.2.6, топ-N от агента). */
const containerRows = computed(() => snap.value?.containers ?? [])
const containerColumns = computed<DataTableColumns<Record<string, unknown>>>(() => [
  { title: t('services.name'), key: 'name', ellipsis: true },
  { title: t('nodes.service'), key: 'service', render: (r) => (r.service as string) || '—' },
  { title: 'CPU %', key: 'cpu_pct', width: 90, sorter: 'default' },
  {
    title: 'RAM', key: 'mem', width: 180,
    render: (r) => `${fmtBytes(r.mem_used as number)}${(r.mem_limit as number) ? ' / ' + fmtBytes(r.mem_limit as number) : ''}`,
  },
])

/** Задачи на ноде (3.2.4) со ссылками на сервисы (появятся в этапе 3). */
const taskColumns = computed<DataTableColumns<NodeTask>>(() => [
  {
    // Клик по сервису ведёт на его страницу — оттуда логи, редеплой, действия.
    title: t('nodes.service'), key: 'service',
    render: (row) =>
      row.service_id
        ? h(RouterLink, { to: { name: 'service', params: { id: row.service_id } }, class: 'svc-link' },
            { default: () => row.service || row.service_id })
        : (row.service || '—'),
  },
  { title: 'Slot', key: 'slot', width: 70 },
  {
    title: t('nodes.state'), key: 'state',
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: row.state === 'running' ? 'success' : row.state === 'failed' ? 'error' : 'warning' },
        { default: () => row.state }),
  },
  { title: t('nodes.message'), key: 'message', ellipsis: true },
  {
    title: t('services.actions'), key: 'a', width: 110,
    render: (row) =>
      row.service_id
        ? rowActions([
            { icon: DocumentTextOutline, tip: t('nav.logs'), onClick: () => router.push({ name: 'logs', query: { service: row.service_id } }) },
            { icon: RefreshOutline, tip: t('services.redeployTip'), onClick: () => redeployService(row.service_id) },
          ])
        : null,
  },
])

/** Быстрый редеплой сервиса прямо со страницы ноды (3.4.3). */
function redeployService(serviceId: string) {
  dialog.info({
    title: t('services.redeployConfirm', { name: serviceId }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await api(`/services/${serviceId}/redeploy`, { method: 'POST' })
        message.success('OK')
      } catch (e) {
        message.error(e instanceof ApiError ? e.message : 'error')
      }
    },
  })
}

// --- управление нодой (FR-03) ---

/** Общий обработчик действий с подтверждением. */
function confirmAction(title: string, run: () => Promise<unknown>) {
  dialog.warning({
    title,
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        const res = (await run()) as { warning?: string } | undefined
        if (res?.warning) message.warning(res.warning)
        else message.success('OK')
        await load()
      } catch (e) {
        message.error(e instanceof ApiError ? e.message : 'error')
      }
    },
  })
}

function setRole(role: 'manager' | 'worker') {
  confirmAction(t(role === 'manager' ? 'nodes.promoteConfirm' : 'nodes.demoteConfirm'), () =>
    api(`/nodes/${nodeId}/role`, { method: 'POST', body: { role } }))
}

function setAvailability(availability: 'active' | 'pause' | 'drain') {
  confirmAction(
    availability === 'drain' ? t('nodes.drainConfirm') : `${availability}?`,
    () => api(`/nodes/${nodeId}/availability`, { method: 'POST', body: { availability } }))
}

/** Удаление ноды — двойное подтверждение для force (3.3.3). */
function removeNode() {
  const isDown = node.value?.state === 'down'
  confirmAction(isDown ? t('nodes.removeConfirm') : t('nodes.forceRemoveConfirm'), async () => {
    await api(`/nodes/${nodeId}${isDown ? '' : '?force=true'}`, { method: 'DELETE' })
    router.replace({ name: 'dashboard' })
  })
}

/** Открыть labels-редактор с текущими значениями (3.2.5). */
function editLabels() {
  labelPairs.value = Object.entries(node.value?.labels ?? {}).map(([key, value]) => ({ key, value }))
  showLabels.value = true
}

/** Сохранить labels (полная замена, PUT). */
async function saveLabels() {
  const labels: Record<string, string> = {}
  for (const p of labelPairs.value) if (p.key) labels[p.key] = p.value
  try {
    await api(`/nodes/${nodeId}/labels`, { method: 'PUT', body: { labels } })
    showLabels.value = false
    message.success('OK')
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  }
}
</script>

<template>
  <AppLayout>
    <template v-if="node">
      <n-space justify="space-between" align="center" class="mb">
        <n-space align="center">
          <h2>{{ node.leader ? '👑' : '' }} {{ node.hostname }}</h2>
          <n-tag :bordered="false">{{ node.role }}</n-tag>
          <n-tag :bordered="false" :type="node.state === 'ready' ? 'success' : 'error'">{{ node.state }}</n-tag>
          <n-tag :bordered="false" type="info">{{ node.availability }}</n-tag>
        </n-space>
        <n-space>
          <n-button v-if="node.role === 'worker'" size="small" @click="setRole('manager')">⬆ Promote</n-button>
          <n-button v-else size="small" @click="setRole('worker')">⬇ Demote</n-button>
          <n-button v-if="node.availability !== 'drain'" size="small" type="warning" @click="setAvailability('drain')">Drain</n-button>
          <n-button v-else size="small" type="success" @click="setAvailability('active')">Activate</n-button>
          <n-button size="small" @click="editLabels">🏷 Labels</n-button>
          <n-button size="small" type="error" @click="removeNode">{{ t('common.remove') }}</n-button>
        </n-space>
      </n-space>

      <div class="sysline mb">
        {{ node.addr }} · {{ node.os }}/{{ node.arch }} · Docker {{ node.engine }}
        <template v-if="snap?.sys"> · ⏱ {{ fmtUptime(snap.sys.uptime) }} · 📦 {{ snap.sys.containers }}</template>
      </div>

      <!-- Селектор окна графиков (3.2.1): 15м live / история из SQLite -->
      <n-radio-group v-model:value="windowSel" size="small" class="mb">
        <n-radio-button value="15m">15m ⚡</n-radio-button>
        <n-radio-button value="1h">1h</n-radio-button>
        <n-radio-button value="6h">6h</n-radio-button>
        <n-radio-button value="24h">24h</n-radio-button>
        <n-radio-button value="7d">7d</n-radio-button>
      </n-radio-group>

      <n-grid cols="1 m:2" responsive="screen" :x-gap="12" :y-gap="12" class="mb">
        <n-gi>
          <n-card :title="`CPU ${fmtPct(snap?.cpu.total_pct)}`" size="small">
            <UPlotChart :data="cpuData" :series="[{ label: 'CPU %', stroke: '#63e2b7', width: 1.5 }]" :y-range="[0, 100]" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card :title="`RAM ${snap ? fmtBytes(snap.mem.used) : '—'} / ${snap ? fmtBytes(snap.mem.total) : '—'}`" size="small">
            <UPlotChart :data="memData" :series="[{ label: 'RAM', stroke: '#70c0e8', width: 1.5 }]" :y-format="fmtBytes" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card title="Disk I/O" size="small">
            <UPlotChart
              :data="diskIOData" :y-format="fmtBps"
              :series="[{ label: 'Read', stroke: '#63e2b7', width: 1.5 }, { label: 'Write', stroke: '#e88080', width: 1.5 }]"
            />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card title="Network" size="small">
            <UPlotChart
              :data="netData" :y-format="fmtBps"
              :series="[{ label: 'RX', stroke: '#63e2b7', width: 1.5 }, { label: 'TX', stroke: '#f2c97d', width: 1.5 }]"
            />
          </n-card>
        </n-gi>
      </n-grid>

      <!-- Таблицы: диски (3.2.2), сеть (3.2.3), задачи (3.2.4) -->
      <n-grid cols="1 m:2" responsive="screen" :x-gap="12" :y-gap="12" class="mb">
        <n-gi>
          <n-card :title="t('nodes.disks')" size="small">
            <n-data-table :columns="diskColumns" :data="diskRows" size="small" :bordered="false" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card :title="t('nodes.network')" size="small">
            <n-data-table :columns="netColumns" :data="netRows" size="small" :bordered="false" />
          </n-card>
        </n-gi>
      </n-grid>

      <!-- Контейнеры ноды: per-container CPU/RAM, топ по нагрузке (3.2.6) -->
      <n-card v-if="containerRows.length" :title="`${t('nodes.containers')} (${snap?.sys?.containers ?? containerRows.length})`" size="small" class="mb">
        <n-data-table :columns="containerColumns" :data="containerRows" size="small" :bordered="false" />
      </n-card>

      <n-card :title="`${t('nodes.tasks')} (${node.tasks.length})`" size="small">
        <n-data-table :columns="taskColumns" :data="node.tasks" size="small" :bordered="false" />
      </n-card>

      <!-- Labels-редактор (3.2.5) -->
      <n-modal v-model:show="showLabels" preset="card" title="Labels" class="labels-modal">
        <n-dynamic-input
          v-model:value="labelPairs" preset="pair"
          :key-placeholder="t('nodes.labelKey')" :value-placeholder="t('nodes.labelValue')"
        />
        <template #footer>
          <n-space justify="end">
            <n-button @click="showLabels = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="saveLabels">{{ t('common.save') }}</n-button>
          </n-space>
        </template>
      </n-modal>
    </template>
  </AppLayout>
</template>

<style scoped>
/* Отступы и служебная строка с инфо о ноде */
.mb {
  margin-bottom: 16px;
}
.sysline {
  font-size: 14px;
  opacity: 0.65;
}
.labels-modal {
  max-width: 560px;
}
/* Ссылка на сервис в таблице задач — акцентный цвет */
:deep(.svc-link) {
  color: var(--n-primary-color, #2563eb);
  text-decoration: none;
}
:deep(.svc-link:hover) {
  text-decoration: underline;
}
</style>
