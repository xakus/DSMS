<script setup lang="ts">
// Корневой компонент: провайдеры Naive UI + тема.
// Тёмная тема по умолчанию, светлая — переключателем (разд. 6.1 ТЗ).
import { computed } from 'vue'
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme } from 'naive-ui'
import type { GlobalThemeOverrides } from 'naive-ui'
import { useThemeStore } from './stores/theme'

const theme = useThemeStore()

// Переопределения общих токенов — единый акцент и мягкий фон в обеих темах.
// Светлая была слишком белой/неконтрастной, поэтому фон — тёплый серый,
// карточки остаются белыми и «всплывают» за счёт контраста.
// Более просторные ячейки таблиц — иначе текст в соседних колонках слипается.
const tableOverrides = {
  DataTable: {
    thPaddingSmall: '10px 16px',
    tdPaddingSmall: '10px 16px',
    thPadding: '12px 16px',
    tdPadding: '12px 16px',
    fontSizeSmall: '13px',
  },
}
const lightOverrides: GlobalThemeOverrides = {
  common: {
    bodyColor: '#f2f3f5',
    cardColor: '#ffffff',
    modalColor: '#ffffff',
    popoverColor: '#ffffff',
    primaryColor: '#2563eb',
    primaryColorHover: '#3b82f6',
    primaryColorPressed: '#1d4ed8',
    primaryColorSuppl: '#3b82f6',
    borderRadius: '8px',
    textColorBase: '#1f2329',
  },
  ...tableOverrides,
}
const darkOverrides: GlobalThemeOverrides = {
  common: {
    bodyColor: '#101014',
    primaryColor: '#5b8cff',
    primaryColorHover: '#78a2ff',
    primaryColorPressed: '#3d72f0',
    primaryColorSuppl: '#78a2ff',
    borderRadius: '8px',
  },
  ...tableOverrides,
}

const overrides = computed(() => (theme.isDark ? darkOverrides : lightOverrides))
</script>

<template>
  <n-config-provider :theme="theme.isDark ? darkTheme : null" :theme-overrides="overrides">
    <n-message-provider>
      <n-dialog-provider>
        <router-view />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style>
/* Базовый сброс: приложение занимает весь экран.
   Фон синхронизирован с темой через data-theme на <html> (см. stores/theme). */
html, body, #app {
  margin: 0;
  height: 100%;
}
:root,
:root[data-theme='light'] {
  background: #f2f3f5;
}
:root[data-theme='dark'] {
  background: #101014;
}
</style>
