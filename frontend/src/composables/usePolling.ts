// Периодический опрос списка без «дёрганья» интерфейса.
//
// Что было плохо в самодельных setInterval на экранах:
//  - спиннер таблицы мигал на каждом опросе;
//  - при недоступной панели тост об ошибке сыпался на каждый тик;
//  - медленный ответ накладывался на следующий запрос.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'

/**
 * Интервал опроса списков сервисов и стеков, мс. Агенты шлют метрики раз в
 * 1–30 с (по умолчанию 3 с): чаще 5 с цифры всё равно почти не меняются,
 * а каждый опрос — три запроса к Docker API на manager-ноде.
 */
export const POLL_INTERVAL_MS = 5000

/**
 * Вызывает fetcher сразу и далее каждые intervalMs.
 *
 * @param fetcher    загрузка данных; при ошибке должна бросить исключение
 * @param intervalMs интервал опроса, мс
 * @returns loading — true только до первого ответа (для спиннера);
 *          refresh — обновить сейчас (после действия пользователя)
 */
export function usePolling(fetcher: () => Promise<void>, intervalMs = POLL_INTERVAL_MS) {
  const { t } = useI18n()
  const message = useMessage()

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

  onMounted(() => {
    void refresh()
    timer = window.setInterval(() => {
      // Вкладка в фоне — кластер зря не дёргаем.
      if (!document.hidden) void refresh()
    }, intervalMs)
  })
  onBeforeUnmount(() => {
    if (timer) window.clearInterval(timer)
  })

  return { loading, refresh }
}
