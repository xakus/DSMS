<script setup lang="ts">
// Экран Alerts (FR-12 3.12.4): активные + журнал, фильтр по правилу.
import { computed, h, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NCard, NTabs, NTabPane, NDataTable, NTag, NSelect, NSpace } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import AppLayout from '../components/AppLayout.vue'
import { useAlertsStore, type AlertItem } from '../stores/alerts'

const { t } = useI18n()
const alerts = useAlertsStore()
const ruleFilter = ref<string | null>(null)

onMounted(() => {
  alerts.load()
  alerts.start()
})

const ruleOptions = [
  { label: 'disk_high', value: 'disk_high' },
  { label: 'agent_silent', value: 'agent_silent' },
  { label: 'service_degraded', value: 'service_degraded' },
  { label: 'task_failed', value: 'task_failed' },
]

const filteredHistory = computed(() =>
  ruleFilter.value ? alerts.history.filter((a) => a.rule === ruleFilter.value) : alerts.history)

/** Цвет severity. */
const sevType = (s: string) => (s === 'critical' ? 'error' : 'warning') as 'error' | 'warning'
const fmtTime = (ts?: number) => (ts ? new Date(ts * 1000).toLocaleString() : '—')

const columns = computed<DataTableColumns<AlertItem>>(() => [
  { title: t('events.time'), key: 'opened_at', width: 170, render: (r) => fmtTime(r.opened_at) },
  {
    title: t('alerts.rule'), key: 'rule', width: 160,
    render: (r) => h(NTag, { size: 'small', bordered: false, type: sevType(r.severity) }, { default: () => r.rule }),
  },
  { title: t('events.object'), key: 'obj', width: 200, render: (r) => `${r.object_type}: ${r.object_id}` },
  { title: t('alerts.message'), key: 'message', ellipsis: true },
  { title: t('alerts.resolved'), key: 'resolved_at', width: 170, render: (r) => fmtTime(r.resolved_at) },
])
</script>

<template>
  <AppLayout>
    <n-card size="small">
      <n-tabs type="line">
        <n-tab-pane name="active" :tab="`${t('alerts.active')} (${alerts.active.length})`">
          <n-data-table :columns="columns" :data="alerts.active" size="small" :bordered="false"
                        :row-key="(r: AlertItem) => r.id" />
        </n-tab-pane>
        <n-tab-pane name="history" :tab="t('alerts.history')">
          <n-space vertical>
            <n-select v-model:value="ruleFilter" :options="ruleOptions" clearable
                      :placeholder="t('alerts.filterRule')" class="rule-filter" />
            <n-data-table :columns="columns" :data="filteredHistory" size="small" :bordered="false"
                          :row-key="(r: AlertItem) => `${r.id}-${r.state}`" />
          </n-space>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </AppLayout>
</template>

<style scoped>
/* Фильтр по правилу */
.rule-filter {
  max-width: 240px;
}
</style>
