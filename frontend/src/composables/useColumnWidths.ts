// Ширины колонок таблицы под текущую ширину контейнера.
//
// Следит за размером контейнера (ResizeObserver) и делит его ширину между
// колонками (см. utils/columns.ts). Экран подключает три вещи: ref на
// обёртку таблицы, widths — в описание колонок, minWidth — в scroll-x.
import { computed, onBeforeUnmount, onMounted, ref, toValue, type MaybeRefOrGetter } from 'vue'
import { distributeWidths, minTableWidth, type ColumnSpec } from '../utils/columns'

/**
 * @param specs колонки таблицы (можно реактивные: набор зависит от экрана)
 * @returns wrapRef — повесить на элемент-обёртку таблицы;
 *          widths — ширина каждой колонки по ключу, px;
 *          minWidth — сумма минимальных ширин (для scroll-x)
 */
export function useColumnWidths(specs: MaybeRefOrGetter<ColumnSpec[]>) {
  const wrapRef = ref<HTMLElement | null>(null)
  /** Ширина обёртки, px; 0 — ещё не измерена. */
  const available = ref(0)
  let observer: ResizeObserver | null = null

  onMounted(() => {
    const el = wrapRef.value
    if (!el) return
    available.value = el.clientWidth
    observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width
      if (width !== undefined) available.value = width
    })
    observer.observe(el)
  })
  onBeforeUnmount(() => observer?.disconnect())

  const widths = computed(() => distributeWidths(toValue(specs), available.value))
  const minWidth = computed(() => minTableWidth(toValue(specs)))
  return { wrapRef, widths, minWidth }
}
