// Pinia-store live-метрик: последний снапшот + скользящее окно на ноду.
// Данные приходят по WS (topic=metrics) в буфер, а в реактивное состояние
// (графики/карточки) попадают по таймеру с интервалом из UI-настроек —
// так «Интервал обновления графиков» реально управляет частотой перерисовки.
import { defineStore } from 'pinia'
import { reactive, ref, watch } from 'vue'
import { wsClient } from '../api/ws'
import { useUiStore } from './ui'
import type { Snapshot } from '../types'

/** Максимум точек в окне ноды: 15 мин / 3 с. */
const WINDOW_POINTS = 300

export const useMetricsStore = defineStore('metrics', () => {
  const ui = useUiStore()

  /** node_id → последний снапшот (реактивно, обновляется по таймеру). */
  const latest = reactive(new Map<string, Snapshot>())
  /** node_id → окно снапшотов для графиков (реактивно, по таймеру). */
  const windows = reactive(new Map<string, Snapshot[]>())
  /** Тик времени для «агент молчит» и перерисовки. */
  const now = ref(Date.now())

  // Неreactive буфер: WS пишет сюда мгновенно, в UI переносится по таймеру.
  const rawLatest = new Map<string, Snapshot>()
  const rawWindows = new Map<string, Snapshot[]>()

  let unsubscribe: (() => void) | null = null
  let flushTimer: number | null = null

  /** Перенести накопленные данные в реактивное состояние → перерисовка графиков. */
  function flush() {
    rawLatest.forEach((v, k) => latest.set(k, v))
    // slice → новая ссылка массива, чтобы computed графиков пересчитались.
    rawWindows.forEach((v, k) => windows.set(k, v.slice()))
    now.value = Date.now()
  }

  /** Пересоздать таймер обновления с текущим интервалом. */
  function restartFlush() {
    if (flushTimer) window.clearInterval(flushTimer)
    flushTimer = window.setInterval(flush, ui.refreshMs)
  }

  // Смена интервала в Settings применяется на лету (если уже слушаем).
  watch(
    () => ui.refreshMs,
    () => {
      if (flushTimer) restartFlush()
    },
  )

  /** Начать слушать метрики (идемпотентно). */
  function start() {
    if (unsubscribe) return
    unsubscribe = wsClient.subscribe({ topic: 'metrics' }, (msg) => {
      const snap = msg.data as Snapshot
      if (!snap?.node_id) return
      rawLatest.set(snap.node_id, snap)
      const win = rawWindows.get(snap.node_id) ?? []
      win.push(snap)
      if (win.length > WINDOW_POINTS) win.splice(0, win.length - WINDOW_POINTS)
      rawWindows.set(snap.node_id, win)
    })
    flush() // первый показ сразу
    restartFlush()
  }

  /** Остановить (logout). */
  function stop() {
    unsubscribe?.()
    unsubscribe = null
    if (flushTimer) window.clearInterval(flushTimer)
    flushTimer = null
  }

  /** Загрузить историю окна ноды с сервера (кольцевой буфер panel). */
  async function preload(nodeId: string, snaps: Snapshot[]) {
    const win = snaps.slice(-WINDOW_POINTS)
    rawWindows.set(nodeId, win)
    windows.set(nodeId, win.slice())
    const last = snaps[snaps.length - 1]
    if (last) {
      rawLatest.set(nodeId, last)
      latest.set(nodeId, last)
    }
  }

  /** Затравка последнего снапшота из REST-ответа /nodes. */
  function seed(nodeId: string, snap: Snapshot) {
    rawLatest.set(nodeId, snap)
    latest.set(nodeId, snap)
  }

  /** Агент молчит: последняя точка старше 15 сек (3.1.4). */
  function isStale(nodeId: string): boolean {
    const snap = latest.get(nodeId)
    if (!snap) return true
    return now.value / 1000 - snap.ts > 15
  }

  return { latest, windows, start, stop, preload, seed, isStale }
})
