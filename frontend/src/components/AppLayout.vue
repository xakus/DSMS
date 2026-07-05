<script setup lang="ts">
// Каркас авторизованной части: сайдбар-навигация + шапка
// (тема, язык, logout). Экраны рендерятся в слот.
import { computed, h } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NLayout, NLayoutSider, NLayoutHeader, NLayoutContent,
  NMenu, NButton, NSpace, NSelect,
} from 'naive-ui'
import type { MenuOption } from 'naive-ui'
import { useAuthStore } from '../stores/auth'
import { useThemeStore } from '../stores/theme'
import { useMetricsStore } from '../stores/metrics'
import { wsClient } from '../api/ws'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const auth = useAuthStore()
const theme = useThemeStore()
const metrics = useMetricsStore()

// Пункты меню. Экраны этапов 4+ добавляются сюда по мере реализации.
const menu = computed<MenuOption[]>(() => [
  { label: () => h(RouterLink, { to: { name: 'dashboard' } }, { default: () => t('nav.dashboard') }), key: 'dashboard' },
  { label: () => h(RouterLink, { to: { name: 'services' } }, { default: () => t('nav.services') }), key: 'services' },
  { label: () => h(RouterLink, { to: { name: 'stacks' } }, { default: () => t('nav.stacks') }), key: 'stacks' },
  { label: () => h(RouterLink, { to: { name: 'logs' } }, { default: () => t('nav.logs') }), key: 'logs' },
  { label: () => h(RouterLink, { to: { name: 'events' } }, { default: () => t('nav.events') }), key: 'events' },
  { label: () => h(RouterLink, { to: { name: 'resources' } }, { default: () => t('nav.resources') }), key: 'resources' },
  { label: () => h(RouterLink, { to: { name: 'disk' } }, { default: () => t('nav.disk') }), key: 'disk' },
  { label: () => h(RouterLink, { to: { name: 'settings' } }, { default: () => t('nav.settings') }), key: 'settings' },
])

/** Языки интерфейса (разд. 6.1: EN базовый, RU; AZ добавится тривиально). */
const locales = [
  { label: 'EN', value: 'en' },
  { label: 'RU', value: 'ru' },
]

/** Сменить язык и запомнить выбор. */
function setLocale(v: string) {
  locale.value = v
  localStorage.setItem('dsms.locale', v)
}

/** Выход: гасим WS и метрики, чистим сессию. */
async function logout() {
  metrics.stop()
  wsClient.close()
  await auth.logout()
  router.replace({ name: 'login' })
}
</script>

<template>
  <n-layout has-sider class="app-root">
    <n-layout-sider bordered collapse-mode="width" :width="200" :collapsed-width="0" show-trigger="bar">
      <div class="logo">DSMS</div>
      <n-menu :options="menu" :value="String(route.name)" />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="header">
        <n-space justify="end" align="center">
          <n-select
            :value="locale" :options="locales" size="small" class="lang"
            @update:value="setLocale"
          />
          <n-button quaternary size="small" @click="theme.toggle()">
            {{ theme.isDark ? '🌙' : '☀️' }}
          </n-button>
          <n-button quaternary size="small" @click="logout">
            {{ t('dashboard.logout') }} ({{ auth.username }})
          </n-button>
        </n-space>
      </n-layout-header>
      <n-layout-content class="content">
        <slot />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<style scoped>
/* Каркас на весь экран, контент со скроллом */
.app-root {
  height: 100vh;
}
.logo {
  font-weight: 700;
  font-size: 18px;
  padding: 16px;
}
.header {
  padding: 8px 16px;
}
.content {
  padding: 16px 24px;
}
.lang {
  width: 72px;
}
</style>
