<script setup lang="ts">
// Каркас авторизованной части: сайдбар-навигация + шапка
// (тема, язык, logout). Экраны рендерятся в слот.
import { computed, h, ref, type Component } from 'vue'
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
import { useUiStore } from '../stores/ui'
import { wsClient } from '../api/ws'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const message = useMessage()
const auth = useAuthStore()
const theme = useThemeStore()
const metrics = useMetricsStore()
const alerts = useAlertsStore()
const ui = useUiStore()

// На телефоне сайдбар свёрнут по умолчанию — иначе он съедает ширину и шапка
// (колокольчик слева) уезжает за край экрана. Открыть можно триггером-баром.
const isMobile = ref(typeof window !== 'undefined' && window.innerWidth < 768)

// Версия сборки фронта (из package.json через Vite define) — для угла шапки.
const appVersion = __APP_VERSION__

// Колокольчик (3.12.2): подписка на алерты + тосты на новые.
onMounted(() => {
  // Синхронизировать интервал графиков с серверной настройкой (её видят агенты).
  ui.loadFromServer()
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
    <n-layout-sider bordered collapse-mode="width" :width="220" :collapsed-width="0"
                    :default-collapsed="isMobile" show-trigger="bar">
      <div class="logo">
        <img class="logo-img" src="/logo.png" alt="DSMS" />
        <div class="logo-text">
          <span class="logo-name">DSMS</span>
          <span class="logo-full">Docker Swarm Management System</span>
        </div>
      </div>
      <n-menu :options="menu" :value="String(route.name)" />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="header">
        <!-- Версия в левом углу шапки — видно, какой билд реально загружен. -->
        <span class="app-version" title="Версия интерфейса">v{{ appVersion }}</span>
        <n-space justify="end" align="center" :wrap="true" class="topbar">
          <!-- Колокольчик активных алертов (FR-12 3.12.2). Стоит первым, но на
               телефоне флекс-обёртка не даёт ему уехать за край (см. .topbar). -->
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
            {{ t('dashboard.logout') }}<span class="uname"> ({{ auth.username }})</span>
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
  padding: 16px 16px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.logo-img {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  flex: 0 0 auto;
}
.logo-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.logo-name {
  font-weight: 700;
  font-size: 20px;
  letter-spacing: 0.5px;
  line-height: 1.1;
}
.logo-full {
  font-size: 14px;
  line-height: 1.3;
  opacity: 0.55;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.header {
  padding: 8px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
/* Версия в левом углу шапки — маленькая и приглушённая, не мешает. */
.app-version {
  font-size: 12px;
  opacity: 0.55;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  flex: 0 0 auto;
}
/* Контролы шапки переносятся, а не обрезаются — на узком экране колокольчик
   гарантированно виден (раньше он, будучи левым при justify=end, уезжал за край). */
.topbar {
  row-gap: 6px;
}
.content {
  padding: 20px 28px;
}
.lang {
  width: 72px;
}
/* Телефон: прячем имя пользователя (кнопка «Выход» была самой широкой и
   выдавливала колокольчик), сужаем контент, компактнее селект языка. */
@media (max-width: 640px) {
  .uname {
    display: none;
  }
  .lang {
    width: 60px;
  }
  .content {
    padding: 14px 12px;
  }
  .header {
    padding: 8px 12px;
  }
}
</style>
