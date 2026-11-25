// Сводка групповой операции над стеком (ТЗ 3.8.6) → сообщения пользователю.
//
// Чистая функция без Vue: по ответу сервера решает, какие тосты показать.
// Вынесена отдельно, чтобы правила «что считать успехом» проверялись тестами.
import type { StackOpResult } from '../types'

/** Групповое действие над стеком. */
export type StackOp = 'stop' | 'start' | 'redeploy' | 'deploy' | 'remove'

/** Одно сообщение: тип тоста + ключ перевода с параметрами. */
export interface StackOpToast {
  type: 'success' | 'info' | 'warning' | 'error'
  key: string
  params: Record<string, string | number>
}

/** Сколько упавших сервисов перечислять в тосте: больше — не читается. */
const MAX_FAILED_SHOWN = 3

/**
 * Превращает сводку операции в список сообщений.
 *
 * @param op    действие
 * @param stack имя стека
 * @param res   сводка от сервера
 * @returns сообщения в порядке показа: итог, предупреждения, ошибки
 */
export function stackOpToasts(op: StackOp, stack: string, res: StackOpResult): StackOpToast[] {
  const out: StackOpToast[] = []
  const done = res.done.length
  const failed = res.failed.length

  if (done > 0 || (op === 'remove' && failed === 0)) {
    // Удаление без сервисов — тоже успех: стек был пустой (деплой упал).
    out.push({
      type: 'success',
      key: `stacks.op.${op}Done`,
      params: { name: stack, count: done, resources: res.resources.length },
    })
  } else if (failed === 0) {
    // Ничего не сделано и ничего не упало — делать было нечего.
    out.push({ type: 'info', key: `stacks.op.${op}Nothing`, params: { name: stack } })
  }

  // Global-сервисы при остановке продолжают работать — об этом надо сказать
  // явно, иначе «стек остановлен» будет неправдой.
  const globals = res.skipped.filter((s) => s.reason === 'global').map((s) => s.name)
  if (op === 'stop' && globals.length > 0) {
    out.push({ type: 'warning', key: 'stacks.op.globalSkipped', params: { names: globals.join(', ') } })
  }

  if (failed > 0) {
    const shown = res.failed.slice(0, MAX_FAILED_SHOWN).map((f) => `${f.name}: ${f.error}`)
    if (failed > MAX_FAILED_SHOWN) shown.push('…')
    out.push({
      type: 'error',
      key: 'stacks.op.failed',
      params: { count: failed, details: shown.join('; ') },
    })
  }
  return out
}

/** Цвет и подпись метки статуса стека. */
export interface StackStatus {
  type: 'success' | 'warning' | 'error' | 'default'
  /** Ключ перевода вместо «running/desired»; undefined — показывать числа. */
  label?: string
}

/**
 * Статус стека для цветной метки в таблице (ТЗ 3.8.1, 3.8.7).
 *
 * - нет сервисов (managed-стек, деплой которого упал) — нейтральная подпись;
 * - всё остановлено панелью — «остановлен», нейтрально: это не авария;
 * - 0 из 0 без остановки панелью (реплики убрали в обход) — жёлтый;
 * - работает всё, что должно, — зелёный; не работает ничего — красный;
 *   работает часть — жёлтый.
 */
export function stackStatus(s: {
  services: number
  running: number
  desired: number
  stopped: number
}): StackStatus {
  if (s.services === 0) return { type: 'default', label: 'stacks.emptyTag' }
  if (s.desired === 0 && s.running === 0) {
    return s.stopped > 0 ? { type: 'default', label: 'stacks.stoppedTag' } : { type: 'warning' }
  }
  if (s.running >= s.desired) return { type: 'success' }
  if (s.running === 0) return { type: 'error' }
  return { type: 'warning' }
}
