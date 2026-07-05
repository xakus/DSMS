// Точка входа SPA: Vue 3 + Pinia + Router + i18n (разд. 2.4, 6.1 ТЗ).
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)
app.mount('#app')
