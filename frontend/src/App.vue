<script setup lang="ts">
// Корневой компонент: провайдеры Naive UI + тема.
// Тёмная тема по умолчанию, светлая — переключателем (разд. 6.1 ТЗ).
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme } from 'naive-ui'
import { useThemeStore } from './stores/theme'

const theme = useThemeStore()
</script>

<template>
  <n-config-provider :theme="theme.isDark ? darkTheme : null">
    <n-message-provider>
      <n-dialog-provider>
        <router-view />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style>
/* Базовый сброс: приложение занимает весь экран */
html, body, #app {
  margin: 0;
  height: 100%;
}
</style>
