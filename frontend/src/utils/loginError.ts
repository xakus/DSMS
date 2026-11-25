// Причина неудачного входа — словами, чтобы было понятно, что делать.
//
// Раньше экран входа на любую ошибку отвечал «Неверные учётные данные»:
// и на неверный пароль, и на блокировку после пяти попыток, и на
// недоступную панель. По такой надписи причину не найти.
import { ApiError } from '../api/client'
import { SessionNotKeptError } from '../stores/auth'

/** Ключ перевода и параметры сообщения об ошибке входа. */
export interface LoginErrorText {
  key: string
  params?: Record<string, unknown>
}

/** HTTP-статусы, которые экран входа объясняет отдельно. */
const HTTP_UNAUTHORIZED = 401
const HTTP_TOO_MANY_REQUESTS = 429

/**
 * Сколько минут длится блокировка входа, если сервер не прислал Retry-After
 * (панель старой версии). Совпадает с окном учёта попыток на сервере.
 */
const DEFAULT_LOCK_MINUTES = 15

const SECONDS_PER_MINUTE = 60

/**
 * Переводит ошибку входа в ключ сообщения.
 *
 * @param e что бросил вход: ApiError, SessionNotKeptError или сбой сети
 * @returns ключ i18n и параметры: для блокировки — minutes (сколько ждать,
 *          округлено вверх), для сбоя сервера — status
 */
export function loginErrorText(e: unknown): LoginErrorText {
  if (e instanceof SessionNotKeptError) return { key: 'login.cookieLost' }
  if (!(e instanceof ApiError)) return { key: 'login.unreachable' }
  if (e.status === HTTP_UNAUTHORIZED) return { key: 'login.failed' }
  if (e.status === HTTP_TOO_MANY_REQUESTS) {
    const minutes = e.retryAfterSec ? Math.ceil(e.retryAfterSec / SECONDS_PER_MINUTE) : DEFAULT_LOCK_MINUTES
    return { key: 'login.locked', params: { minutes } }
  }
  return { key: 'login.serverError', params: { status: e.status } }
}
