// Периодический опрос списка без «дёрганья» интерфейса.
//
// Что было плохо в самодельных setInterval на экранах:
//  - спиннер таблицы мигал на каждом опросе;
//  - при недоступной панели тост об ошибке сыпался на каждый тик;
//  - медленный ответ накладывался на следующий запрос.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { useUiStore } from '../stores/ui'

/**
 * Вызывает fetcher сразу и далее с интервалом обновления из настроек.
 *
 * Интервал — тот же, с которым агенты шлют метрики (Настройки → «Интервал
 * метрик», 1–30 с, по умолчанию 3 с). Опрашивать чаще бессмысленно: между
 * батчами агентов цифры не меняются; реже — таблица отстаёт от кластера.
 * Смена настройки применяется на лету.
 *
 * @param fetcher загрузка данных; при ошибке должна бросить исключение
 * @returns loading — true только до первого ответа (для спиннера);
 *          refresh — обновить сейчас (после действия пользователя)
 */
export function usePolling(fetcher: () => Promise<void>) {
  const { t } = useI18n()
  const message = useMessage()
  const ui = useUiStore()

  const loading = ref(true)
  let timer: number | null = null
  let inFlight = false // запрос уже идёт
  let again = false // пока шёл запрос, попросили обновить ещё раз
  let failing = false // прошлый опрос упал — тост уже показан

  async function refresh(): Promise<void> {
    if (inFlight) {
      // Не теряем обновление после действия: повторим, когда текущий закончится.
      again = true
      return
    }
    inFlight = true
    try {
      await fetcher()
      failing = false
    } catch {
      // Один тост на серию сбоев, а не на каждый тик.
      if (!failing) message.error(t('common.loadFailed'))
      failing = true
    } finally {
      inFlight = false
      loading.value = false
    }
    if (again) {
      again = false
      await refresh()
    }
  }

  /** Запустить таймер заново с текущим интервалом из настроек. */
  function restart() {
    if (timer) window.clearInterval(timer)
    timer = window.setInterval(() => {
      // Вкладка в фоне — кластер зря не дёргаем.
      if (!document.hidden) void refresh()
    }, ui.refreshMs)
  }

  onMounted(() => {
    void refresh()
    restart()
  })
  watch(() => ui.refreshMs, restart)
  onBeforeUnmount(() => {
    if (timer) window.clearInterval(timer)
  })

  return { loading, refresh }
}
