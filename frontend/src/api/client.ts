// HTTP-клиент SPA: единая обёртка над fetch.
// Все мутирующие запросы несут X-CSRF-Token (FR-07 3.7.6);
// сессия — HttpOnly cookie, руками не трогаем.

/** CSRF-токен текущей сессии (выдаётся на /login и /me). */
let csrfToken = ''

/** Запомнить CSRF-токен после входа/восстановления сессии. */
export function setCsrf(token: string) {
  csrfToken = token
}

/** Ошибка API с HTTP-статусом и сообщением сервера. */
export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

/** Текущий CSRF-токен — для запросов в обход api() (скачивание файлов). */
export function csrf(): string {
  return csrfToken
}

/**
 * Скачать ответ эндпоинта как файл (резервная копия БД, NFR-10).
 * Отдельно от api(): тело здесь — бинарник, а не JSON, и имя файла
 * приходит в Content-Disposition.
 */
export async function download(path: string, fallbackName: string): Promise<void> {
  const res = await fetch(`/api/v1${path}`, {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
  })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new ApiError(res.status, data.error ?? res.statusText)
  }
  const name = fileNameFrom(res.headers.get('Content-Disposition')) ?? fallbackName
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
}

/** Достать имя файла из заголовка Content-Disposition. */
function fileNameFrom(header: string | null): string | undefined {
  const m = header?.match(/filename="([^"]+)"/)
  return m?.[1]
}

/** Выполнить запрос к /api/v1; JSON туда и обратно. */
export async function api<T>(path: string, options: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers: Record<string, string> = {}
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && method !== 'HEAD') headers['X-CSRF-Token'] = csrfToken

  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText)
  return data as T
}
