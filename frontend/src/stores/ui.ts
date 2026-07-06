// Pinia-store UI-настроек, хранимых локально в браузере (не на сервере):
// интервал обновления данных на экранах. Влияет на периодический опрос
// REST-данных (списки нод/сервисов/томов) и частоту перерисовки графиков.
import { defineStore } from 'pinia'
import { ref } from 'vue'

/** Границы интервала обновления, мс (0.5с … 30с). */
export const MIN_REFRESH_MS = 500
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

  /** Задать интервал (с клампом и сохранением). */
  function setRefreshMs(ms: number) {
    const v = Math.min(MAX_REFRESH_MS, Math.max(MIN_REFRESH_MS, Math.round(ms)))
    refreshMs.value = v
    localStorage.setItem('dsms.refreshMs', String(v))
  }

  return { refreshMs, setRefreshMs }
})
