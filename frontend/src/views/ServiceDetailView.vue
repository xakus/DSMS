<script setup lang="ts">
// Страница сервиса (FR-04 3.4.4, 3.4.7): сводка и потребление сейчас,
// задачи с нодами и нагрузкой, спецификация read-only, статус обновления.
import { computed, h, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCard, NDataTable, NTag, NFlex, NButton, NIcon, NText, NResult, NSkeleton,
  NDescriptions, NDescriptionsItem, NGrid, NGridItem, NStatistic,
  useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { CloudDownloadOutline, DocumentTextOutline, SyncOutline, ArrowBackOutline } from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import UsageStats from '../components/UsageStats.vue'
import { isUpdateNoteworthy, usageColumns, USAGE_COLS_WIDTH } from '../utils/usage'
import { fmtBytes, fmtDuration } from '../utils/format'
import { api, ApiError } from '../api/client'
import { usePolling } from '../composables/usePolling'
import type { ServiceDetail, ServiceResources, ServiceTask } from '../types'

/** HTTP-статус «сервиса нет». */
const HTTP_NOT_FOUND = 404

/** Ширины колонок таблицы задач, px. */
const COL = { slot: 60, state: 120, node: 140, message: 130, created: 175 } as const

/** Минимальная ширина таблицы задач: уже — прокрутка внутри карточки. */
const TASKS_MIN_WIDTH = COL.slot + COL.state + COL.node + USAGE_COLS_WIDTH + COL.message + COL.created

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

const serviceId = route.params.id as string
const svc = ref<ServiceDetail | null>(null)
/** Сервис не найден (удалён или ссылка устарела). */
const notFound = ref(false)

const { loading, refresh: load } = usePolling(async () => {
  try {
    svc.value = await api<ServiceDetail>(`/services/${encodeURIComponent(serviceId)}`)
    notFound.value = false
  } catch (e) {
    if (e instanceof ApiError && e.status === HTTP_NOT_FOUND) {
      svc.value = null
      notFound.value = true
      return
    }
    throw e
  }
})

// Деплой: тянет свежую версию образа по текущему тегу (в отличие от
// redeploy — тот же digest). Окно закрываем сразу, итог — тостом: долгий
// деплой не должен держать кнопку и провоцировать повторные клики.
function deploy() {
  if (!svc.value) return
  const s = svc.value
  dialog.info({
    title: t('services.deployConfirm', { name: s.name }),
    content: t('services.deployHint'),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      message.info(t('services.deployStarted'))
      api(`/services/${encodeURIComponent(s.id)}/deploy`, { method: 'POST' })
        .then(() => message.success(t('common.done')))
        .catch((e) => message.error(e instanceof ApiError ? e.message : t('common.error')))
        .finally(() => load())
    },
  })
}

/** Цвет состояния задачи (разд. 6.3). */
function stateType(s: string): 'success' | 'error' | 'warning' | 'default' {
  if (s === 'running') return 'success'
  if (s === 'failed' || s === 'rejected') return 'error'
  if (s === 'shutdown') return 'default'
  return 'warning'
}

/** Цвет реплик в шапке: остановлен панелью — нейтральный, не красный. */
const replicasType = computed<'success' | 'error' | 'warning' | 'default'>(() => {
  const s = svc.value
  if (!s || s.stopped) return 'default'
  if (s.running >= s.desired && s.desired > 0) return 'success'
  if (s.running === 0 && s.desired === 0) return 'warning'
  return 'error'
})

/** Лимиты/резервы одной строкой: «CPU 0.5 · RAM 512 MiB». */
function fmtResources(r: ServiceResources | undefined): string {
  if (!r) return ''
  const parts: string[] = []
  if (r.cpus) parts.push(`CPU ${r.cpus}`)
  if (r.memory) parts.push(`RAM ${fmtBytes(r.memory)}`)
  return parts.join(' · ')
}

/** Короткий digest для показа: полный — в подсказке. */
const DIGEST_SHOWN_CHARS = 19 // «sha256:» + 12 символов хеша
const digestShort = computed(() => svc.value?.spec.digest?.slice(0, DIGEST_SHOWN_CHARS) ?? '')

/** Команда запуска одной строкой (command + args). */
const commandLine = computed(() => {
  const sp = svc.value?.spec
  return sp ? [...sp.command, ...sp.args].join(' ') : ''
})

const labelEntries = computed(() => Object.entries(svc.value?.spec.labels ?? {}))

const taskColumns = computed<DataTableColumns<ServiceTask>>(() => [
  { title: 'Slot', key: 'slot', width: COL.slot },
  {
    title: t('nodes.state'), key: 'state', width: COL.state,
    render: (row) => h(NTag, { size: 'small', bordered: false, type: stateType(row.state) },
      { default: () => row.state + (row.exit_code !== undefined ? ` (${row.exit_code})` : '') }),
  },
  {
    // Нода — ссылка на её страницу: оттуда видно, что ещё на ней работает.
    title: t('services.node'), key: 'node', width: COL.node,
    render: (row) => row.node_id
      ? h(RouterLink, { to: { name: 'node', params: { id: row.node_id } }, class: 'row-link' },
          { default: () => row.node || row.node_id })
      : '—',
  },
  ...usageColumns<ServiceTask>(t),
  { title: t('nodes.message'), key: 'message', minWidth: COL.message, ellipsis: { tooltip: true } },
  {
    title: t('services.created'), key: 'created_at', width: COL.created,
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
])
</script>

<template>
  <AppLayout>
    <!-- Загрузка: скелетон вместо пустого экрана -->
    <n-card v-if="loading && !svc" size="small">
      <n-skeleton text :repeat="4" />
    </n-card>

    <!-- Сервис не найден -->
    <n-result
      v-else-if="notFound" status="warning"
      :title="t('services.notFound')" :description="t('services.notFoundHint')"
    >
      <template #footer>
        <n-button type="primary" @click="router.push({ name: 'services' })">{{ t('services.backToList') }}</n-button>
      </template>
    </n-result>

    <template v-else-if="svc">
      <n-flex align="center" justify="space-between" class="mb">
        <n-flex align="center" :size="12">
          <n-button quaternary circle :aria-label="t('services.backToList')" @click="router.push({ name: 'services' })">
            <template #icon><n-icon :component="ArrowBackOutline" /></template>
          </n-button>
          <h2 class="title">{{ svc.name }}</h2>
          <n-tag :bordered="false" :type="replicasType">
            {{ svc.stopped ? t('services.stoppedTag') : `${svc.running}/${svc.desired}` }}
          </n-tag>
          <n-tag :bordered="false">{{ svc.mode }}</n-tag>
          <router-link v-if="svc.stack" :to="{ name: 'stack', params: { name: svc.stack } }">
            <n-tag :bordered="false" type="info" class="clickable">{{ svc.stack }}</n-tag>
          </router-link>
          <n-tag v-if="svc.update && isUpdateNoteworthy(svc.update.state)" :bordered="false" type="warning">
            <template #icon><n-icon :component="SyncOutline" /></template>
            {{ svc.update.state }}
          </n-tag>
        </n-flex>
        <n-flex align="center" :size="8">
          <!-- Деплой: подтянуть свежий образ по текущему тегу -->
          <n-button size="small" type="primary" @click="deploy">
            <template #icon><n-icon :component="CloudDownloadOutline" /></template>
            {{ t('services.deployBtn') }}
          </n-button>
          <!-- Логи сервиса (3.5) — переход на экран логов -->
          <n-button size="small" @click="router.push({ name: 'logs', query: { service: svc.id } })">
            <template #icon><n-icon :component="DocumentTextOutline" /></template>
            {{ t('nav.logs') }}
          </n-button>
        </n-flex>
      </n-flex>

      <!-- Сообщение о зависшем/идущем обновлении (3.4.5) -->
      <n-text v-if="svc.update?.message && isUpdateNoteworthy(svc.update.state)" depth="3" class="update-msg">
        {{ svc.update.message }}
      </n-text>

      <!-- Сейчас: где работает и сколько потребляет (3.4.7) -->
      <n-card :title="t('usage.now')" size="small" class="mb">
        <n-grid cols="2 s:4" responsive="screen" :x-gap="16" :y-gap="16" class="mb">
          <n-grid-item>
            <n-statistic :label="t('services.replicas')" :value="`${svc.running}/${svc.desired}`" />
          </n-grid-item>
          <n-grid-item span="1 s:3">
            <n-statistic :label="t('services.nodes')">
              <n-text v-if="svc.nodes.length" class="nodes">{{ svc.nodes.join(', ') }}</n-text>
              <n-text v-else depth="3">—</n-text>
            </n-statistic>
          </n-grid-item>
        </n-grid>
        <UsageStats :usage="svc.usage" :running="svc.running" />
      </n-card>

      <!-- Задачи с историей и потреблением (3.4.4) -->
      <n-card :title="`${t('services.tasks')} (${svc.tasks.length})`" size="small" class="mb">
        <n-data-table
          :columns="taskColumns" :data="svc.tasks" size="small" :bordered="false"
          :row-key="(r: ServiceTask) => r.id" :scroll-x="TASKS_MIN_WIDTH"
        >
          <template #empty>
            <n-text depth="3">{{ svc.stopped ? t('services.noTasksStopped') : t('services.noTasks') }}</n-text>
          </template>
        </n-data-table>
      </n-card>

      <!-- Спецификация read-only (3.4.4) -->
      <n-card :title="t('services.spec')" size="small">
        <n-descriptions :column="1" label-placement="left" size="small" label-class="spec-label">
          <n-descriptions-item :label="t('spec.image')">
            <span class="mono">{{ svc.spec.image }}</span>
            <n-text v-if="svc.spec.digest" depth="3" class="mono digest" :title="svc.spec.digest">
              {{ digestShort }}
            </n-text>
          </n-descriptions-item>
          <n-descriptions-item v-if="commandLine" :label="t('spec.command')">
            <span class="mono">{{ commandLine }}</span>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.ports.length" :label="t('services.ports')">
            <div v-for="p in svc.spec.ports" :key="p" class="mono">{{ p }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.networks.length" :label="t('spec.networks')">
            <div v-for="n in svc.spec.networks" :key="n" class="mono">{{ n }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.mounts.length" :label="t('spec.mounts')">
            <div v-for="m in svc.spec.mounts" :key="m" class="mono">{{ m }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.constraints.length" :label="t('spec.constraints')">
            <div v-for="c in svc.spec.constraints" :key="c" class="mono">{{ c }}</div>
          </n-descriptions-item>
          <n-descriptions-item :label="t('spec.limits')">
            {{ fmtResources(svc.spec.limits) || t('spec.notSet') }}
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.reservations" :label="t('spec.reservations')">
            {{ fmtResources(svc.spec.reservations) }}
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.restart" :label="t('spec.restart')">
            {{ svc.spec.restart.condition }}
            <template v-if="svc.spec.restart.delay_sec"> · {{ t('spec.delay') }} {{ fmtDuration(svc.spec.restart.delay_sec) }}</template>
            <template v-if="svc.spec.restart.max_attempts"> · {{ t('spec.maxAttempts', { n: svc.spec.restart.max_attempts }) }}</template>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.update_config" :label="t('spec.updateConfig')">
            {{ svc.spec.update_config.order || 'stop-first' }}
            · {{ t('spec.parallelism', { n: svc.spec.update_config.parallelism }) }}
            <template v-if="svc.spec.update_config.delay_sec"> · {{ t('spec.delay') }} {{ fmtDuration(svc.spec.update_config.delay_sec) }}</template>
            <template v-if="svc.spec.update_config.failure_action"> · {{ t('spec.onFailure') }} {{ svc.spec.update_config.failure_action }}</template>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.healthcheck.length" :label="t('spec.healthcheck')">
            <span class="mono">{{ svc.spec.healthcheck.join(' ') }}</span>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.secrets.length" :label="t('spec.secrets')">
            <div v-for="s in svc.spec.secrets" :key="s" class="mono">{{ s }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.configs.length" :label="t('spec.configs')">
            <div v-for="c in svc.spec.configs" :key="c" class="mono">{{ c }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="svc.spec.env.length" :label="t('spec.env')">
            <div v-for="e in svc.spec.env" :key="e" class="mono">{{ e }}</div>
          </n-descriptions-item>
          <n-descriptions-item v-if="labelEntries.length" :label="t('spec.labels')">
            <div v-for="[k, v] in labelEntries" :key="k" class="mono">{{ k }}={{ v }}</div>
          </n-descriptions-item>
          <n-descriptions-item :label="t('spec.createdAt')">
            {{ new Date(svc.created_at).toLocaleString() }}
          </n-descriptions-item>
          <n-descriptions-item :label="t('spec.updatedAt')">
            {{ new Date(svc.updated_at).toLocaleString() }}
          </n-descriptions-item>
        </n-descriptions>
      </n-card>
    </template>
  </AppLayout>
</template>

<style scoped>
/* Моноширинный текст спецификации; длинные значения переносятся, а не
   распирают страницу */
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 14px;
  word-break: break-all;
}
.digest {
  margin-left: 8px;
}
.mb {
  margin-bottom: 16px;
}
.title {
  margin: 0;
}
.update-msg {
  display: block;
  margin-bottom: 16px;
}
/* Список нод в плитке — обычным размером, а не крупной цифрой */
.nodes {
  font-size: 16px;
  word-break: break-word;
}
.clickable {
  cursor: pointer;
}
:deep(.row-link) {
  color: inherit;
}
/* Подписи спецификации одной ширины — значения выстраиваются в колонку */
:deep(.spec-label) {
  white-space: nowrap;
  vertical-align: top;
}
</style>
