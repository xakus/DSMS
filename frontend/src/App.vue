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
    // Насыщенный светло-серый фон + чисто-белые карточки = чёткий контраст.
    bodyColor: '#e6e8ee',
    cardColor: '#ffffff',
    modalColor: '#ffffff',
    popoverColor: '#ffffff',
    tableColor: '#ffffff',
    // Заметные границы и разделители — раньше сливались.
    borderColor: '#d3d7e0',
    dividerColor: '#dcdfe7',
    // Индиго-акцент хорошо сочетается с нейтральными серыми.
    primaryColor: '#3b5bdb',
    primaryColorHover: '#4c6ef5',
    primaryColorPressed: '#364fc7',
    primaryColorSuppl: '#4c6ef5',
    borderRadius: '10px',
    // Контрастная типографика (три уровня).
    textColorBase: '#171a21',
    textColor1: '#171a21',
    textColor2: '#3b414f',
    textColor3: '#6b7280',
  },
  Card: { borderColor: '#e2e5ec' },
  Layout: { siderColor: '#eef0f4', headerColor: '#eef0f4' },
  ...tableOverrides,
}
// Тёмная тема — только акцент и радиус поверх штатного darkTheme Naive
// (он уже даёт хорошие тёмные поверхности; лишние переопределения убраны,
// чтобы не появлялось «белесой пелены»).
const darkOverrides: GlobalThemeOverrides = {
  common: {
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
  background: #e6e8ee;
}
:root[data-theme='dark'] {
  background: #101014;
}

/* В светлой теме карточки «всплывают» над серым фоном за счёт мягкой тени —
   иначе белое на светло-сером почти сливается. */
:root[data-theme='light'] .n-card {
  box-shadow: 0 1px 2px rgba(20, 23, 33, 0.05), 0 4px 12px rgba(20, 23, 33, 0.05);
}
</style>
