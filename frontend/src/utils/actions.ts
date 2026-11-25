// Хелпер для колонок действий в таблицах: кнопка-иконка с тултипом.
// Заменяет голые эмодзи нормальными SVG-иконками (@vicons) —
// единый стиль в духе Google/Apple, с подсказкой при наведении.
import { h, type Component } from 'vue'
import { NButton, NDropdown, NTooltip, NIcon, NSpace } from 'naive-ui'
import { EllipsisHorizontal } from '@vicons/ionicons5'

/** Одно действие строки таблицы. */
export interface RowAction {
  /** Иконка-компонент из @vicons/ionicons5. */
  icon: Component
  /** Текст подсказки (переведённый). */
  tip: string
  /** Обработчик клика. */
  onClick: () => void
  /** Цветовой акцент кнопки. */
  type?: 'default' | 'primary' | 'success' | 'warning' | 'error'
  /** Неактивна. */
  disabled?: boolean
}

/** Настройки группы действий. */
export interface RowActionsOptions {
  /**
   * Сколько кнопок помещается в колонку. Если действий больше, последняя
   * кнопка — меню «ещё» с остальными. Не задано — показываются все.
   */
  maxVisible?: number
  /** Подпись кнопки-меню «ещё» (переведённая) — обязательна при maxVisible. */
  moreLabel?: string
}

/** Размер иконки в кнопке действия, px. */
const ICON_SIZE = 18

/** Кнопка-иконка с тултипом. */
export function iconButton(a: RowAction) {
  return h(
    NTooltip,
    { delay: 300 },
    {
      trigger: () =>
        h(
          NButton,
          {
            size: 'small',
            circle: true,
            tertiary: true,
            type: a.type ?? 'default',
            disabled: a.disabled,
            // Кнопка без текста: без aria-label скринридер прочитает «кнопка».
            'aria-label': a.tip,
            onClick: a.onClick,
          },
          { default: () => h(NIcon, { size: ICON_SIZE }, { default: () => h(a.icon) }) },
        ),
      default: () => a.tip,
    },
  )
}

/** Кнопка «ещё»: выпадающее меню с действиями, не поместившимися в строку. */
function moreButton(actions: RowAction[], label: string) {
  return h(
    NDropdown,
    {
      trigger: 'click',
      options: actions.map((a, i) => ({
        key: i,
        label: a.tip,
        disabled: a.disabled,
        icon: () => h(NIcon, null, { default: () => h(a.icon) }),
      })),
      onSelect: (key: number) => actions[key].onClick(),
    },
    {
      default: () =>
        h(
          NButton,
          { size: 'small', circle: true, tertiary: true, 'aria-label': label, title: label },
          { default: () => h(NIcon, { size: ICON_SIZE }, { default: () => h(EllipsisHorizontal) }) },
        ),
    },
  )
}

/**
 * Горизонтальная группа кнопок-действий (не переносится).
 * При opts.maxVisible лишние действия уходят в меню «ещё»: первые
 * maxVisible − 1 остаются кнопками, поэтому частые действия ставь первыми.
 */
export function rowActions(actions: RowAction[], opts: RowActionsOptions = {}) {
  const max = opts.maxVisible ?? actions.length
  const nodes =
    actions.length <= max
      ? actions.map(iconButton)
      : [
          ...actions.slice(0, max - 1).map(iconButton),
          moreButton(actions.slice(max - 1), opts.moreLabel ?? ''),
        ]
  return h(NSpace, { size: 6, wrap: false, align: 'center' }, { default: () => nodes })
}
