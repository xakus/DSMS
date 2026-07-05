// Форматирование величин для UI: байты, скорости, проценты, время.

/** Байты → человекочитаемая строка (1.5 GiB). */
export function fmtBytes(n: number | undefined): string {
  if (n === undefined || !isFinite(n)) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

/** Байты/с → строка скорости (2.1 MiB/s). */
export function fmtBps(n: number | undefined): string {
  return n === undefined ? '—' : `${fmtBytes(n)}/s`
}

/** Проценты с одним знаком. */
export function fmtPct(n: number | undefined): string {
  return n === undefined || !isFinite(n) ? '—' : `${n.toFixed(1)}%`
}

/** Секунды аптайма → "12d 5h" / "3h 12m". */
export function fmtUptime(sec: number | undefined): string {
  if (!sec) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}
