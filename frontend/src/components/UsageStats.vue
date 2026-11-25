<script setup lang="ts">
// Плитки «потребление сейчас»: CPU, RAM, сеть (ТЗ 3.4.7).
// Используется в шапке страницы сервиса и страницы стека.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { NGrid, NGridItem, NStatistic, NText, NIcon, NFlex } from 'naive-ui'
import { ArrowDownOutline, ArrowUpOutline, AlertCircleOutline } from '@vicons/ionicons5'
import type { NoData, Usage } from '../types'
import { fmtBps, fmtBytes, fmtCores } from '../utils/format'
import { isPartial, noDataLines } from '../utils/usage'

const props = defineProps<{
  usage?: Usage | null
  /** Сколько задач работает — чтобы предупредить о неполных данных. */
  running: number
  /** С каких нод нет метрик и почему (у стека причин нет — они в таблице сервисов). */
  noData?: NoData[] | null
}>()

const { t } = useI18n()

const partial = computed(() => isPartial({ usage: props.usage, running: props.running }))
/** Метрик нет вообще, хотя задачи работают. */
const missing = computed(() => !props.usage && props.running > 0)
/** Причины по нодам: «нода: что случилось». */
const reasons = computed(() => noDataLines({ no_data: props.noData ?? [] }, t))
</script>

<template>
  <div>
    <n-grid cols="2 s:4" responsive="screen" :x-gap="16" :y-gap="16">
      <n-grid-item>
        <n-statistic label="CPU" :value="fmtCores(usage?.cpu_cores)" />
      </n-grid-item>
      <n-grid-item>
        <n-statistic label="RAM" :value="usage ? fmtBytes(usage.mem_used) : '—'" />
        <!-- Лимит — отдельной строкой: рядом с числом на телефоне он рвался пополам -->
        <n-text v-if="usage?.mem_limit" depth="3" class="limit">
          {{ t('usage.ofLimit', { limit: fmtBytes(usage.mem_limit) }) }}
        </n-text>
      </n-grid-item>
      <n-grid-item>
        <n-statistic :value="usage ? fmtBps(usage.rx_bps) : '—'">
          <template #label>
            <n-flex align="center" :size="4" :wrap="false">
              <n-icon :component="ArrowDownOutline" :size="14" aria-hidden="true" />
              {{ t('usage.rx') }}
            </n-flex>
          </template>
        </n-statistic>
      </n-grid-item>
      <n-grid-item>
        <n-statistic :value="usage ? fmtBps(usage.tx_bps) : '—'">
          <template #label>
            <n-flex align="center" :size="4" :wrap="false">
              <n-icon :component="ArrowUpOutline" :size="14" aria-hidden="true" />
              {{ t('usage.tx') }}
            </n-flex>
          </template>
        </n-statistic>
      </n-grid-item>
    </n-grid>

    <!-- Пояснения: откуда цифры и можно ли им верить -->
    <n-text depth="3" class="note">
      <template v-if="missing">
        <n-icon :component="AlertCircleOutline" :size="14" aria-hidden="true" />
        {{ t('usage.noData.title') }}
      </template>
      <template v-else-if="partial">
        <n-icon :component="AlertCircleOutline" :size="14" aria-hidden="true" />
        {{ t('usage.partial', { got: usage?.tasks, total: running }) }}
      </template>
      <template v-else>{{ t('usage.cpuHint') }}</template>
      <!-- Почему данных нет — по каждой ноде, чтобы было понятно, что чинить -->
      <template v-if="missing || partial">
        <span v-for="line in reasons" :key="line" class="reason">{{ line }}</span>
      </template>
    </n-text>
  </div>
</template>

<style scoped>
/* Лимит под числом — мельче самого числа и одной строкой */
.limit {
  display: block;
  font-size: 14px;
  white-space: nowrap;
}
.note {
  display: block;
  margin-top: 12px;
  font-size: 14px;
}
.note :deep(.n-icon) {
  vertical-align: -2px;
}
.reason {
  display: block;
}
</style>
