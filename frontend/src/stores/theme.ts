// Pinia-store темы: тёмная по умолчанию (разд. 6.1 ТЗ), выбор сохраняется.
import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useThemeStore = defineStore('theme', () => {
  /** Тёмная тема включена (по умолчанию — да). */
  const isDark = ref(localStorage.getItem('dsms.theme') !== 'light')

  /** Переключить тему и запомнить выбор. */
  function toggle() {
    isDark.value = !isDark.value
    localStorage.setItem('dsms.theme', isDark.value ? 'dark' : 'light')
  }

  return { isDark, toggle }
})
