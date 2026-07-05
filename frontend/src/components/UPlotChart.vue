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
  const opts: uPlot.Options = {
    width: el.value.clientWidth || 600,
    height: props.height ?? 220,
    // Тёмная/светлая тема наследуются через CSS-переменные Naive UI.
    series: [{}, ...props.series],
    axes: [
      {
        stroke: 'rgba(128,128,128,0.9)',
        grid: { stroke: 'rgba(128,128,128,0.15)' },
        // Ось X — время (unix-секунды).
        values: (_u, ticks) => ticks.map((t) => new Date(t * 1000).toLocaleTimeString()),
      },
      {
        stroke: 'rgba(128,128,128,0.9)',
        grid: { stroke: 'rgba(128,128,128,0.15)' },
        values: (_u, ticks) => ticks.map((t) => (props.yFormat ? props.yFormat(t) : String(t))),
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
