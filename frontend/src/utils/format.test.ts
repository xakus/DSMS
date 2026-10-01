// Тесты форматирования величин (NFR-12).
//
// Эти функции стоят между сырыми метриками и глазами администратора:
// ошибка тут не роняет панель, а тихо показывает неверную цифру, по которой
// принимают решения («на диске 900 GiB свободно» вместо 900 MiB).
import { describe, it, expect } from 'vitest'
import { fmtBytes, fmtBps, fmtPct, fmtUptime } from './format'

describe('fmtBytes', () => {
  it('переводит в подходящую единицу', () => {
    expect(fmtBytes(0)).toBe('0.0 B')
    expect(fmtBytes(512)).toBe('512 B')
    expect(fmtBytes(1024)).toBe('1.0 KiB')
    expect(fmtBytes(1536)).toBe('1.5 KiB')
    expect(fmtBytes(1024 ** 3)).toBe('1.0 GiB')
    expect(fmtBytes(1.5 * 1024 ** 4)).toBe('1.5 TiB')
  })

  it('не уходит выше TiB — иначе разряд «съедается» без предупреждения', () => {
    expect(fmtBytes(5 * 1024 ** 5)).toBe('5120 TiB')
  })

  it('вместо отсутствующих данных показывает прочерк, а не 0', () => {
    expect(fmtBytes(undefined)).toBe('—')
    expect(fmtBytes(NaN)).toBe('—')
    expect(fmtBytes(Infinity)).toBe('—')
  })
})

describe('fmtBps', () => {
  it('добавляет /s к размеру', () => {
    expect(fmtBps(1024)).toBe('1.0 KiB/s')
  })
  it('нет данных — прочерк', () => {
    expect(fmtBps(undefined)).toBe('—')
  })
})

describe('fmtPct', () => {
  it('округляет до одного знака', () => {
    expect(fmtPct(0)).toBe('0.0%')
    expect(fmtPct(42.34)).toBe('42.3%')
    expect(fmtPct(99.99)).toBe('100.0%')
  })
  it('нечисло — прочерк', () => {
    expect(fmtPct(undefined)).toBe('—')
    expect(fmtPct(NaN)).toBe('—')
  })
})

describe('fmtUptime', () => {
  it('показывает две старшие единицы', () => {
    expect(fmtUptime(12 * 86400 + 5 * 3600)).toBe('12d 5h')
    expect(fmtUptime(3 * 3600 + 12 * 60)).toBe('3h 12m')
    expect(fmtUptime(45 * 60)).toBe('45m')
  })
  it('нулевой и отсутствующий аптайм — прочерк', () => {
    expect(fmtUptime(0)).toBe('—')
    expect(fmtUptime(undefined)).toBe('—')
  })
})
