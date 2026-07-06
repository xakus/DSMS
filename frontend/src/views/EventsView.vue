<script setup lang="ts">
// События и журнал (FR-06, разд. 6.2 экран 11): две вкладки —
// живая лента Docker events (WS) + audit-журнал действий (REST, пагинация).
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NCard, NTabs, NTabPane, NDataTable, NTag, NButton, NSpace } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import AppLayout from '../components/AppLayout.vue'
import { api } from '../api/client'
import { wsClient } from '../api/ws'

/** Docker event из WS (разд. 4.2). */
interface EventRow {
  type: string
  action: string
  actor: string
  ts: number
}

/** Запись audit-журнала (3.6.2). */
interface AuditRow {
  id: number
  ts: number
  username: string
  action: string
  object_type: string
  object_id: string
  details: string
}

const { t } = useI18n()

const events = ref<EventRow[]>([])
const audit = ref<AuditRow[]>([])
const auditOffset = ref(0)
const AUDIT_PAGE = 50
/** Живая лента ограничена, чтобы не расти бесконечно. */
const MAX_EVENTS = 1000

let unsub: (() => void) | null = null

// Высота живой ленты — почти на весь экран (virtual-scroll требует высоту).
const tableHeight = ref(window.innerHeight - 260)
function onResize() {
  tableHeight.value = window.innerHeight - 260
}

onMounted(() => {
  unsub = wsClient.subscribe({ topic: 'events' }, (msg) => {
    events.value.unshift(msg as unknown as EventRow)
    if (events.value.length > MAX_EVENTS) events.value.pop()
  })
  loadAudit()
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  unsub?.()
  window.removeEventListener('resize', onResize)
})

async function loadAudit(more = false) {
  if (!more) auditOffset.value = 0
  const page = await api<AuditRow[]>(`/audit?limit=${AUDIT_PAGE}&offset=${auditOffset.value}`)
  audit.value = more ? [...audit.value, ...page] : page
  auditOffset.value += page.length
}

/** Цвет действия события (create/update — норм, die/fail — плохо). */
function actionType(a: string): 'success' | 'error' | 'default' {
  if (/create|start|update/.test(a)) return 'success'
  if (/die|fail|kill|down|destroy|remove|delete/.test(a)) return 'error'
  return 'default'
}

const fmtTime = (ts: number) => new Date(ts * 1000).toLocaleString()

const eventColumns = computed<DataTableColumns<EventRow>>(() => [
  { title: t('events.time'), key: 'ts', width: 180, render: (r) => fmtTime(r.ts) },
  { title: t('events.type'), key: 'type', width: 110 },
  {
    title: t('events.action'), key: 'action', width: 140,
    render: (r) => h(NTag, { size: 'small', bordered: false, type: actionType(r.action) }, { default: () => r.action }),
  },
  { title: t('events.object'), key: 'actor', ellipsis: true },
])

const auditColumns = computed<DataTableColumns<AuditRow>>(() => [
  { title: t('events.time'), key: 'ts', width: 180, render: (r) => fmtTime(r.ts) },
  { title: t('events.user'), key: 'username', width: 120 },
  { title: t('events.action'), key: 'action', width: 200 },
  { title: t('events.object'), key: 'object', render: (r) => `${r.object_type}: ${r.object_id}` },
  { title: t('events.details'), key: 'details', ellipsis: true },
])
</script>

<template>
  <AppLayout>
    <n-card size="small">
      <n-tabs type="line">
        <!-- Живая лента Docker events (3.6.1) — на высоту экрана -->
        <n-tab-pane name="events" :tab="t('events.events')">
          <n-data-table :columns="eventColumns" :data="events" size="small" :bordered="false"
                        :max-height="tableHeight" virtual-scroll :row-key="(r: EventRow) => r.ts + r.actor + r.action" />
        </n-tab-pane>
        <!-- Журнал действий пользователя (3.6.2) — по контенту + «загрузить ещё» -->
        <n-tab-pane name="audit" :tab="t('events.audit')">
          <n-space vertical>
            <n-data-table :columns="auditColumns" :data="audit" size="small" :bordered="false"
                          :row-key="(r: AuditRow) => r.id" />
            <n-space justify="center">
              <n-button size="small" quaternary @click="loadAudit(true)">{{ t('events.loadMore') }}</n-button>
            </n-space>
          </n-space>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </AppLayout>
</template>
