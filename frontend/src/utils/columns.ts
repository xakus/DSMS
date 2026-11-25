// Раскладка ширины колонок таблицы.
//
// Зачем: у Naive UI колонка без ширины забирает всё свободное место. На
// широком мониторе «Имя» растягивалось на пол-экрана, а остальные данные
// прижимались к правому краю. Здесь лишняя ширина делится между колонками,
// и каждая получает точное число — таблица с фиксированной раскладкой не
// «дышит», когда меняются цифры в ячейках.

/** Описание колонки для раскладки. */
export interface ColumnSpec {
  /** Ключ колонки (как в описании колонок таблицы). */
  key: string
  /** Минимальная ширина, px: меньше колонка не становится. */
  base: number
  /** Доля лишней ширины; 0 или не задано — колонка не растёт (кнопки действий). */
  grow?: number
}

/** Сумма минимальных ширин — ниже неё таблица прокручивается внутри карточки. */
export function minTableWidth(cols: ColumnSpec[]): number {
  return cols.reduce((sum, c) => sum + c.base, 0)
}

/**
 * Делит доступную ширину между колонками.
 *
 * Места меньше суммы минимальных ширин (или ширина ещё не измерена) —
 * колонки получают минимальные ширины. Места больше — излишек делится
 * пропорционально grow.
 *
 * Пример: колонки 200 (grow 2) и 100 (grow 1), доступно 600 px. Излишек
 * 300 px делится 2:1 → 200 + 200 = 400 и 100 + 100 = 200.
 *
 * @param cols      колонки в порядке показа
 * @param available ширина контейнера таблицы, px
 * @returns ширины по ключам колонок, целые px; их сумма равна available,
 *          если места хватает и есть хотя бы одна растущая колонка
 */
export function distributeWidths(cols: ColumnSpec[], available: number): Record<string, number> {
  const out: Record<string, number> = {}
  const base = minTableWidth(cols)
  const growTotal = cols.reduce((sum, c) => sum + (c.grow ?? 0), 0)
  const extra = Math.floor(available) - base

  if (extra <= 0 || growTotal <= 0) {
    for (const c of cols) out[c.key] = c.base
    return out
  }

  let given = 0
  let lastGrowing = ''
  for (const c of cols) {
    const grow = c.grow ?? 0
    const add = Math.floor((extra * grow) / growTotal)
    out[c.key] = c.base + add
    given += add
    if (grow > 0) lastGrowing = c.key
  }
  // Остаток от округления вниз (несколько px) — последней растущей колонке:
  // иначе справа осталась бы полоска пустоты.
  out[lastGrowing] += extra - given
  return out
}
