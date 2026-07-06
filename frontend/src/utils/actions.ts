// Хелпер для колонок действий в таблицах: кнопка-иконка с тултипом.
// Заменяет голые эмодзи нормальными SVG-иконками (@vicons) —
// единый стиль в духе Google/Apple, с подсказкой при наведении.
import { h, type Component } from 'vue'
import { NButton, NTooltip, NIcon, NSpace } from 'naive-ui'

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
            onClick: a.onClick,
          },
          { default: () => h(NIcon, { size: 18 }, { default: () => h(a.icon) }) },
        ),
      default: () => a.tip,
    },
  )
}

/** Горизонтальная группа кнопок-действий (не переносится). */
export function rowActions(actions: RowAction[]) {
  return h(NSpace, { size: 6, wrap: false, align: 'center' }, { default: () => actions.map(iconButton) })
}
