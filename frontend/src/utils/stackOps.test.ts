// Тесты сводки групповых операций над стеком (ТЗ 3.8.6).
//
// Цена ошибки: тост «стек остановлен», когда половина сервисов продолжает
// работать, — и администратор уходит, считая, что всё выключил.
import { describe, it, expect } from 'vitest'
import { stackOpToasts, stackStatus } from './stackOps'
import { isUpdateNoteworthy } from './usage'
import type { StackOpResult } from '../types'

/** Пустая сводка с переопределением нужных полей. */
function result(part: Partial<StackOpResult>): StackOpResult {
  return { done: [], skipped: [], failed: [], resources: [], ...part }
}

describe('stackOpToasts', () => {
  it('успех: одно сообщение с числом сервисов', () => {
    const toasts = stackOpToasts('stop', 'app', result({ done: ['app_web', 'app_db'] }))
    expect(toasts).toEqual([
      { type: 'success', key: 'stacks.op.stopDone', params: { name: 'app', count: 2, resources: 0 } },
    ])
  })

  it('остановка: global-сервисы названы отдельным предупреждением', () => {
    const toasts = stackOpToasts('stop', 'app', result({
      done: ['app_web'],
      skipped: [{ name: 'app_agent', reason: 'global' }, { name: 'app_db', reason: 'already_stopped' }],
    }))
    expect(toasts.map((x) => x.type)).toEqual(['success', 'warning'])
    expect(toasts[1].params.names).toBe('app_agent')
  })

  it('запуск: global в пропущенных — не повод для предупреждения', () => {
    const toasts = stackOpToasts('start', 'app', result({
      done: ['app_web'],
      skipped: [{ name: 'app_agent', reason: 'not_stopped' }],
    }))
    expect(toasts).toHaveLength(1)
    expect(toasts[0].type).toBe('success')
  })

  it('нечего делать — информационное сообщение, а не «успех»', () => {
    const toasts = stackOpToasts('start', 'app', result({
      skipped: [{ name: 'app_web', reason: 'not_stopped' }],
    }))
    expect(toasts).toEqual([{ type: 'info', key: 'stacks.op.startNothing', params: { name: 'app' } }])
  })

  it('частичный сбой: и успех, и ошибка с именем сервиса', () => {
    const toasts = stackOpToasts('redeploy', 'app', result({
      done: ['app_db'],
      failed: [{ name: 'app_web', error: 'update out of sequence' }],
    }))
    expect(toasts.map((x) => x.type)).toEqual(['success', 'error'])
    expect(toasts[1].params).toEqual({ count: 1, details: 'app_web: update out of sequence' })
  })

  it('полный сбой: успеха нет, только ошибка', () => {
    const toasts = stackOpToasts('stop', 'app', result({
      failed: [{ name: 'app_web', error: 'boom' }],
    }))
    expect(toasts.map((x) => x.type)).toEqual(['error'])
  })

  it('много ошибок: показываются первые три и многоточие', () => {
    const failed = ['a', 'b', 'c', 'd', 'e'].map((name) => ({ name, error: 'x' }))
    const [toast] = stackOpToasts('deploy', 'app', result({ failed }))
    expect(toast.params.count).toBe(5)
    expect(toast.params.details).toBe('a: x; b: x; c: x; …')
  })

  it('удаление пустого стека (без сервисов) — успех', () => {
    const toasts = stackOpToasts('remove', 'ghost', result({}))
    expect(toasts).toEqual([
      { type: 'success', key: 'stacks.op.removeDone', params: { name: 'ghost', count: 0, resources: 0 } },
    ])
  })

  it('удаление: считает и сервисы, и объекты стека', () => {
    const [toast] = stackOpToasts('remove', 'app', result({
      done: ['app_web'], resources: ['network app_net', 'secret app_token'],
    }))
    expect(toast.params).toEqual({ name: 'app', count: 1, resources: 2 })
  })
})


describe('stackStatus', () => {
  const base = { services: 2, running: 0, desired: 0, stopped: 0 }

  it('остановлен панелью — нейтральная подпись, не «авария»', () => {
    expect(stackStatus({ ...base, stopped: 2 })).toEqual({ type: 'default', label: 'stacks.stoppedTag' })
  })
  it('нет сервисов (деплой упал) — отдельная подпись', () => {
    expect(stackStatus({ ...base, services: 0 })).toEqual({ type: 'default', label: 'stacks.emptyTag' })
  })
  it('0 из 0 без остановки панелью — жёлтый: реплики убрали в обход', () => {
    expect(stackStatus(base)).toEqual({ type: 'warning' })
  })
  it('всё работает — зелёный', () => {
    expect(stackStatus({ ...base, running: 4, desired: 4 })).toEqual({ type: 'success' })
  })
  it('не работает ничего — красный', () => {
    expect(stackStatus({ ...base, running: 0, desired: 4 })).toEqual({ type: 'error' })
  })
  it('работает часть — жёлтый', () => {
    expect(stackStatus({ ...base, running: 1, desired: 4 })).toEqual({ type: 'warning' })
  })
  it('часть сервисов остановлена, остальные работают — по числам, зелёный', () => {
    expect(stackStatus({ ...base, running: 2, desired: 2, stopped: 1 })).toEqual({ type: 'success' })
  })
})

describe('isUpdateNoteworthy', () => {
  it('законченное обновление — не событие: Docker хранит этот статус бессрочно', () => {
    expect(isUpdateNoteworthy('completed')).toBe(false)
    expect(isUpdateNoteworthy(undefined)).toBe(false)
    expect(isUpdateNoteworthy('')).toBe(false)
  })
  it('идущее, зависшее и откатившееся обновление показываем', () => {
    for (const st of ['updating', 'paused', 'rollback_started', 'rollback_paused', 'rollback_completed']) {
      expect(isUpdateNoteworthy(st)).toBe(true)
    }
  })
})
