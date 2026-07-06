<script setup lang="ts">
// Универсальная обёртка над uPlot: живой график с ресайзом.
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
    // Тёмная/светлая тема наследуются через CSS-переменные Naive UI.
    series: [{}, ...props.series],
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
    legend: { show: props.series.length > 1 },
    cursor: { drag: { x: false, y: false } },
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
}
</style>
