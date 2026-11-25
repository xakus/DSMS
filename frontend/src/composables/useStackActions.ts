// Действия над стеком целиком (FR-08 3.8.3): остановить, запустить, деплой,
// передеплой. Общие для списка стеков и страницы стека — подтверждения и
// сообщения в двух местах не расходятся.
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import type { StackOpResult } from '../types'
import { stackOpToasts, type StackOp } from '../utils/stackOps'

/** Сколько держать тост с ошибкой, мс: в нём имена сервисов, их надо успеть прочитать. */
const ERROR_TOAST_MS = 10000

/** Путь к API стека; имя кодируется — в URL оно попадает как есть. */
export function stackPath(name: string, suffix = ''): string {
  return `/stacks/${encodeURIComponent(name)}${suffix}`
}

/**
 * @param onChanged вызывается после любого действия — обновить данные экрана
 */
export function useStackActions(onChanged: () => void | Promise<void>) {
  const { t } = useI18n()
  const message = useMessage()
  const dialog = useDialog()

  /** Показать итог операции: успех, предупреждения, ошибки с именами сервисов. */
  function report(op: StackOp, name: string, res: StackOpResult) {
    for (const toast of stackOpToasts(op, name, res)) {
      const text = t(toast.key, toast.params)
      if (toast.type === 'error') message.error(text, { duration: ERROR_TOAST_MS, closable: true })
      else message[toast.type](text)
    }
  }

  /** Выполнить действие и сообщить результат. Ошибку запроса показываем тостом. */
  async function run(op: StackOp, name: string, method: 'POST' | 'DELETE', suffix: string): Promise<boolean> {
    try {
      const res = await api<StackOpResult>(stackPath(name, suffix), { method })
      report(op, name, res)
      return res.failed.length === 0
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : t('common.error'), { duration: ERROR_TOAST_MS, closable: true })
      return false
    } finally {
      await onChanged()
    }
  }

  /** Остановить все сервисы стека — с подтверждением. */
  function stop(name: string) {
    dialog.warning({
      title: t('stacks.stopConfirm', { name }),
      content: t('stacks.stopHint'),
      positiveText: t('stacks.stopBtn'),
      negativeText: t('common.cancel'),
      // Promise держит кнопку в loading — повторный клик невозможен.
      onPositiveClick: () => run('stop', name, 'POST', '/stop'),
    })
  }

  /** Запустить остановленные сервисы стека. Подтверждение не нужно: действие возвращает прежнее состояние. */
  function start(name: string) {
    return run('start', name, 'POST', '/start')
  }

  /** Передеплой: перезапуск всех сервисов на тех же образах. */
  function redeploy(name: string) {
    dialog.info({
      title: t('stacks.redeployConfirm', { name }),
      positiveText: t('common.confirm'),
      negativeText: t('common.cancel'),
      onPositiveClick: () => run('redeploy', name, 'POST', '/redeploy'),
    })
  }

  /**
   * Деплой: каждому сервису — свежий образ по тегу. Операция долгая, поэтому
   * окно закрываем сразу (без Promise), а итог приходит тостом: иначе окно
   * «висит», пользователь жмёт повторно и плодит лишние задачи.
   */
  function deploy(name: string) {
    dialog.info({
      title: t('stacks.deployConfirm', { name }),
      content: t('stacks.deployHint'),
      positiveText: t('common.confirm'),
      negativeText: t('common.cancel'),
      onPositiveClick: () => {
        message.info(t('stacks.deployStarted'))
        void run('deploy', name, 'POST', '/deploy')
      },
    })
  }

  /** Удалить стек (после подтверждения вводом имени — см. StackRemoveModal). */
  function remove(name: string) {
    return run('remove', name, 'DELETE', '')
  }

  return { stop, start, redeploy, deploy, remove }
}
