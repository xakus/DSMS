// Pinia-store live-метрик: последний снапшот + скользящее окно на ноду.
// Данные приходят по WS (topic=metrics); окно ограничено 5 мин для
// sparkline-графиков карточек (3.1.2) и 15 мин для страницы ноды.
import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'
import { wsClient } from '../api/ws'
import type { Snapshot } from '../types'

/** Максимум точек в окне ноды: 15 мин / 3 с. */
const WINDOW_POINTS = 300

export const useMetricsStore = defineStore('metrics', () => {
  /** node_id → последний снапшот. */
  const latest = reactive(new Map<string, Snapshot>())
  /** node_id → окно снапшотов (для графиков). */
  const windows = reactive(new Map<string, Snapshot[]>())
  /** Тик для реактивного пересчёта «агент молчит» (3.1.4). */
  const now = ref(Date.now())

  let unsubscribe: (() => void) | null = null
  let staleTimer: number | null = null

  /** Начать слушать метрики (идемпотентно). */
  function start() {
    if (unsubscribe) return
    unsubscribe = wsClient.subscribe({ topic: 'metrics' }, (msg) => {
      const snap = msg.data as Snapshot
      if (!snap?.node_id) return
      latest.set(snap.node_id, snap)
      const win = windows.get(snap.node_id) ?? []
      win.push(snap)
      if (win.length > WINDOW_POINTS) win.splice(0, win.length - WINDOW_POINTS)
      windows.set(snap.node_id, win)
    })
    staleTimer = window.setInterval(() => (now.value = Date.now()), 5000)
  }

  /** Остановить (logout). */
  function stop() {
    unsubscribe?.()
    unsubscribe = null
    if (staleTimer) window.clearInterval(staleTimer)
    staleTimer = null
  }

  /** Загрузить историю окна ноды с сервера (кольцевой буфер panel). */
  async function preload(nodeId: string, snaps: Snapshot[]) {
    windows.set(nodeId, snaps.slice(-WINDOW_POINTS))
    const last = snaps[snaps.length - 1]
    if (last) latest.set(nodeId, last)
  }

  /** Агент молчит: последняя точка старше 15 сек (3.1.4). */
  function isStale(nodeId: string): boolean {
    const snap = latest.get(nodeId)
    if (!snap) return true
    return now.value / 1000 - snap.ts > 15
  }

  return { latest, windows, start, stop, preload, isStale }
})
