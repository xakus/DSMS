// WebSocket-клиент /api/v1/ws (разд. 4.2 ТЗ):
// один мультиплексированный канал, протокол sub/unsub,
// автопереподключение с восстановлением всех подписок.

/** Подписка: топик + опциональный объект (сервис для логов). */
export interface Sub {
  topic: string
  service?: string
  tail?: number
}

/** Входящее сообщение сервера: топик + произвольные поля данных. */
export interface WsMessage {
  topic: string
  [key: string]: unknown
}

type Handler = (msg: WsMessage) => void

/** Ключ подписки для реестра обработчиков. */
function keyOf(topic: string, service?: string): string {
  return service ? `${topic}:${service}` : topic
}

/**
 * WsClient — синглтон соединения панели.
 * Подписчики регистрируются через subscribe(); при обрыве соединение
 * восстанавливается с экспоненциальной задержкой и повторной подпиской.
 */
class WsClient {
  private ws: WebSocket | null = null
  private handlers = new Map<string, Set<Handler>>()
  private subs = new Map<string, Sub>()
  private retryMs = 1000 // текущая задержка переподключения
  private closedByUser = false

  /** Открыть соединение (идемпотентно). */
  connect() {
    if (this.ws && this.ws.readyState <= WebSocket.OPEN) return
    this.closedByUser = false
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    this.ws = new WebSocket(`${proto}://${location.host}/api/v1/ws`)

    this.ws.onopen = () => {
      this.retryMs = 1000
      // Восстановление подписок после переподключения (разд. 4.2).
      for (const sub of this.subs.values()) this.send({ op: 'sub', ...sub })
    }
    this.ws.onmessage = (ev) => {
      let msg: WsMessage
      try {
        msg = JSON.parse(ev.data)
      } catch {
        return
      }
      const k = keyOf(msg.topic, msg.service as string | undefined)
      // Обработчики точного ключа + обработчики «всего топика».
      this.handlers.get(k)?.forEach((h) => h(msg))
      if (k !== msg.topic) this.handlers.get(msg.topic)?.forEach((h) => h(msg))
    }
    this.ws.onclose = () => {
      this.ws = null
      if (this.closedByUser) return
      setTimeout(() => this.connect(), this.retryMs)
      this.retryMs = Math.min(this.retryMs * 2, 15000)
    }
  }

  /** Закрыть навсегда (logout). */
  close() {
    this.closedByUser = true
    this.ws?.close()
    this.ws = null
  }

  /** Подписаться на топик; возвращает функцию отписки. */
  subscribe(sub: Sub, handler: Handler): () => void {
    const k = keyOf(sub.topic, sub.service)
    if (!this.handlers.has(k)) this.handlers.set(k, new Set())
    this.handlers.get(k)!.add(handler)

    if (!this.subs.has(k)) {
      this.subs.set(k, sub)
      this.send({ op: 'sub', ...sub })
    }
    this.connect()

    return () => {
      const set = this.handlers.get(k)
      set?.delete(handler)
      if (set && set.size === 0) {
        this.handlers.delete(k)
        this.subs.delete(k)
        this.send({ op: 'unsub', topic: sub.topic, service: sub.service })
      }
    }
  }

  /** Отправить фрейм, если соединение открыто. */
  private send(payload: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(payload))
  }
}

/** Единственный экземпляр на всё приложение. */
export const wsClient = new WsClient()
