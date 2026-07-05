// Pinia-store аутентификации: login/logout/setup + восстановление сессии.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, setCsrf } from '../api/client'

/** Ответ сервера на /login и /me. */
interface MeResponse {
  username: string
  role: string
  csrf: string
}

export const useAuthStore = defineStore('auth', () => {
  /** Имя вошедшего пользователя; пустое — не авторизован. */
  const username = ref('')
  /** Роль (в v1 всегда 'admin', задел под RBAC). */
  const role = ref('')
  /** Сессия проверена на сервере (гард роутера ждёт эту отметку). */
  const checked = ref(false)

  /** Применить данные сессии из ответа сервера. */
  function apply(me: MeResponse) {
    username.value = me.username
    role.value = me.role
    setCsrf(me.csrf)
  }

  /** Восстановить сессию по cookie при загрузке SPA. */
  async function restore() {
    try {
      apply(await api<MeResponse>('/me'))
    } catch {
      username.value = ''
    } finally {
      checked.value = true
    }
  }

  /** Войти по логину/паролю. */
  async function login(user: string, password: string) {
    apply(await api<MeResponse>('/login', { method: 'POST', body: { username: user, password } }))
  }

  /** Выйти и сбросить состояние. */
  async function logout() {
    await api('/logout', { method: 'POST' })
    username.value = ''
    role.value = ''
    setCsrf('')
  }

  return { username, role, checked, restore, login, logout }
})
