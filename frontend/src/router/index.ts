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
    { path: '/logs', name: 'logs', component: () => import('../views/LogsView.vue') },
    { path: '/events', name: 'events', component: () => import('../views/EventsView.vue') },
    { path: '/resources', name: 'resources', component: () => import('../views/ResourcesView.vue') },
    { path: '/disk', name: 'disk', component: () => import('../views/DiskUsageView.vue') },
    { path: '/alerts', name: 'alerts', component: () => import('../views/AlertsView.vue') },
    { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
  ],
})

// Гард: без сессии — на /login. Сессия восстанавливается один раз при старте.
router.beforeEach(async (to) => {
  if (to.name === 'login' || to.name === 'setup') return true
  const auth = useAuthStore()
  if (!auth.checked) await auth.restore()
  if (!auth.username) return { name: 'login' }
  return true
})
