// Роутер SPA: login/setup — публичные, остальное под гардом сессии.
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('../views/LoginView.vue') },
    { path: '/setup', name: 'setup', component: () => import('../views/SetupView.vue') },
    { path: '/', name: 'dashboard', component: () => import('../views/DashboardView.vue') },
    // Остальные экраны (nodes, services, stacks, ...) — этапы 2+.
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
