import { describe, expect, it } from 'vitest'
import { replicasHint, replicasTone } from './replicas'

/** Переводчик-заглушка: ключ и параметры одной строкой. */
const t = (key: string, params?: Record<string, unknown>) =>
  params ? `${key} ${JSON.stringify(params)}` : key

describe('replicasTone', () => {
  it('зелёный — работает ровно столько, сколько нужно', () => {
    expect(replicasTone({ running: 2, desired: 2, stopped: false })).toBe('success')
  })

  it('красный — работает меньше, чем нужно', () => {
    expect(replicasTone({ running: 1, desired: 2, stopped: false })).toBe('error')
  })

  it('жёлтый — работает больше, чем нужно (9 из 3: обновление не завершилось)', () => {
    // Раньше это показывалось зелёным, как норма.
    expect(replicasTone({ running: 9, desired: 3, stopped: false })).toBe('warning')
  })

  it('серый — остановлен панелью', () => {
    expect(replicasTone({ running: 0, desired: 0, stopped: true })).toBe('default')
  })

  it('жёлтый — ноль реплик без остановки панелью', () => {
    expect(replicasTone({ running: 0, desired: 0, stopped: false })).toBe('warning')
  })
})

describe('replicasHint', () => {
  it('объясняет лишние задачи', () => {
    const hint = replicasHint({ running: 9, desired: 3, stopped: false }, t)
    expect(hint).toContain('services.replicasHint.counts')
    expect(hint).toContain('services.replicasHint.over')
  })

  it('объясняет нехватку задач', () => {
    expect(replicasHint({ running: 1, desired: 3, stopped: false }, t)).toContain('services.replicasHint.under')
  })

  it('для нормы — только числа', () => {
    const hint = replicasHint({ running: 3, desired: 3, stopped: false }, t)
    expect(hint).not.toContain('over')
    expect(hint).not.toContain('under')
  })

  it('для остановленного — своё пояснение', () => {
    expect(replicasHint({ running: 0, desired: 0, stopped: true }, t)).toBe('services.replicasHint.stopped')
  })
})
