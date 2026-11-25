// Реактивная проверка CSS media query (например, «экран шире 1024px»).
import { onBeforeUnmount, onMounted, ref } from 'vue'

/**
 * @param query строка media query, например '(min-width: 1024px)'
 * @returns ref: true, пока условие выполняется; обновляется при ресайзе окна
 */
export function useMediaQuery(query: string) {
  const mql = typeof window !== 'undefined' ? window.matchMedia(query) : null
  const matches = ref(mql?.matches ?? false)
  const update = () => { matches.value = mql?.matches ?? false }

  onMounted(() => mql?.addEventListener('change', update))
  onBeforeUnmount(() => mql?.removeEventListener('change', update))
  return matches
}
