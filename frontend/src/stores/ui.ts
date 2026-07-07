// Pinia-store UI-настроек, хранимых локально в браузере (не на сервере):
// интервал обновления данных на экранах. Влияет на периодический опрос
// REST-данных (списки нод/сервисов/томов) и частоту перерисовки графиков.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../api/client'

/** Границы интервала обновления, мс (1с … 30с — совпадает с сервером). */
export const MIN_REFRESH_MS = 1000
export const MAX_REFRESH_MS = 30000
const DEFAULT_REFRESH_MS = 3000

/** Прочитать сохранённое значение с валидацией. */
function loadRefresh(): number {
  const raw = Number(localStorage.getItem('dsms.refreshMs'))
  if (!Number.isFinite(raw) || raw < MIN_REFRESH_MS || raw > MAX_REFRESH_MS) {
    return DEFAULT_REFRESH_MS
  }
  return raw
}

export const useUiStore = defineStore('ui', () => {
  /** Интервал обновления, мс. */
  const refreshMs = ref(loadRefresh())

  /** Задать интервал локально (с клампом и кэшем в localStorage).
   *  Это частота перерисовки графиков на фронте. Источник правды —
   *  серверная настройка metrics.interval_sec (управляет и агентами). */
  function setRefreshMs(ms: number) {
    const v = Math.min(MAX_REFRESH_MS, Math.max(MIN_REFRESH_MS, Math.round(ms)))
    refreshMs.value = v
    localStorage.setItem('dsms.refreshMs', String(v))
  }

  /** Подтянуть интервал с сервера (после логина) — синхронизация с настройкой,
   *  которую видят агенты. localStorage служит лишь кэшем для мгновенного старта. */
  async function loadFromServer() {
    try {
      const s = await api<{ metrics_interval_sec: number }>('/settings')
      if (s?.metrics_interval_sec) setRefreshMs(s.metrics_interval_sec * 1000)
    } catch { /* настройки недоступны — оставляем кэш */ }
  }

  return { refreshMs, setRefreshMs, loadFromServer }
})
