<script setup lang="ts">
// Карточка ноды на Dashboard (FR-01 3.1.1–3.1.4):
// роль/лидер/состояние, sparkline CPU/RAM (окно 5 мин),
// текущие Disk/Net, серая карточка при молчании агента.
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NTag, NSpace } from 'naive-ui'
import type uPlot from 'uplot'
import Sparkline from './Sparkline.vue'
import { useMetricsStore } from '../stores/metrics'
import { fmtBps, fmtPct, fmtBytes } from '../utils/format'
import type { NodeInfo } from '../types'

const props = defineProps<{ node: NodeInfo }>()
const router = useRouter()
const { t } = useI18n()
const metrics = useMetricsStore()

/** Точек в окне sparkline: 5 мин / 3 с (3.1.2). */
const SPARK_POINTS = 100

/** Агент молчит > 15 сек → карточка серая + предупреждение (3.1.4). */
const stale = computed(() => metrics.isStale(props.node.id))

/** Последний снапшот: из WS-store, fallback — из ответа /nodes. */
const snap = computed(() => metrics.latest.get(props.node.id) ?? props.node.metrics)

/** Данные sparkline CPU: [ts[], pct[]]. */
const cpuData = computed<uPlot.AlignedData>(() => {
  const win = (metrics.windows.get(props.node.id) ?? []).slice(-SPARK_POINTS)
  return [win.map((s) => s.ts), win.map((s) => s.cpu.total_pct)]
})

/** Данные sparkline RAM (% использования). */
const memData = computed<uPlot.AlignedData>(() => {
  const win = (metrics.windows.get(props.node.id) ?? []).slice(-SPARK_POINTS)
  return [win.map((s) => s.ts), win.map((s) => (s.mem.total ? (s.mem.used / s.mem.total) * 100 : 0))]
})

/** Суммарные скорости сети по интерфейсам. */
const netTotals = computed(() => {
  const nets = snap.value?.net ?? []
  return {
    rx: nets.reduce((a, n) => a + n.rx_bps, 0),
    tx: nets.reduce((a, n) => a + n.tx_bps, 0),
  }
})

/** Использование корневого диска (для строки Disk). */
const rootDisk = computed(() => {
  const disks = snap.value?.disk ?? []
  return disks.find((d) => d.mount === '/') ?? disks[0]
})

/** Цвет тега состояния (разд. 6.3). */
const stateType = computed(() => {
  if (props.node.state === 'ready' && props.node.availability === 'active') return 'success'
  if (props.node.state === 'down') return 'error'
  return 'warning'
})
</script>

<template>
  <n-card
    hoverable class="node-card" :class="{ stale }"
    @click="router.push({ name: 'node', params: { id: node.id } })"
  >
    <template #header>
      <n-space align="center" :size="8">
        <span>{{ node.leader ? '👑' : '' }} {{ node.hostname }}</span>
        <n-tag size="small" :bordered="false">{{ node.role }}</n-tag>
        <n-tag size="small" :type="stateType" :bordered="false">
          {{ node.availability === 'active' ? node.state : node.availability }}
        </n-tag>
        <n-tag v-if="stale" size="small" type="warning" :bordered="false">⚠ {{ t('nodes.agentSilent') }}</n-tag>
      </n-space>
    </template>

    <div class="metric-row">
      <span class="label">CPU {{ fmtPct(snap?.cpu.total_pct) }}</span>
      <Sparkline :data="cpuData" :y-max="100" color="#63e2b7" />
    </div>
    <div class="metric-row">
      <span class="label">
        RAM {{ snap ? fmtPct((snap.mem.used / snap.mem.total) * 100) : '—' }}
      </span>
      <Sparkline :data="memData" :y-max="100" color="#70c0e8" />
    </div>
    <div class="footer-row">
      <span v-if="rootDisk">💾 {{ fmtBytes(rootDisk.used) }} / {{ fmtBytes(rootDisk.total) }}</span>
      <span>⇅ ↓{{ fmtBps(netTotals.rx) }} ↑{{ fmtBps(netTotals.tx) }}</span>
    </div>
    <div class="addr">{{ node.addr }}</div>
  </n-card>
</template>

<style scoped>
/* Карточка кликабельна; при молчании агента — приглушена (3.1.4) */
.node-card {
  cursor: pointer;
}
/* «Агент молчит» — лёгкое приглушение (warning-тег уже сигнализирует),
   без сильного затемнения, чтобы не выглядело как пелена поверх UI. */
.node-card.stale {
  opacity: 0.82;
}
.metric-row {
  margin-bottom: 8px;
}
.label {
  font-size: 14px;
  opacity: 0.8;
}
.footer-row {
  display: flex;
  justify-content: space-between;
  font-size: 14px;
  opacity: 0.85;
  margin-top: 4px;
}
.addr {
  font-size: 14px;
  opacity: 0.5;
  margin-top: 4px;
}
</style>
