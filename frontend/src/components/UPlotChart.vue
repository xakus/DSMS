<script setup lang="ts">
// Универсальная обёртка над uPlot: живой график с ресайзом и tooltip.
// series/data приходят снаружи; компонент только рисует.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'

const props = defineProps<{
  /** Данные uPlot: [xs, ys1, ys2, ...] */
  data: uPlot.AlignedData
  /** Описания линий (без первой оси X) */
  series: uPlot.Series[]
  /** Высота графика, px */
  height?: number
  /** Подпись значения оси Y (например, форматирование байт) */
  yFormat?: (v: number) => string
  /** Диапазон Y: [min, max]; авто — если не задан */
  yRange?: [number, number]
}>()

const el = ref<HTMLDivElement | null>(null)
let chart: uPlot | null = null
let resizeObs: ResizeObserver | null = null

/** Форматирование значения серии для tooltip. */
function fmt(v: number | null | undefined): string {
  if (v == null || !isFinite(v)) return '—'
  return props.yFormat ? props.yFormat(v) : String(Math.round(v * 100) / 100)
}

/**
 * Grafana-подобный tooltip: при наведении показывает время и значения
 * всех серий в точке под курсором, с цветными маркерами.
 */
function tooltipPlugin(): uPlot.Plugin {
  let tip: HTMLDivElement | null = null
  return {
    hooks: {
      init: (u) => {
        tip = document.createElement('div')
        tip.className = 'uplot-tip'
        tip.style.display = 'none'
        u.over.appendChild(tip)
        // Прятать tooltip, когда курсор уходит с графика.
        u.over.addEventListener('mouseleave', () => {
          if (tip) tip.style.display = 'none'
        })
      },
      setCursor: (u) => {
        if (!tip) return
        const idx = u.cursor.idx
        if (idx == null) {
          tip.style.display = 'none'
          return
        }
        const xs = u.data[0]
        const ts = xs[idx]
        if (ts == null) {
          tip.style.display = 'none'
          return
        }
        let html = `<div class="tip-time">${new Date(ts * 1000).toLocaleTimeString()}</div>`
        for (let i = 1; i < u.series.length; i++) {
          const s = u.series[i]
          if (s.show === false) continue
          const val = u.data[i][idx] as number | null
          const color = typeof s.stroke === 'function' ? s.stroke(u, i) : s.stroke
          html += `<div class="tip-row"><span class="tip-dot" style="background:${color}"></span>` +
            `<span class="tip-label">${s.label ?? ''}</span>` +
            `<span class="tip-val">${fmt(val)}</span></div>`
        }
        tip.innerHTML = html
        tip.style.display = 'block'
        // Позиционируем рядом с курсором, не вылезая за правый край.
        const left = u.cursor.left ?? 0
        const anchor = left > u.width / 2 ? left - tip.offsetWidth - 14 : left + 14
        tip.style.transform = `translate(${Math.max(4, anchor)}px, 8px)`
      },
    },
  }
}

/** Построить график под текущую ширину контейнера. */
function build() {
  if (!el.value) return
  chart?.destroy()
  // Единый шрифт подписей осей — читаемый, совпадает с UI.
  const axisFont = '12px system-ui, sans-serif'
  const stroke = 'rgba(140,140,150,0.9)'
  const grid = { stroke: 'rgba(140,140,150,0.15)', width: 1 }
  const ticks = { stroke: 'rgba(140,140,150,0.25)', width: 1 }
  const opts: uPlot.Options = {
    width: el.value.clientWidth || 600,
    height: props.height ?? 220,
    // Внутренние отступы, чтобы крайние подписи осей не срезались.
    padding: [10, 12, 4, 4],
    plugins: [tooltipPlugin()],
    // Тёмная/светлая тема наследуются через CSS-переменные Naive UI.
    series: [
      {},
      // Точки-маркеры на hover делают попадание курсора наглядным.
      ...props.series.map((s) => ({ points: { show: false }, ...s })),
    ],
    axes: [
      {
        stroke,
        grid,
        ticks,
        font: axisFont,
        size: 32, // высота зоны оси X — метки времени не обрезаются снизу
        // Ось X — время (unix-секунды).
        values: (_u, vals) => vals.map((t) => new Date(t * 1000).toLocaleTimeString()),
      },
      {
        stroke,
        grid,
        ticks,
        font: axisFont,
        size: 72, // ширина зоны оси Y — «2.1 MiB/s» и т.п. помещаются целиком
        values: (_u, vals) => vals.map((t) => (props.yFormat ? props.yFormat(t) : String(t))),
      },
    ],
    scales: props.yRange ? { y: { range: props.yRange } } : undefined,
    // Legend прячем — значения показывает tooltip (как в Grafana).
    legend: { show: false },
    cursor: {
      // Вертикальная линия по X, привязка к ближайшей точке (focus).
      x: true,
      y: false,
      drag: { x: false, y: false },
      points: { size: 7, width: 2 },
    },
  }
  chart = new uPlot(opts, props.data, el.value)
}

onMounted(() => {
  build()
  // Перестройка при изменении ширины контейнера (адаптив, разд. 6.1).
  resizeObs = new ResizeObserver(() => {
    if (chart && el.value) chart.setSize({ width: el.value.clientWidth, height: props.height ?? 220 })
  })
  if (el.value) resizeObs.observe(el.value)
})

// Живое обновление данных без пересоздания графика.
watch(
  () => props.data,
  (d) => chart?.setData(d),
)

onBeforeUnmount(() => {
  resizeObs?.disconnect()
  chart?.destroy()
})
</script>

<template>
  <div ref="el" class="uplot-wrap" />
</template>

<style scoped>
/* График растягивается на ширину родителя */
.uplot-wrap {
  width: 100%;
  position: relative;
}

/* Grafana-подобный tooltip */
:deep(.uplot-tip) {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 10;
  pointer-events: none;
  background: rgba(24, 24, 28, 0.94);
  color: #eaeaea;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  padding: 6px 8px;
  font-size: 14px;
  line-height: 1.5;
  white-space: nowrap;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}
:deep(.tip-time) {
  font-weight: 600;
  margin-bottom: 2px;
  opacity: 0.85;
}
:deep(.tip-row) {
  display: flex;
  align-items: center;
  gap: 6px;
}
:deep(.tip-dot) {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex: 0 0 auto;
}
:deep(.tip-label) {
  opacity: 0.8;
}
:deep(.tip-val) {
  margin-left: auto;
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
</style>
