// Ячейки и колонки «потребление ресурсов» для таблиц сервисов, задач и
// стеков (ТЗ 3.4.7). Один код на все таблицы — цифры везде выглядят одинаково.
import { h, type VNodeChild } from 'vue'
import { NFlex, NIcon, NText, NTooltip } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { AlertCircleOutline, ArrowDownOutline, ArrowUpOutline } from '@vicons/ionicons5'
import type { NoData, Usage } from '../types'
import type { ColumnSpec } from './columns'
import { fmtBps, fmtBytes, fmtCores, shortImageName } from './format'

/** Функция перевода vue-i18n. */
type Translate = (key: string, params?: Record<string, unknown>) => string

/** Строка таблицы с потреблением. */
export interface UsageRow {
  usage?: Usage | null
  /** Сколько задач сейчас работает: нужно, чтобы заметить неполные данные. */
  running?: number
  /**
   * Почему метрик нет: у сервиса — список «нода и причина», у задачи — код
   * причины (нода задачи известна из её строки).
   */
  no_data?: NoData[] | string
  /** Hostname ноды — только у строки задачи. */
  node?: string
  /** Состояние — только у строки задачи: метрики есть лишь у running. */
  state?: string
}

/** Сколько hostname показывать в ячейке «Ноды»; остальные — в подсказке. */
const MAX_NODES_SHOWN = 2

/**
 * Колонки потребления: минимальная ширина, px, и доля лишней ширины.
 * Минимум подобран под самые длинные значения: «100.0%» со значком,
 * «1023 MiB» + «из 1023 MiB», «↓ 1023 MiB/s».
 */
export const USAGE_COLUMN_SPECS: ColumnSpec[] = [
  { key: 'cpu', base: 96, grow: 1 },
  { key: 'mem', base: 116, grow: 1 },
  { key: 'net', base: 132, grow: 1 },
]

/** Значение с единицей («из 256 MiB») не должно рваться между строками. */
const NOWRAP = 'white-space: nowrap'

/** Размер значка «данные неполные / данных нет», px. */
const NOTE_ICON_SIZE = 16

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

/** Задачи работают, а метрик нет ни по одной. */
function isMissing(row: UsageRow): boolean {
  if (row.usage) return false
  // Строка задачи: метрики ждём только от работающей.
  if (row.state !== undefined) return row.state === 'running'
  return (row.running ?? 0) > 0
}

/**
 * Причины «нет метрик» строками «нода: что случилось» — для подсказки.
 * Ответ на вопрос «почему прочерк»: по причине видно, что чинить.
 */
export function noDataLines(row: UsageRow, t: Translate): string[] {
  const reasonText = (reason: string) => t(`usage.noData.${reason}`)
  if (typeof row.no_data === 'string') {
    return row.no_data ? [`${row.node || '—'}: ${reasonText(row.no_data)}`] : []
  }
  return (row.no_data ?? []).map((n) => `${n.node}: ${reasonText(n.reason)}`)
}

/** Подсказка в несколько строк: заголовок и причины по нодам. */
function noteTooltip(trigger: () => VNodeChild, title: string, lines: string[]): VNodeChild {
  return h(NTooltip, { style: 'max-width: 420px' }, {
    trigger,
    default: () => [h('div', null, title), ...lines.map((line) => h('div', { key: line }, line))],
  })
}

/** Значение со значком-пояснением (данные неполные или их нет). */
function noted(value: VNodeChild, title: string, lines: string[]): VNodeChild {
  return noteTooltip(
    () =>
      // tabindex: подсказка открывается и с клавиатуры, не только мышью.
      h(NFlex, { align: 'center', size: 4, wrap: false, inline: true, style: NOWRAP, tabindex: 0 }, {
        default: () => [
          value,
          h(NIcon, { size: NOTE_ICON_SIZE, 'aria-label': title }, { default: () => h(AlertCircleOutline) }),
        ],
      }),
    title,
    lines,
  )
}

/**
 * Прочерк для ячейки без данных. Если задачи работают, а метрик нет —
 * с подсказкой о причине; значок показывает только колонка CPU (withIcon),
 * чтобы не повторять его три раза в строке.
 */
function noDataCell(row: UsageRow, t: Translate, withIcon: boolean): VNodeChild {
  if (!isMissing(row)) return dash()
  const title = t('usage.noData.title')
  const lines = noDataLines(row, t)
  if (withIcon) return noted(dash(), title, lines)
  return noteTooltip(() => h('span', { tabindex: 0 }, [dash()]), title, lines)
}

/** Ячейка CPU: проценты одного ядра + значок, если данные неполные. */
export function cpuCell(row: UsageRow, t: Translate): VNodeChild {
  if (!row.usage) return noDataCell(row, t, true)
  const value = fmtCores(row.usage.cpu_cores)
  if (!isPartial(row)) return value
  return noted(value, t('usage.partial', { got: row.usage.tasks, total: row.running }), noDataLines(row, t))
}

/** Ячейка RAM: занято, второй строкой — лимит (если он есть). */
export function memCell(row: UsageRow, t: Translate): VNodeChild {
  if (!row.usage) return noDataCell(row, t, false)
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
  if (!row.usage) return noDataCell(row, t, false)
  return h(NFlex, { vertical: true, size: 0 }, {
    default: () => [
      rateLine(ArrowDownOutline, row.usage!.rx_bps, t('usage.rx')),
      rateLine(ArrowUpOutline, row.usage!.tx_bps, t('usage.tx')),
    ],
  })
}

/**
 * Ячейка «Ноды»: не больше двух строк, чтобы строка таблицы не меняла
 * высоту. Нод больше двух — первая и «ещё N», полный список — в подсказке.
 */
export function nodesCell(nodes: string[] | undefined, t: Translate): VNodeChild {
  if (!nodes || nodes.length === 0) return dash()
  // Каждое имя — своим блоком: голые строки подряд слипаются в одну.
  const line = (name: string) => h('div', { key: name, class: 'one-line', title: name }, name)
  if (nodes.length <= MAX_NODES_SHOWN) {
    return h('div', null, nodes.map(line))
  }
  const rest = nodes.length - 1
  return h(NTooltip, null, {
    trigger: () =>
      h('div', { tabindex: 0 }, [
        line(nodes[0]),
        h(NText, { depth: 3 }, { default: () => t('usage.moreNodes', { count: rest }) }),
      ]),
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
  return h('div', null, [
    h('div', { class: 'one-line' }, [nameLink]),
    h(NText, { depth: 3, class: 'sub one-line', title: image }, { default: () => shortImageName(image) }),
  ])
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
 *
 * @param widths ширины колонок по ключам cpu / mem / net, px
 *               (из useColumnWidths с USAGE_COLUMN_SPECS)
 */
export function usageColumns<R extends UsageRow>(t: Translate, widths: Record<string, number>): DataTableColumns<R> {
  return [
    {
      title: hinted('CPU', t('usage.cpuHint')), key: 'cpu', width: widths.cpu,
      sorter: by((u) => u.cpu_cores),
      render: (row) => cpuCell(row, t),
    },
    {
      title: hinted('RAM', t('usage.memHint')), key: 'mem', width: widths.mem,
      sorter: by((u) => u.mem_used),
      render: (row) => memCell(row, t),
    },
    {
      title: hinted(t('usage.net'), t('usage.netHint')), key: 'net', width: widths.net,
      sorter: by((u) => u.rx_bps + u.tx_bps),
      render: (row) => netCell(row, t),
    },
  ]
}
