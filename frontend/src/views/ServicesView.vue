<script setup lang="ts">
// Таблица сервисов (FR-04 3.4.1–3.4.3, 3.4.7): стек, реплики с цветовой
// индикацией, ноды размещения, потребление (CPU, RAM, сеть), быстрые действия.
import { computed, h, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCard, NDataTable, NTag, NInput, NInputNumber, NFlex, NText, NIcon,
  NModal, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import {
  PlayOutline, PauseOutline, RefreshOutline, ResizeOutline,
  PricetagOutline, ArrowUndoOutline, TrashOutline, DocumentTextOutline,
  CloudDownloadOutline, SyncOutline,
} from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import { rowActions } from '../utils/actions'
import { isUpdateNoteworthy, nameWithImage, nodesCell, usageColumns, USAGE_COLS_WIDTH } from '../utils/usage'
import { api, ApiError } from '../api/client'
import { usePolling } from '../composables/usePolling'
import { useMediaQuery } from '../composables/useMediaQuery'
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
 * Ширины колонок, px. Кнопка действия — 28 px + зазор 6 px, поля ячейки — 24 px:
 * 8 кнопок = 8·28 + 7·6 + 24 = 290; 5 кнопок = 5·28 + 4·6 + 24 = 188.
 */
const COL = {
  name: 180, replicas: 125, nodes: 135, stack: 110, ports: 130,
  actionsFull: 290, actionsCompact: 190,
} as const

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

/** Ширина колонки действий под текущий экран. */
const actionsWidth = computed(() => (allActions.value ? COL.actionsFull : COL.actionsCompact))

/** Минимальная ширина таблицы: уже — прокрутка внутри карточки, а не страницы. */
const tableMinWidth = computed(() =>
  COL.name + COL.replicas + COL.nodes + USAGE_COLS_WIDTH + COL.stack + COL.ports + actionsWidth.value,
)

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

/** Цвет реплик: зелёный/красный (разд. 6.3); остановлен панелью — нейтральный. */
function replicasType(svc: ServiceInfo): 'success' | 'error' | 'warning' | 'default' {
  if (svc.stopped) return 'default'
  if (svc.running >= svc.desired && svc.desired > 0) return 'success'
  if (svc.running === 0 && svc.desired === 0) return 'warning'
  return 'error'
}

/** Метка реплик: числа или «остановлен», плюс значок идущего обновления. */
function replicasCell(row: ServiceInfo) {
  const text = row.stopped ? t('services.stoppedTag') : `${row.running}/${row.desired}`
  return h(NFlex, { align: 'center', size: 6 }, {
    default: () => [
      h(NTag, { size: 'small', bordered: false, type: replicasType(row) }, { default: () => text }),
      row.mode === 'global'
        ? h(NTag, { size: 'tiny', bordered: false }, { default: () => 'global' })
        : null,
      isUpdateNoteworthy(row.update_state)
        ? h(NIcon, { size: 16, title: row.update_state, 'aria-label': row.update_state },
            { default: () => h(SyncOutline) })
        : null,
    ],
  })
}

// Порядок колонок — по важности: что работает, где и сколько ест. Стек и
// порты — в конце: на ноутбуке они уходят в прокрутку, главное остаётся видно.
const columns = computed<DataTableColumns<ServiceInfo>>(() => [
  {
    // Имя и образ в одной ячейке: образ — второстепенная строка под именем.
    title: t('services.name'), key: 'name', minWidth: COL.name,
    sorter: (a, b) => a.name.localeCompare(b.name),
    render: (row) => nameWithImage(
      h(RouterLink, { to: { name: 'service', params: { id: row.id } }, class: 'svc-link' }, { default: () => row.name }),
      row.image,
    ),
  },
  { title: t('services.replicas'), key: 'replicas', width: COL.replicas, render: replicasCell },
  { title: t('services.nodes'), key: 'nodes', width: COL.nodes, render: (row) => nodesCell(row.nodes) },
  ...usageColumns<ServiceInfo>(t),
  {
    title: t('services.stack'), key: 'stack', width: COL.stack,
    render: (row) => row.stack
      ? h(RouterLink, { to: { name: 'stack', params: { name: row.stack } }, class: 'svc-link' }, { default: () => row.stack })
      : '—',
  },
  { title: t('services.ports'), key: 'ports', width: COL.ports, render: (row) => (row.ports ?? []).join(', ') || '—' },
  {
    title: t('services.actions'), key: 'actions', width: actionsWidth.value,
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
      <n-data-table
        :columns="columns" :data="services" :loading="loading"
        size="small" :bordered="false" :row-key="(r: ServiceInfo) => r.id"
        :scroll-x="tableMinWidth"
      >
        <template #empty>
          <n-text depth="3">{{ t('services.empty') }}</n-text>
        </template>
      </n-data-table>
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
:deep(.svc-link) {
  color: inherit;
}
/* Образ под именем сервиса — второстепенная строка в одну линию:
   не влез — многоточие, полное имя — в подсказке */
:deep(.sub) {
  display: block;
  font-size: 14px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
