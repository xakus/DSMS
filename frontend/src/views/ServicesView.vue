<script setup lang="ts">
// Таблица сервисов (FR-04 3.4.1–3.4.3, 3.4.7): стек, реплики с цветовой
// индикацией, ноды размещения, потребление (CPU, RAM, сеть), быстрые действия.
import { computed, h, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCard, NDataTable, NInput, NInputNumber, NText,
  NModal, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import {
  PlayOutline, PauseOutline, RefreshOutline, ResizeOutline,
  PricetagOutline, ArrowUndoOutline, TrashOutline, DocumentTextOutline,
  CloudDownloadOutline,
} from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import { rowActions } from '../utils/actions'
import { nameWithImage, nodesCell, usageColumns, USAGE_COLUMN_SPECS } from '../utils/usage'
import { replicasCell } from '../utils/replicas'
import type { ColumnSpec } from '../utils/columns'
import { api, ApiError } from '../api/client'
import { usePolling } from '../composables/usePolling'
import { useMediaQuery } from '../composables/useMediaQuery'
import { useColumnWidths } from '../composables/useColumnWidths'
import type { ServiceInfo } from '../types'

/**
 * С какой ширины экрана колонка действий закрепляется справа. На узком
 * экране закреплённая колонка заняла бы всю ширину и закрыла данные.
 */
const PIN_ACTIONS_QUERY = '(min-width: 1024px)'

/**
 * С какой ширины экрана показываются все 8 кнопок действий. Уже — редкие
 * действия уходят в меню «ещё»: иначе на ноутбуке (1280–1366 px) кнопки
 * вытесняли колонки CPU/RAM/Сеть за горизонтальную прокрутку.
 */
const ALL_ACTIONS_QUERY = '(min-width: 1440px)'

/** Сколько кнопок действий в компактном режиме (последняя — меню «ещё»). */
const COMPACT_ACTIONS = 5

/**
 * Колонки с данными: минимальная ширина, px, и доля лишней ширины экрана.
 * На широком мониторе лишнее место делится между всеми колонками — раньше
 * его целиком забирало «Имя», и данные прижимались к правому краю.
 */
const DATA_COLUMNS: ColumnSpec[] = [
  { key: 'name', base: 180, grow: 2 },
  { key: 'replicas', base: 125, grow: 1 },
  { key: 'nodes', base: 135, grow: 1.5 },
  ...USAGE_COLUMN_SPECS,
  { key: 'stack', base: 110, grow: 1 },
  { key: 'ports', base: 130, grow: 1.5 },
]

/**
 * Ширина колонки действий, px (не растёт). Кнопка — 28 px + зазор 6 px,
 * поля ячейки — 32 px: 8 кнопок = 8·28 + 7·6 + 32 = 298; 5 кнопок = 5·28 + 4·6 + 32 = 196.
 */
const ACTIONS_WIDTH_FULL = 298
const ACTIONS_WIDTH_COMPACT = 196

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()

/** Перейти на экран логов этого сервиса. */
function goLogs(svc: ServiceInfo) {
  router.push({ name: 'logs', query: { service: svc.id } })
}

const services = ref<ServiceInfo[]>([])
const { loading, refresh: load } = usePolling(async () => {
  services.value = await api<ServiceInfo[]>('/services')
})
const pinActions = useMediaQuery(PIN_ACTIONS_QUERY)
const allActions = useMediaQuery(ALL_ACTIONS_QUERY)

/** Все колонки таблицы; ширина действий зависит от числа видимых кнопок. */
const columnSpecs = computed<ColumnSpec[]>(() => [
  ...DATA_COLUMNS,
  { key: 'actions', base: allActions.value ? ACTIONS_WIDTH_FULL : ACTIONS_WIDTH_COMPACT },
])
// widths — точная ширина каждой колонки под текущий экран; minWidth — уже
// неё таблица прокручивается внутри карточки, а не страницы.
const { wrapRef, widths, minWidth } = useColumnWidths(columnSpecs)

// Модал Scale
const scaleTarget = ref<ServiceInfo | null>(null)
const scaleValue = ref(1)
// Модал Update image
const imageTarget = ref<ServiceInfo | null>(null)
const imageValue = ref('')
// Модал Remove (двойное подтверждение с вводом имени, 3.4.3)
const removeTarget = ref<ServiceInfo | null>(null)
const removeConfirmName = ref('')

/** Выполнить действие с обновлением списка и обработкой ошибок. */
async function run(action: () => Promise<unknown>) {
  try {
    await action()
    message.success(t('common.done'))
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('common.error'))
  } finally {
    await load()
  }
}

function stop(svc: ServiceInfo) {
  dialog.warning({
    title: t('services.stopConfirm', { name: svc.name }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => run(() => api(`/services/${svc.id}/scale`, { method: 'POST', body: { replicas: 0 } })),
  })
}

function start(svc: ServiceInfo) {
  run(() => api(`/services/${svc.id}/start`, { method: 'POST' }))
}

function redeploy(svc: ServiceInfo) {
  dialog.info({
    title: t('services.redeployConfirm', { name: svc.name }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => run(() => api(`/services/${svc.id}/redeploy`, { method: 'POST' })),
  })
}

// Деплой: тянет свежую версию образа по текущему тегу (в отличие от
// redeploy, который перезапускает тот же digest). Обновление на бэке может
// идти не мгновенно, поэтому окно НЕ держим в loading — закрываем сразу
// (onPositiveClick не возвращает Promise), а прогресс показываем тостом.
// Иначе окно «висит», юзер жмёт повторно и плодит лишние задачи.
function deploy(svc: ServiceInfo) {
  dialog.info({
    title: t('services.deployConfirm', { name: svc.name }),
    content: t('services.deployHint'),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      message.info(t('services.deployStarted'))
      run(() => api(`/services/${svc.id}/deploy`, { method: 'POST' }))
    },
  })
}

function openScale(svc: ServiceInfo) {
  scaleTarget.value = svc
  scaleValue.value = svc.desired
}
function applyScale() {
  const svc = scaleTarget.value
  if (!svc) return
  scaleTarget.value = null
  run(() => api(`/services/${svc.id}/scale`, { method: 'POST', body: { replicas: scaleValue.value } }))
}

function openImage(svc: ServiceInfo) {
  imageTarget.value = svc
  imageValue.value = svc.image
}
function applyImage() {
  const svc = imageTarget.value
  if (!svc) return
  imageTarget.value = null
  run(() => api(`/services/${svc.id}/image`, { method: 'POST', body: { image: imageValue.value } }))
}

function rollback(svc: ServiceInfo) {
  dialog.warning({
    title: t('services.rollbackConfirm', { name: svc.name }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => run(() => api(`/services/${svc.id}/rollback`, { method: 'POST' })),
  })
}

function openRemove(svc: ServiceInfo) {
  removeTarget.value = svc
  removeConfirmName.value = ''
}
function applyRemove() {
  const svc = removeTarget.value
  if (!svc || removeConfirmName.value !== svc.name) return
  removeTarget.value = null
  run(() => api(`/services/${svc.id}`, { method: 'DELETE' }))
}

/** Сколько портов показывать в ячейке; остальные — в подсказке. */
const MAX_PORTS_SHOWN = 2

/**
 * Ячейка «Порты»: не больше двух строк (высота строки таблицы постоянна).
 * Портов больше двух — первый и «ещё N», полный список — в подсказке.
 */
function portsCell(ports: string[] | undefined) {
  const list = ports ?? []
  if (list.length === 0) return '—'
  const line = (p: string) => h('div', { key: p, class: 'one-line' }, p)
  if (list.length <= MAX_PORTS_SHOWN) return h('div', { title: list.join(', ') }, list.map(line))
  return h('div', { title: list.join(', ') }, [
    line(list[0]),
    h(NText, { depth: 3 }, { default: () => t('usage.moreNodes', { count: list.length - 1 }) }),
  ])
}

// Порядок колонок — по важности: что работает, где и сколько ест. Стек и
// порты — в конце: на ноутбуке они уходят в прокрутку, главное остаётся видно.
const columns = computed<DataTableColumns<ServiceInfo>>(() => [
  {
    // Имя и образ в одной ячейке: образ — второстепенная строка под именем.
    title: t('services.name'), key: 'name', width: widths.value.name,
    sorter: (a, b) => a.name.localeCompare(b.name),
    render: (row) => nameWithImage(
      h(RouterLink, { to: { name: 'service', params: { id: row.id } }, class: 'row-link', title: row.name }, { default: () => row.name }),
      row.image,
    ),
  },
  { title: t('services.replicas'), key: 'replicas', width: widths.value.replicas, render: (row) => replicasCell(row, t) },
  { title: t('services.nodes'), key: 'nodes', width: widths.value.nodes, render: (row) => nodesCell(row.nodes, t) },
  ...usageColumns<ServiceInfo>(t, widths.value),
  {
    title: t('services.stack'), key: 'stack', width: widths.value.stack,
    render: (row) => row.stack
      ? h(RouterLink, { to: { name: 'stack', params: { name: row.stack } }, class: 'row-link one-line', title: row.stack }, { default: () => row.stack })
      : '—',
  },
  { title: t('services.ports'), key: 'ports', width: widths.value.ports, render: (row) => portsCell(row.ports) },
  {
    title: t('services.actions'), key: 'actions', width: widths.value.actions,
    fixed: pinActions.value ? 'right' : undefined,
    render: (row) =>
      // Частые действия — первыми: в компактном режиме остальные уходят в «ещё».
      rowActions([
        row.stopped
          ? { icon: PlayOutline, tip: t('services.startTip'), type: 'success', onClick: () => start(row) }
          : { icon: PauseOutline, tip: t('services.stopTip'), type: 'warning', disabled: row.mode === 'global' || row.desired === 0, onClick: () => stop(row) },
        { icon: CloudDownloadOutline, tip: t('services.deployTip'), type: 'primary', onClick: () => deploy(row) },
        { icon: RefreshOutline, tip: t('services.redeployTip'), onClick: () => redeploy(row) },
        { icon: DocumentTextOutline, tip: t('nav.logs'), onClick: () => goLogs(row) },
        { icon: ResizeOutline, tip: t('services.scaleTip'), disabled: row.mode === 'global', onClick: () => openScale(row) },
        { icon: PricetagOutline, tip: t('services.imageTip'), onClick: () => openImage(row) },
        { icon: ArrowUndoOutline, tip: t('services.rollbackTip'), onClick: () => rollback(row) },
        { icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => openRemove(row) },
      ], allActions.value ? {} : { maxVisible: COMPACT_ACTIONS, moreLabel: t('common.more') }),
  },
])
</script>

<template>
  <AppLayout>
    <n-card :title="t('nav.services')" size="small">
      <!-- Обёртка измеряется: по её ширине считаются ширины колонок -->
      <div ref="wrapRef">
        <n-data-table
          class="live-table" table-layout="fixed"
          :columns="columns" :data="services" :loading="loading"
          size="small" :bordered="false" :row-key="(r: ServiceInfo) => r.id"
          :scroll-x="minWidth"
        >
          <template #empty>
            <n-text depth="3">{{ t('services.empty') }}</n-text>
          </template>
        </n-data-table>
      </div>
    </n-card>

    <!-- Scale -->
    <n-modal :show="!!scaleTarget" preset="dialog" :title="`${t('services.scale')}: ${scaleTarget?.name}`"
             :positive-text="t('common.confirm')" :negative-text="t('common.cancel')"
             @positive-click="applyScale" @negative-click="scaleTarget = null" @close="scaleTarget = null">
      <n-input-number v-model:value="scaleValue" :min="0" class="w100" />
    </n-modal>

    <!-- Update image -->
    <n-modal :show="!!imageTarget" preset="dialog" :title="`${t('services.updateImage')}: ${imageTarget?.name}`"
             :positive-text="t('common.confirm')" :negative-text="t('common.cancel')"
             @positive-click="applyImage" @negative-click="imageTarget = null" @close="imageTarget = null">
      <n-input v-model:value="imageValue" placeholder="repo/image:tag" />
    </n-modal>

    <!-- Remove: двойное подтверждение с вводом имени (3.4.3) -->
    <n-modal :show="!!removeTarget" preset="dialog" type="error" :title="t('services.removeTitle')"
             :positive-text="t('common.remove')" :negative-text="t('common.cancel')"
             :positive-button-props="{ disabled: removeConfirmName !== removeTarget?.name, type: 'error' }"
             @positive-click="applyRemove" @negative-click="removeTarget = null" @close="removeTarget = null">
      <p>{{ t('services.removeHint', { name: removeTarget?.name }) }}</p>
      <n-input v-model:value="removeConfirmName" :placeholder="removeTarget?.name" />
    </n-modal>
  </AppLayout>
</template>

<style scoped>
/* Полноширинные контролы модалов */
.w100 {
  width: 100%;
}
</style>
