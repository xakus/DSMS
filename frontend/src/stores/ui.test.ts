// Тесты стора UI-настроек: интервал обновления читается из localStorage
// и всегда остаётся в допустимых границах.
//
// Смысл: интервал уходит в опрос API и перерисовку графиков. Значение вроде
// 0 мс превратило бы панель в генератор нагрузки на собственный кластер.
import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useUiStore, MIN_REFRESH_MS, MAX_REFRESH_MS } from './ui'

describe('ui store: интервал обновления', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('по умолчанию — разумные 3 секунды', () => {
    expect(useUiStore().refreshMs).toBe(3000)
  })

  it('подхватывает сохранённое значение', () => {
    localStorage.setItem('dsms.refreshMs', '5000')
    expect(useUiStore().refreshMs).toBe(5000)
  })

  it('игнорирует мусор в localStorage', () => {
    for (const bad of ['0', '-1000', '999999', 'быстро', '']) {
      localStorage.setItem('dsms.refreshMs', bad)
      setActivePinia(createPinia())
      expect(useUiStore().refreshMs).toBe(3000)
    }
  })

  it('зажимает значение в границах и сохраняет его', () => {
    const ui = useUiStore()

    ui.setRefreshMs(100) // слишком часто — опрос забил бы API
    expect(ui.refreshMs).toBe(MIN_REFRESH_MS)

    ui.setRefreshMs(120000) // слишком редко — панель перестаёт быть живой
    expect(ui.refreshMs).toBe(MAX_REFRESH_MS)

    ui.setRefreshMs(4500)
    expect(ui.refreshMs).toBe(4500)
    expect(localStorage.getItem('dsms.refreshMs')).toBe('4500')
  })

  it('округляет дробные миллисекунды', () => {
    const ui = useUiStore()
    ui.setRefreshMs(2500.7)
    expect(ui.refreshMs).toBe(2501)
  })
})
