<script setup lang="ts">
// Логи (FR-05): терминало-подобный вид, живой стриминг по WS,
// tail-селектор, timestamps on/off, фильтр, пауза автоскролла,
// скачивание буфера в .txt, раскраска stdout/stderr.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NSpace, NSelect, NSwitch, NInput, NButton, NTag } from 'naive-ui'
import AppLayout from '../components/AppLayout.vue'
import { api } from '../api/client'
import { wsClient } from '../api/ws'
import type { ServiceInfo } from '../types'

/** Строка лога из WS (формат разд. 4.2). */
interface LogRow {
  task?: string
  node?: string
  stream: string
  ts?: string
  line: string
}

const route = useRoute()
const { t } = useI18n()

const services = ref<ServiceInfo[]>([])
const serviceId = ref<string>((route.query.service as string) ?? '')
const tail = ref(500)
const showTs = ref(false)
const follow = ref(true) // автоскролл (3.5.4)
const filter = ref('')
const rows = ref<LogRow[]>([])
const termEl = ref<HTMLDivElement | null>(null)

/** Максимум строк в клиентском буфере — защита памяти вкладки. */
const MAX_ROWS = 5000

let unsub: (() => void) | null = null

const tailOptions = [100, 500, 1000].map((v) => ({ label: String(v), value: v }))
const serviceOptions = computed(() =>
  services.value.map((s) => ({ label: s.name, value: s.id })))

/** Отфильтрованные строки (поиск по подстроке на клиенте, 3.5.4). */
const visible = computed(() => {
  if (!filter.value) return rows.value
  const q = filter.value.toLowerCase()
  return rows.value.filter((r) => r.line.toLowerCase().includes(q))
})

/** Подписаться на логи выбранного сервиса. */
function subscribe() {
  unsub?.()
  rows.value = []
  if (!serviceId.value) return
  unsub = wsClient.subscribe(
    { topic: 'logs', service: serviceId.value, tail: tail.value },
    (msg) => {
      rows.value.push(msg as unknown as LogRow)
      if (rows.value.length > MAX_ROWS) rows.value.splice(0, rows.value.length - MAX_ROWS)
      if (follow.value) nextTick(scrollDown)
    },
  )
}

function scrollDown() {
  if (termEl.value) termEl.value.scrollTop = termEl.value.scrollHeight
}

/** Скачивание видимого буфера в .txt (3.5.6). */
function download() {
  const text = visible.value
    .map((r) => `${r.ts ?? ''} [${r.node ?? ''}/${r.task?.slice(0, 8) ?? ''}] ${r.line}`)
    .join('\n')
  const blob = new Blob([text], { type: 'text/plain' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `dsms-logs-${serviceId.value.slice(0, 12)}.txt`
  a.click()
  URL.revokeObjectURL(a.href)
}

onMounted(async () => {
  services.value = await api<ServiceInfo[]>('/services')
  if (serviceId.value) subscribe()
})
onBeforeUnmount(() => unsub?.())

watch([serviceId, tail], subscribe)
</script>

<template>
  <AppLayout>
    <n-card size="small" class="logs-card">
      <n-space align="center" class="toolbar">
        <n-select v-model:value="serviceId" :options="serviceOptions" :placeholder="t('logs.selectService')" class="svc-select" filterable />
        <n-select v-model:value="tail" :options="tailOptions" class="tail-select" />
        <n-switch v-model:value="showTs"><template #checked>ts</template><template #unchecked>ts</template></n-switch>
        <n-switch v-model:value="follow"><template #checked>follow</template><template #unchecked>follow</template></n-switch>
        <n-input v-model:value="filter" :placeholder="t('logs.filter')" clearable class="filter" />
        <n-button size="small" @click="download">⬇ .txt</n-button>
        <n-tag size="small" :bordered="false">{{ visible.length }}</n-tag>
      </n-space>

      <!-- Терминальный вид: моношрифт, тёмный фон (разд. 6.2 экран 7) -->
      <div ref="termEl" class="term">
        <div v-for="(r, i) in visible" :key="i" class="row" :class="r.stream">
          <span v-if="showTs && r.ts" class="ts">{{ r.ts.slice(11, 23) }}</span>
          <span v-if="r.node || r.task" class="meta">[{{ (r.node ?? '').slice(0, 8) }}/{{ (r.task ?? '').slice(0, 8) }}]</span>
          <span class="line">{{ r.line }}</span>
        </div>
        <div v-if="!serviceId" class="hint">{{ t('logs.selectService') }}</div>
      </div>
    </n-card>
  </AppLayout>
</template>

<style scoped>
/* Терминало-подобный вид (3.5, разд. 6.2): чёрный фон, моношрифт */
.toolbar {
  margin-bottom: 8px;
}
.svc-select {
  min-width: 220px;
}
.tail-select {
  width: 90px;
}
.filter {
  width: 200px;
}
.term {
  background: #0c0c0c;
  color: #d8d8d8;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 12px;
  line-height: 1.5;
  height: calc(100vh - 220px);
  overflow-y: auto;
  padding: 8px 12px;
  border-radius: 4px;
  white-space: pre-wrap;
  word-break: break-all;
}
/* stderr — красноватый (3.5.2) */
.row.stderr .line {
  color: #ff8a8a;
}
.ts {
  color: #6a9955;
  margin-right: 8px;
}
.meta {
  color: #569cd6;
  margin-right: 8px;
}
.hint {
  opacity: 0.5;
  padding: 24px;
  text-align: center;
}
</style>
