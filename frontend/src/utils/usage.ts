// Ячейки и колонки «потребление ресурсов» для таблиц сервисов, задач и
// стеков (ТЗ 3.4.7). Один код на все таблицы — цифры везде выглядят одинаково.
import { h, type VNodeChild } from 'vue'
import { NFlex, NIcon, NText, NTooltip } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { AlertCircleOutline, ArrowDownOutline, ArrowUpOutline } from '@vicons/ionicons5'
import type { Usage } from '../types'
import { fmtBps, fmtBytes, fmtCores, shortImageName } from './format'

/** Функция перевода vue-i18n. */
type Translate = (key: string, params?: Record<string, unknown>) => string

/** Строка таблицы с потреблением. */
export interface UsageRow {
  usage?: Usage | null
  /** Сколько задач сейчас работает: нужно, чтобы заметить неполные данные. */
  running?: number
}

/** Сколько hostname показывать в ячейке «Ноды»; остальные — в подсказке. */
const MAX_NODES_SHOWN = 2

/**
 * Ширины колонок потребления, px. Подобраны под самые длинные значения:
 * «100.0%», «1023 MiB» + «из 1023 MiB», «↓ 1023 MiB/s».
 */
const CPU_COL_WIDTH = 95 // «100.0%» + значок неполных данных
const MEM_COL_WIDTH = 115
const NET_COL_WIDTH = 130

/** Значение с единицей («из 256 MiB») не должно рваться между строками. */
const NOWRAP = 'white-space: nowrap'

/** Суммарная ширина трёх колонок потребления — для расчёта ширины таблиц. */
export const USAGE_COLS_WIDTH = CPU_COL_WIDTH + MEM_COL_WIDTH + NET_COL_WIDTH

/** Прочерк «нет данных» приглушённым цветом. */
function dash(): VNodeChild {
  return h(NText, { depth: 3 }, { default: () => '—' })
}

/**
 * Данные пришли не со всех работающих задач (агент на части нод молчит) —
 * сумма занижена, и это надо показать, а не выдавать её за полную.
 */
export function isPartial(row: UsageRow): boolean {
  return !!row.usage && row.running !== undefined && row.usage.tasks < row.running
}

/** Ячейка CPU: проценты одного ядра + значок, если данные неполные. */
export function cpuCell(row: UsageRow, t: Translate): VNodeChild {
  if (!row.usage) return dash()
  const value = fmtCores(row.usage.cpu_cores)
  if (!isPartial(row)) return value
  return h(NTooltip, null, {
    trigger: () =>
      h(NFlex, { align: 'center', size: 4, wrap: false, inline: true, style: NOWRAP }, {
        default: () => [
          value,
          h(NIcon, { size: 16, 'aria-label': t('usage.partialShort') },
            { default: () => h(AlertCircleOutline) }),
        ],
      }),
    default: () => t('usage.partial', { got: row.usage!.tasks, total: row.running }),
  })
}

/** Ячейка RAM: занято, второй строкой — лимит (если он есть). */
export function memCell(row: UsageRow, t: Translate): VNodeChild {
  if (!row.usage) return dash()
  const used = fmtBytes(row.usage.mem_used)
  if (!row.usage.mem_limit) return used
  return h(NFlex, { vertical: true, size: 0 }, {
    default: () => [
      used,
      h(NText, { depth: 3, style: NOWRAP }, { default: () => t('usage.ofLimit', { limit: fmtBytes(row.usage!.mem_limit) }) }),
    ],
  })
}

/** Одна строка скорости: стрелка + значение. label — для подсказки и скринридера. */
function rateLine(icon: typeof ArrowDownOutline, value: number, label: string): VNodeChild {
  return h(NFlex, { align: 'center', size: 4, wrap: false, title: label, style: NOWRAP }, {
    default: () => [
      h(NIcon, { size: 14, 'aria-label': label }, { default: () => h(icon) }),
      fmtBps(value),
    ],
  })
}

/** Ячейка «Сеть»: приём и отдача в две строки. */
export function netCell(row: UsageRow, t: Translate): VNodeChild {
  if (!row.usage) return dash()
  return h(NFlex, { vertical: true, size: 0 }, {
    default: () => [
      rateLine(ArrowDownOutline, row.usage!.rx_bps, t('usage.rx')),
      rateLine(ArrowUpOutline, row.usage!.tx_bps, t('usage.tx')),
    ],
  })
}

/** Ячейка «Ноды»: первые hostname, остальные — «+N» с полным списком в подсказке. */
export function nodesCell(nodes: string[] | undefined): VNodeChild {
  if (!nodes || nodes.length === 0) return dash()
  // Каждое имя — своим блоком: голые строки подряд слипаются в одну.
  const line = (name: string) => h('div', { key: name, style: NOWRAP }, name)
  if (nodes.length <= MAX_NODES_SHOWN) {
    return h(NFlex, { vertical: true, size: 0 }, { default: () => nodes.map(line) })
  }
  const rest = nodes.length - MAX_NODES_SHOWN
  return h(NTooltip, null, {
    trigger: () =>
      h(NFlex, { vertical: true, size: 0 }, {
        default: () => [
          ...nodes.slice(0, MAX_NODES_SHOWN).map(line),
          h(NText, { depth: 3 }, { default: () => `+${rest}` }),
        ],
      }),
    default: () => nodes.join(', '),
  })
}

/**
 * Идёт ли обновление сервиса или оно зависло (ТЗ 3.4.5). Docker хранит
 * статус последнего обновления бессрочно: «completed» — давно законченное
 * обновление, а не текущее событие, значок для него только сбивает с толку.
 */
export function isUpdateNoteworthy(state: string | undefined | null): boolean {
  return !!state && state !== 'completed'
}

/**
 * Ячейка «имя сервиса + образ»: имя — ссылка (её отдаёт вызывающий экран),
 * образ — второстепенной строкой, коротко; полная ссылка на образ — в подсказке.
 */
export function nameWithImage(nameLink: VNodeChild, image: string): VNodeChild {
  return h(NFlex, { vertical: true, size: 0 }, {
    default: () => [
      nameLink,
      h(NText, { depth: 3, class: 'sub', title: image }, { default: () => shortImageName(image) }),
    ],
  })
}

/** Заголовок колонки с пояснением по наведению. */
function hinted(title: string, hint: string) {
  return () => h(NTooltip, null, {
    trigger: () => h('span', { tabindex: 0 }, title),
    default: () => hint,
  })
}

/** Сравнение для сортировки: строки без данных — в конец при убывании. */
function by(pick: (u: Usage) => number) {
  return (a: UsageRow, b: UsageRow) => (a.usage ? pick(a.usage) : -1) - (b.usage ? pick(b.usage) : -1)
}

/**
 * Колонки CPU / RAM / Сеть с сортировкой — «кто грузит кластер».
 * Подключаются в таблицы сервисов, задач и стеков.
 */
export function usageColumns<R extends UsageRow>(t: Translate): DataTableColumns<R> {
  return [
    {
      title: hinted('CPU', t('usage.cpuHint')), key: 'cpu', width: CPU_COL_WIDTH,
      sorter: by((u) => u.cpu_cores),
      render: (row) => cpuCell(row, t),
    },
    {
      title: 'RAM', key: 'mem', width: MEM_COL_WIDTH,
      sorter: by((u) => u.mem_used),
      render: (row) => memCell(row, t),
    },
    {
      title: hinted(t('usage.net'), t('usage.netHint')), key: 'net', width: NET_COL_WIDTH,
      sorter: by((u) => u.rx_bps + u.tx_bps),
      render: (row) => netCell(row, t),
    },
  ]
}
