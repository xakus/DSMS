<script setup lang="ts">
// Страница стека (FR-08 3.8.2): сводка и потребление, сервисы стека,
// задействованные сети/тома/секреты/конфиги, действия над стеком целиком.
import { computed, h, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCard, NDataTable, NTag, NButton, NIcon, NFlex, NText, NResult, NSkeleton,
  NGrid, NGridItem, NStatistic,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import {
  RefreshOutline, TrashOutline, CloudDownloadOutline, DocumentTextOutline,
  PlayOutline, PauseOutline, ArrowBackOutline,
} from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import StackDeployModal from '../components/StackDeployModal.vue'
import StackRemoveModal from '../components/StackRemoveModal.vue'
import UsageStats from '../components/UsageStats.vue'
import { nameWithImage, nodesCell, usageColumns, USAGE_COLUMN_SPECS } from '../utils/usage'
import { replicasCell } from '../utils/replicas'
import type { ColumnSpec } from '../utils/columns'
import { stackStatus } from '../utils/stackOps'
import { api, ApiError } from '../api/client'
import { usePolling } from '../composables/usePolling'
import { stackPath, useStackActions } from '../composables/useStackActions'
import { useColumnWidths } from '../composables/useColumnWidths'
import type { ServiceInfo, StackDetail, StackResource } from '../types'

/** HTTP-статус «стека нет». */
const HTTP_NOT_FOUND = 404

/** Колонки таблицы сервисов: минимальная ширина, px, и доля лишней ширины. */
const COLUMNS: ColumnSpec[] = [
  { key: 'name', base: 200, grow: 2 },
  { key: 'replicas', base: 125, grow: 1 },
  { key: 'nodes', base: 135, grow: 1.5 },
  ...USAGE_COLUMN_SPECS,
]

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const stackName = route.params.name as string
const stack = ref<StackDetail | null>(null)
/** Стек не найден (удалён или имя из старой ссылки). */
const notFound = ref(false)

const { loading, refresh } = usePolling(async () => {
  try {
    stack.value = await api<StackDetail>(stackPath(stackName))
    notFound.value = false
  } catch (e) {
    // 404 — не сбой связи, а состояние: показываем экран «не найден».
    if (e instanceof ApiError && e.status === HTTP_NOT_FOUND) {
      stack.value = null
      notFound.value = true
      return
    }
    throw e
  }
})
const actions = useStackActions(refresh)
// Точные ширины колонок под текущий экран (таблица не «дышит» от цифр).
const { wrapRef, widths, minWidth } = useColumnWidths(COLUMNS)

const deployShow = ref(false)
const removeShow = ref(false)

/** Удалить стек и вернуться к списку (страницы стека больше нет). */
async function applyRemove(name: string) {
  removeShow.value = false
  if (await actions.remove(name)) await router.push({ name: 'stacks' })
}

const status = computed(() =>
  stack.value
    ? stackStatus({ ...stack.value, services: stack.value.services.length })
    : { type: 'default' as const },
)
const statusText = computed(() => {
  if (!stack.value) return ''
  return status.value.label ? t(status.value.label) : `${stack.value.running}/${stack.value.desired}`
})

const columns = computed<DataTableColumns<ServiceInfo>>(() => [
  {
    title: t('services.name'), key: 'name', width: widths.value.name,
    render: (row) => nameWithImage(
      h(RouterLink, { to: { name: 'service', params: { id: row.id } }, class: 'row-link', title: row.name },
        { default: () => row.name }),
      row.image,
    ),
  },
  { title: t('services.replicas'), key: 'replicas', width: widths.value.replicas, render: (row) => replicasCell(row, t) },
  { title: t('services.nodes'), key: 'nodes', width: widths.value.nodes, render: (row) => nodesCell(row.nodes, t) },
  ...usageColumns<ServiceInfo>(t, widths.value),
])

/** Группы ресурсов для карточки «Ресурсы стека». */
const resources = computed(() => {
  const s = stack.value
  if (!s) return []
  const plain = (names: string[]): StackResource[] => names.map((name) => ({ name, external: false }))
  return [
    { key: 'networks', title: t('stacks.networks'), items: s.networks },
    { key: 'volumes', title: t('stacks.volumes'), items: plain(s.volumes) },
    { key: 'secrets', title: t('stacks.secrets'), items: s.secrets },
    { key: 'configs', title: t('stacks.configs'), items: s.configs },
  ]
})
</script>

<template>
  <AppLayout>
    <!-- Загрузка: скелетон вместо пустого экрана -->
    <n-card v-if="loading && !stack" size="small">
      <n-skeleton text :repeat="4" />
    </n-card>

    <!-- Стек не найден -->
    <n-result
      v-else-if="notFound" status="warning"
      :title="t('stacks.notFound', { name: stackName })" :description="t('stacks.notFoundHint')"
    >
      <template #footer>
        <n-button type="primary" @click="router.push({ name: 'stacks' })">{{ t('stacks.backToList') }}</n-button>
      </template>
    </n-result>

    <template v-else-if="stack">
      <n-flex align="center" justify="space-between" class="mb">
        <n-flex align="center" :size="12">
          <n-button quaternary circle :aria-label="t('stacks.backToList')" @click="router.push({ name: 'stacks' })">
            <template #icon><n-icon :component="ArrowBackOutline" /></template>
          </n-button>
          <h2 class="title">{{ stack.name }}</h2>
          <n-tag :bordered="false" :type="status.type">{{ statusText }}</n-tag>
          <n-tag v-if="!status.label && stack.stopped > 0" :bordered="false">
            {{ t('stacks.stoppedCount', { count: stack.stopped }) }}
          </n-tag>
          <n-tag v-if="stack.managed" :bordered="false" type="info">{{ t('stacks.fromFile') }}</n-tag>
        </n-flex>
        <n-flex align="center" :size="8">
          <n-button v-if="stack.managed" size="small" @click="deployShow = true">
            <template #icon><n-icon :component="DocumentTextOutline" /></template>
            {{ t('stacks.editFileBtn') }}
          </n-button>
          <n-button size="small" type="success" secondary :disabled="stack.stopped === 0" @click="actions.start(stack.name)">
            <template #icon><n-icon :component="PlayOutline" /></template>
            {{ t('stacks.startBtn') }}
          </n-button>
          <n-button size="small" type="warning" secondary :disabled="stack.stoppable === 0" @click="actions.stop(stack.name)">
            <template #icon><n-icon :component="PauseOutline" /></template>
            {{ t('stacks.stopBtn') }}
          </n-button>
          <n-button size="small" type="primary" :disabled="stack.services.length === 0" @click="actions.deploy(stack.name)">
            <template #icon><n-icon :component="CloudDownloadOutline" /></template>
            {{ t('services.deployBtn') }}
          </n-button>
          <n-button size="small" :disabled="stack.services.length === 0" @click="actions.redeploy(stack.name)">
            <template #icon><n-icon :component="RefreshOutline" /></template>
            {{ t('stacks.redeployBtn') }}
          </n-button>
          <n-button size="small" type="error" secondary @click="removeShow = true">
            <template #icon><n-icon :component="TrashOutline" /></template>
            {{ t('common.remove') }}
          </n-button>
        </n-flex>
      </n-flex>

      <!-- Сводка: сколько сервисов и что стек потребляет сейчас -->
      <n-card :title="t('usage.now')" size="small" class="mb">
        <n-grid cols="2 s:4" responsive="screen" :x-gap="16" :y-gap="16" class="mb">
          <n-grid-item>
            <n-statistic :label="t('stacks.services')" :value="stack.services.length" />
          </n-grid-item>
          <n-grid-item>
            <n-statistic :label="t('services.replicas')" :value="`${stack.running}/${stack.desired}`" />
          </n-grid-item>
        </n-grid>
        <UsageStats :usage="stack.usage" :running="stack.running" />
      </n-card>

      <!-- Сервисы стека -->
      <n-card :title="`${t('stacks.services')} (${stack.services.length})`" size="small" class="mb">
        <!-- Обёртка измеряется: по её ширине считаются ширины колонок -->
        <div ref="wrapRef">
          <n-data-table
            class="live-table" table-layout="fixed"
            :columns="columns" :data="stack.services" size="small" :bordered="false"
            :row-key="(r: ServiceInfo) => r.id" :scroll-x="minWidth"
          >
            <template #empty>
              <n-text depth="3">{{ stack.managed ? t('stacks.noServicesManaged') : t('stacks.noServices') }}</n-text>
            </template>
          </n-data-table>
        </div>
      </n-card>

      <!-- Ресурсы стека: сети, тома, секреты, конфиги (3.8.2) -->
      <n-card :title="t('stacks.resources')" size="small">
        <n-flex vertical :size="16">
          <div v-for="group in resources" :key="group.key">
            <n-text depth="3" class="group-title">{{ group.title }}</n-text>
            <n-flex v-if="group.items.length" :size="8">
              <n-tag v-for="item in group.items" :key="item.name" :bordered="false">
                {{ item.name }}
                <template v-if="item.external"> · {{ t('stacks.external') }}</template>
              </n-tag>
            </n-flex>
            <n-text v-else depth="3">—</n-text>
          </div>
          <n-text depth="3" class="hint">{{ t('stacks.resourcesHint') }}</n-text>
        </n-flex>
      </n-card>

      <StackDeployModal v-model:show="deployShow" :edit-name="stack.name" @deployed="refresh" />
      <StackRemoveModal
        :stack="removeShow ? { name: stack.name, services: stack.services.length } : null"
        @close="removeShow = false" @confirm="applyRemove"
      />
    </template>
  </AppLayout>
</template>

<style scoped>
.mb {
  margin-bottom: 16px;
}
.title {
  margin: 0;
}
.group-title {
  display: block;
  margin-bottom: 8px;
}
.hint {
  font-size: 14px;
}
</style>
