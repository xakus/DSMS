// Роутер SPA: login/setup — публичные, остальное под гардом сессии.
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('../views/LoginView.vue') },
    { path: '/setup', name: 'setup', component: () => import('../views/SetupView.vue') },
    { path: '/', name: 'dashboard', component: () => import('../views/DashboardView.vue') },
    { path: '/nodes/:id', name: 'node', component: () => import('../views/NodeDetailView.vue') },
    { path: '/services', name: 'services', component: () => import('../views/ServicesView.vue') },
    { path: '/services/:id', name: 'service', component: () => import('../views/ServiceDetailView.vue') },
    { path: '/stacks', name: 'stacks', component: () => import('../views/StacksView.vue') },
    { path: '/stacks/:name', name: 'stack', component: () => import('../views/StackDetailView.vue') },
    { path: '/logs', name: 'logs', component: () => import('../views/LogsView.vue') },
    { path: '/events', name: 'events', component: () => import('../views/EventsView.vue') },
    { path: '/resources', name: 'resources', component: () => import('../views/ResourcesView.vue') },
    { path: '/disk', name: 'disk', component: () => import('../views/DiskUsageView.vue') },
    { path: '/alerts', name: 'alerts', component: () => import('../views/AlertsView.vue') },
    { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
    // Catch-all: любой неизвестный путь (битая ссылка/уведомление, старый URL)
    // не даёт пустой экран/404 — уводим на дашборд вместо тупика.
    { path: '/:pathMatch(.*)*', name: 'notfound', redirect: { name: 'dashboard' } },
  ],
})

/** Ключ sessionStorage: когда страницу последний раз перезагружали из-за старой сборки. */
const RELOAD_MARK_KEY = 'dsms.staleReloadAt'

/** Повторная перезагрузка не чаще этого, мс: иначе при настоящей поломке был бы бесконечный цикл. */
const RELOAD_GUARD_MS = 10_000

/** Так браузеры сообщают, что файл экрана не загрузился (Chrome, Firefox, Safari). */
const CHUNK_LOAD_ERROR = /dynamically imported module|Importing a module script failed|Unable to preload CSS/i

// После обновления панели во вкладке остаётся старый интерфейс: он просит
// файлы экранов со старыми именами, которых на сервере уже нет. Раньше
// переход (например, на «Обзор» после входа) молча не происходил. Теперь
// страница перезагружается и получает свежую сборку.
router.onError((err: unknown, to) => {
  if (!(err instanceof Error) || !CHUNK_LOAD_ERROR.test(err.message)) return
  const last = Number(sessionStorage.getItem(RELOAD_MARK_KEY))
  if (Number.isFinite(last) && Date.now() - last < RELOAD_GUARD_MS) return
  sessionStorage.setItem(RELOAD_MARK_KEY, String(Date.now()))
  window.location.assign(to.fullPath)
})

// Гард: без сессии — на /login. Сессия восстанавливается один раз при старте.
router.beforeEach(async (to) => {
  if (to.name === 'login' || to.name === 'setup') return true
  const auth = useAuthStore()
  if (!auth.checked) await auth.restore()
  if (!auth.username) return { name: 'login' }
  return true
})
