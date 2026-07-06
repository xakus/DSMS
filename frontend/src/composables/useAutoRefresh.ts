// Переиспользуемый авто-рефреш: вызывает fn с интервалом из UI-настроек
// и пересоздаёт таймер при изменении интервала (Settings меняет на лету).
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { useUiStore } from '../stores/ui'

/** Периодически вызывать fn с текущим интервалом обновления. */
export function useAutoRefresh(fn: () => void) {
  const ui = useUiStore()
  let timer: number | null = null

  function restart() {
    if (timer) window.clearInterval(timer)
    timer = window.setInterval(fn, ui.refreshMs)
  }

  // Первый вызов сразу при монтировании, далее — по таймеру.
  onMounted(() => {
    fn()
    restart()
  })
  watch(() => ui.refreshMs, restart)
  onBeforeUnmount(() => {
    if (timer) window.clearInterval(timer)
  })
}
