<script setup lang="ts">
// Каркас авторизованной части: сайдбар-навигация + шапка
// (тема, язык, logout). Экраны рендерятся в слот.
import { computed, h, type Component } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { onMounted } from 'vue'
import {
  NLayout, NLayoutSider, NLayoutHeader, NLayoutContent,
  NMenu, NButton, NSpace, NSelect, NBadge, NIcon, NTooltip, useMessage,
} from 'naive-ui'
import type { MenuOption } from 'naive-ui'
import {
  SpeedometerOutline, CubeOutline, LayersOutline, DocumentTextOutline,
  PulseOutline, KeyOutline, ServerOutline, WarningOutline, SettingsOutline,
} from '@vicons/ionicons5'
import { useAuthStore } from '../stores/auth'
import { useThemeStore } from '../stores/theme'
import { useMetricsStore } from '../stores/metrics'
import { useAlertsStore } from '../stores/alerts'
import { wsClient } from '../api/ws'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const message = useMessage()
const auth = useAuthStore()
const theme = useThemeStore()
const metrics = useMetricsStore()
const alerts = useAlertsStore()

// Колокольчик (3.12.2): подписка на алерты + тосты на новые.
onMounted(() => {
  alerts.load().catch(() => {})
  alerts.start((a) => {
    const text = `${a.rule}: ${a.message}`
    if (a.severity === 'critical') message.error(text, { duration: 8000 })
    else message.warning(text, { duration: 5000 })
  })
})

/** Собрать пункт меню: иконка + подпись-ссылка с тултипом-описанием. */
function item(key: string, icon: Component): MenuOption {
  return {
    key,
    icon: () => h(NIcon, null, { default: () => h(icon) }),
    // Тултип справа объясняет, что за раздел (описания в i18n navDesc.*).
    label: () =>
      h(
        NTooltip,
        { placement: 'right', delay: 400 },
        {
          trigger: () => h(RouterLink, { to: { name: key } }, { default: () => t(`nav.${key}`) }),
          default: () => t(`navDesc.${key}`),
        },
      ),
  }
}

const menu = computed<MenuOption[]>(() => [
  item('dashboard', SpeedometerOutline),
  item('services', CubeOutline),
  item('stacks', LayersOutline),
  item('logs', DocumentTextOutline),
  item('events', PulseOutline),
  item('resources', KeyOutline),
  item('disk', ServerOutline),
  item('alerts', WarningOutline),
  item('settings', SettingsOutline),
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
  alerts.stop()
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
          <!-- Колокольчик активных алертов (FR-12 3.12.2) -->
          <n-badge :value="alerts.active.length" :max="99" :show="alerts.active.length > 0">
            <n-button quaternary size="small" @click="router.push({ name: 'alerts' })">🔔</n-button>
          </n-badge>
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
