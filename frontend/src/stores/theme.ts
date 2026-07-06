// Pinia-store темы: тёмная по умолчанию (разд. 6.1 ТЗ), выбор сохраняется.
// Пишет data-theme на <html> — чтобы фон body совпадал с темой Naive UI
// (иначе просветы вокруг layout остаются белыми).
import { defineStore } from 'pinia'
import { ref, watchEffect } from 'vue'

export const useThemeStore = defineStore('theme', () => {
  /** Тёмная тема включена (по умолчанию — да). */
  const isDark = ref(localStorage.getItem('dsms.theme') !== 'light')

  // Синхронизируем атрибут на <html> с текущей темой.
  watchEffect(() => {
    document.documentElement.dataset.theme = isDark.value ? 'dark' : 'light'
  })

  /** Переключить тему и запомнить выбор. */
  function toggle() {
    isDark.value = !isDark.value
    localStorage.setItem('dsms.theme', isDark.value ? 'dark' : 'light')
  }

  return { isDark, toggle }
})
