// Pinia-store алертов (FR-12): активные + live-обновления по WS,
// тосты на новые critical-алерты делает подписчик в AppLayout.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../api/client'
import { wsClient } from '../api/ws'

/** Алерт (актив/резолв/событие) — формат WS и REST. */
export interface AlertItem {
  id: number
  rule: string
  severity: 'warning' | 'critical'
  state: 'active' | 'resolved' | 'event'
  object_type: string
  object_id: string
  message: string
  opened_at: number
  resolved_at?: number
}

export const useAlertsStore = defineStore('alerts', () => {
  const active = ref<AlertItem[]>([])
  const history = ref<AlertItem[]>([])
  /** Колбэк для тостов на новые алерты (устанавливает AppLayout). */
  let onNew: ((a: AlertItem) => void) | null = null
  let unsub: (() => void) | null = null

  /** Загрузить активные + историю с сервера. */
  async function load() {
    const res = await api<{ active: AlertItem[]; history: AlertItem[] }>('/alerts')
    active.value = res.active ?? []
    history.value = res.history ?? []
  }

  /** Начать слушать WS (идемпотентно). */
  function start(notify?: (a: AlertItem) => void) {
    if (notify) onNew = notify
    if (unsub) return
    unsub = wsClient.subscribe({ topic: 'alerts' }, (msg) => {
      const a = msg as unknown as AlertItem
      if (a.state === 'active') {
        // добавить, если ещё нет
        if (!active.value.some((x) => x.id === a.id)) active.value.unshift(a)
        onNew?.(a)
      } else if (a.state === 'resolved') {
        active.value = active.value.filter((x) => x.id !== a.id)
      } else {
        onNew?.(a) // событие (task_failed)
      }
      history.value.unshift(a)
    })
  }

  /** Остановить (logout). */
  function stop() {
    unsub?.()
    unsub = null
  }

  return { active, history, load, start, stop }
})
