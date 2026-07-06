<script setup lang="ts">
// Страница сервиса (FR-04 3.4.4): спецификация read-only,
// задачи с историей (state, exit code, нода, время), статус update.
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NDataTable, NTag, NSpace, NButton, NDescriptions, NDescriptionsItem, useMessage } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import AppLayout from '../components/AppLayout.vue'
import { api } from '../api/client'
import type { ServiceDetail, ServiceTask } from '../types'

const route = useRoute()
const { t } = useI18n()
const message = useMessage()

const serviceId = route.params.id as string
const svc = ref<ServiceDetail | null>(null)

async function load() {
  try {
    svc.value = await api<ServiceDetail>(`/services/${serviceId}`)
  } catch {
    message.error(t('common.loadFailed'))
  }
}

// Детали сервиса обновляются на фиксированном интервале.
let timer: number | null = null
onMounted(() => {
  load()
  timer = window.setInterval(load, 10000)
})
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

/** Цвет состояния задачи (разд. 6.3). */
function stateType(s: string): 'success' | 'error' | 'warning' | 'default' {
  if (s === 'running') return 'success'
  if (s === 'failed' || s === 'rejected') return 'error'
  if (s === 'shutdown') return 'default'
  return 'warning'
}

const taskColumns = computed<DataTableColumns<ServiceTask>>(() => [
  { title: 'Slot', key: 'slot', width: 60 },
  { title: t('nodes.state'), key: 'state', width: 120,
    render: (row) => h(NTag, { size: 'small', bordered: false, type: stateType(row.state) },
      { default: () => row.state + (row.exit_code !== undefined ? ` (${row.exit_code})` : '') }) },
  { title: t('services.node'), key: 'node' },
  { title: t('nodes.message'), key: 'message', ellipsis: true },
  { title: t('services.created'), key: 'created_at', width: 180,
    render: (row) => new Date(row.created_at).toLocaleString() },
])
</script>

<template>
  <AppLayout>
    <template v-if="svc">
      <n-space align="center" justify="space-between" class="mb">
        <n-space align="center">
          <h2>{{ svc.name }}</h2>
          <n-tag :bordered="false">{{ svc.mode }}</n-tag>
          <n-tag v-if="svc.stack" :bordered="false" type="info">{{ svc.stack }}</n-tag>
          <n-tag v-if="svc.update" :bordered="false" type="warning">⟳ {{ svc.update.state }}</n-tag>
        </n-space>
        <!-- Вкладка Logs страницы сервиса (3.5) — переход на экран логов -->
        <n-button size="small" @click="$router.push({ name: 'logs', query: { service: svc.id } })">
          📜 {{ t('nav.logs') }}
        </n-button>
      </n-space>

      <!-- Спецификация read-only (3.4.4) -->
      <n-card :title="t('services.spec')" size="small" class="mb">
        <n-descriptions :column="1" label-placement="left" size="small">
          <n-descriptions-item label="Image">{{ svc.spec.image }}</n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.env?.length" label="Env">
            <div v-for="e in svc.spec.env" :key="e" class="mono">{{ e }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.mounts?.length" label="Mounts">
            <div v-for="m in svc.spec.mounts" :key="m" class="mono">{{ m }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.constraints?.length" label="Constraints">
            <div v-for="c in svc.spec.constraints" :key="c" class="mono">{{ c }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.limits" label="Limits">
            CPU {{ svc.spec.limits.cpus || '—' }} · MEM {{ svc.spec.limits.memory || '—' }}
          </n-descriptions-item>
        </n-descriptions>
      </n-card>

      <!-- Задачи с историей (3.4.4) -->
      <n-card :title="`${t('services.tasks')} (${svc.tasks.length})`" size="small">
        <n-data-table :columns="taskColumns" :data="svc.tasks" size="small" :bordered="false" />
      </n-card>
    </template>
  </AppLayout>
</template>

<style scoped>
/* Моноширинный текст спецификации */
.mono {
  font-family: monospace;
  font-size: 12px;
}
.mb {
  margin-bottom: 16px;
}
</style>
