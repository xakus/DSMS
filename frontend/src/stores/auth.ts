// Pinia-store аутентификации: login/logout/setup + восстановление сессии.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, ApiError, setCsrf } from '../api/client'

/** HTTP-статус «сессии нет». */
const HTTP_UNAUTHORIZED = 401

/**
 * Вход прошёл, но сессия не держится: браузер не сохранил cookie.
 * Cookie сессии помечена Secure, и по http:// браузер её отбрасывает —
 * раньше это выглядело как молчаливый возврат на форму входа.
 */
export class SessionNotKeptError extends Error {}

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

  /**
   * Войти по логину/паролю.
   *
   * После входа проверяет, что сессия действительно держится (запрос /me с
   * только что выданной cookie).
   *
   * @throws ApiError            сервер отказал (401 — неверные данные,
   *                             429 — блокировка, 5xx — сбой панели)
   * @throws SessionNotKeptError вход принят, но cookie не сохранилась
   */
  async function login(user: string, password: string) {
    apply(await api<MeResponse>('/login', { method: 'POST', body: { username: user, password } }))
    try {
      await api<MeResponse>('/me')
    } catch (e) {
      if (e instanceof ApiError && e.status === HTTP_UNAUTHORIZED) {
        username.value = ''
        role.value = ''
        throw new SessionNotKeptError()
      }
      throw e
    }
    checked.value = true
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
