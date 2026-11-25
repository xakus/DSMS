// Ячейка «Реплики» для таблиц сервисов: сколько задач работает из нужных,
// цвет состояния (ТЗ 6.3) и значок идущего обновления (ТЗ 3.4.5).
import { h, type VNodeChild } from 'vue'
import { NFlex, NIcon, NTag, NTooltip } from 'naive-ui'
import { SyncOutline } from '@vicons/ionicons5'
import { isUpdateNoteworthy } from './usage'

/** Функция перевода vue-i18n. */
type Translate = (key: string, params?: Record<string, unknown>) => string

/** Цвет метки реплик. */
export type ReplicasTone = 'success' | 'error' | 'warning' | 'default'

/** Что нужно знать о сервисе, чтобы показать его реплики. */
export interface ReplicasRow {
  running: number
  desired: number
  /** Остановлен панелью (scale 0 с запомненным числом реплик). */
  stopped: boolean
  mode?: 'replicated' | 'global'
  update_state?: string
}

/**
 * Цвет реплик:
 *  - серый — остановлен панелью (так задумано, не авария);
 *  - зелёный — работает ровно столько, сколько нужно;
 *  - жёлтый — работает БОЛЬШЕ, чем нужно (обновление не завершилось, старые
 *    задачи не убраны) или сервис с нулём реплик;
 *  - красный — работает меньше, чем нужно.
 */
export function replicasTone(svc: ReplicasRow): ReplicasTone {
  if (svc.stopped) return 'default'
  if (svc.running > svc.desired) return 'warning'
  if (svc.running === svc.desired && svc.desired > 0) return 'success'
  if (svc.running === 0 && svc.desired === 0) return 'warning'
  return 'error'
}

/** Пояснение к числам реплик простыми словами — для подсказки. */
export function replicasHint(svc: ReplicasRow, t: Translate): string {
  if (svc.stopped) return t('services.replicasHint.stopped')
  const counts = t('services.replicasHint.counts', { running: svc.running, desired: svc.desired })
  if (svc.running > svc.desired) return `${counts} ${t('services.replicasHint.over')}`
  if (svc.running < svc.desired) return `${counts} ${t('services.replicasHint.under')}`
  return counts
}

/** Ячейка: метка реплик с подсказкой, пометка global, значок обновления. */
export function replicasCell(row: ReplicasRow, t: Translate): VNodeChild {
  const text = row.stopped ? t('services.stoppedTag') : `${row.running}/${row.desired}`
  const state = row.update_state
  return h(NFlex, { align: 'center', size: 6 }, {
    default: () => [
      h(NTooltip, { style: 'max-width: 360px' }, {
        trigger: () => h(NTag, { size: 'small', bordered: false, type: replicasTone(row), tabindex: 0 },
          { default: () => text }),
        default: () => replicasHint(row, t),
      }),
      row.mode === 'global'
        ? h(NTag, { size: 'tiny', bordered: false }, { default: () => 'global' })
        : null,
      isUpdateNoteworthy(state)
        ? h(NTooltip, null, {
            trigger: () => h(NIcon, { size: 16, tabindex: 0, 'aria-label': t('services.updateState', { state }) },
              { default: () => h(SyncOutline) }),
            default: () => t('services.updateState', { state }),
          })
        : null,
    ],
  })
}
