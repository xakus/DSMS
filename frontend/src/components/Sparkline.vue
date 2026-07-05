<script setup lang="ts">
// Мини-график (sparkline) для карточек нод (3.1.2): одна линия,
// без осей и легенды, фиксированная высота.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'

const props = defineProps<{
  /** [xs, ys] — время и значения */
  data: uPlot.AlignedData
  /** Цвет линии */
  color?: string
  /** Фиксированный максимум Y (для CPU — 100) */
  yMax?: number
}>()

const el = ref<HTMLDivElement | null>(null)
let chart: uPlot | null = null

function build() {
  if (!el.value) return
  chart?.destroy()
  chart = new uPlot(
    {
      width: el.value.clientWidth || 180,
      height: 36,
      series: [
        {},
        {
          stroke: props.color ?? '#63e2b7',
          fill: (props.color ?? '#63e2b7') + '22', // полупрозрачная заливка
          width: 1.5,
          points: { show: false },
        },
      ],
      axes: [{ show: false }, { show: false }],
      scales: props.yMax ? { y: { range: [0, props.yMax] } } : undefined,
      legend: { show: false },
      cursor: { show: false },
    },
    props.data,
    el.value,
  )
}

onMounted(build)
watch(
  () => props.data,
  (d) => chart?.setData(d),
)
onBeforeUnmount(() => chart?.destroy())
</script>

<template>
  <div ref="el" class="spark" />
</template>

<style scoped>
/* Sparkline занимает ширину карточки */
.spark {
  width: 100%;
}
</style>
