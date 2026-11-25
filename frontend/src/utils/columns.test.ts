import { describe, expect, it } from 'vitest'
import { distributeWidths, minTableWidth, type ColumnSpec } from './columns'

const cols: ColumnSpec[] = [
  { key: 'name', base: 200, grow: 2 },
  { key: 'cpu', base: 100, grow: 1 },
  { key: 'actions', base: 290 },
]

describe('distributeWidths', () => {
  it('отдаёт минимальные ширины, когда места не хватает', () => {
    expect(distributeWidths(cols, 400)).toEqual({ name: 200, cpu: 100, actions: 290 })
  })

  it('отдаёт минимальные ширины, пока контейнер не измерен', () => {
    expect(distributeWidths(cols, 0)).toEqual({ name: 200, cpu: 100, actions: 290 })
  })

  it('делит излишек пропорционально grow, колонка действий не растёт', () => {
    // 890 − 590 = 300 px излишка: 200 имени, 100 CPU.
    expect(distributeWidths(cols, 890)).toEqual({ name: 400, cpu: 200, actions: 290 })
  })

  it('сумма ширин равна доступной ширине даже при некратном излишке', () => {
    const w = distributeWidths(cols, 1001)
    expect(w.name + w.cpu + w.actions).toBe(1001)
    expect(w.actions).toBe(290)
  })

  it('на очень широком экране имя не забирает всё место', () => {
    const wide: ColumnSpec[] = [
      { key: 'name', base: 220, grow: 3 },
      { key: 'a', base: 130, grow: 1 },
      { key: 'b', base: 130, grow: 1 },
      { key: 'c', base: 130, grow: 1 },
    ]
    const w = distributeWidths(wide, 3600)
    // Раньше имя получало всё свободное: 3600 − 390 = 3210 px.
    expect(w.name).toBeLessThan(3600 / 2)
    expect(w.a).toBeGreaterThan(500)
  })

  it('без растущих колонок ширины не меняются', () => {
    const fixed: ColumnSpec[] = [{ key: 'x', base: 100 }, { key: 'y', base: 50 }]
    expect(distributeWidths(fixed, 1000)).toEqual({ x: 100, y: 50 })
  })
})

describe('minTableWidth', () => {
  it('складывает минимальные ширины', () => {
    expect(minTableWidth(cols)).toBe(590)
  })
})
