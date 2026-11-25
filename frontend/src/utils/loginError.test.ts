import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client'
import { SessionNotKeptError } from '../stores/auth'
import { loginErrorText } from './loginError'

describe('loginErrorText', () => {
  it('401 — неверное имя или пароль', () => {
    expect(loginErrorText(new ApiError(401, 'invalid credentials'))).toEqual({ key: 'login.failed' })
  })

  it('429 — блокировка: время ожидания из Retry-After, округление вверх', () => {
    // 541 с = 9 мин 1 с → «подождите 10 минут».
    expect(loginErrorText(new ApiError(429, 'too many attempts', 541)))
      .toEqual({ key: 'login.locked', params: { minutes: 10 } })
  })

  it('429 без Retry-After (панель старой версии) — 15 минут', () => {
    expect(loginErrorText(new ApiError(429, 'too many attempts')))
      .toEqual({ key: 'login.locked', params: { minutes: 15 } })
  })

  it('502 от прокси — панель не отвечает, а не «неверный пароль»', () => {
    expect(loginErrorText(new ApiError(502, 'Bad Gateway')))
      .toEqual({ key: 'login.serverError', params: { status: 502 } })
  })

  it('сбой сети — панель недоступна', () => {
    expect(loginErrorText(new TypeError('Failed to fetch'))).toEqual({ key: 'login.unreachable' })
  })

  it('вход принят, но cookie не сохранилась (панель открыта по http)', () => {
    expect(loginErrorText(new SessionNotKeptError())).toEqual({ key: 'login.cookieLost' })
  })
})
