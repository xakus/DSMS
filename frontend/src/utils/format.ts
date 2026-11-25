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

/**
 * Занятые ядра → проценты одного ядра, как в `docker stats`.
 * 0.35 ядра → "35.0%"; 1.5 ядра → "150%". Нет данных → прочерк.
 */
export function fmtCores(cores: number | undefined | null): string {
  if (cores === undefined || cores === null || !isFinite(cores)) return '—'
  const pct = cores * 100
  return `${pct >= 100 ? pct.toFixed(0) : pct.toFixed(1)}%`
}

/**
 * Память «занято / лимит». Лимит 0 или не задан — показываем только занятое
 * (0 означает «лимита нет», а не «лимит ноль»).
 */
export function fmtMem(used: number | undefined | null, limit?: number | null): string {
  if (used === undefined || used === null) return '—'
  return limit ? `${fmtBytes(used)} / ${fmtBytes(limit)}` : fmtBytes(used)
}

/** Секунды → короткая длительность для политик: 0.5 → "0.5s", 90 → "1m 30s". */
export function fmtDuration(sec: number | undefined | null): string {
  if (sec === undefined || sec === null || !isFinite(sec)) return '—'
  if (sec < 60) return `${Number(sec.toFixed(1))}s`
  const m = Math.floor(sec / 60)
  const s = Math.round(sec % 60)
  return s ? `${m}m ${s}s` : `${m}m`
}

/**
 * Короткое имя образа для таблиц: без адреса реестра и пути, но с тегом —
 * тег и есть самое важное. «registry.example.com:5000/shad/app:1.4.2» → «app:1.4.2».
 * Полную ссылку показывай рядом в подсказке.
 */
export function shortImageName(image: string): string {
  return image.slice(image.lastIndexOf('/') + 1)
}
