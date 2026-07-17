<script setup lang="ts">
// Таблица сервисов (FR-04 3.4.1–3.4.3): группировка по стекам,
// цветовая индикация реплик, быстрые действия в строке.
import { computed, h, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCard, NDataTable, NTag, NInput, NInputNumber,
  NModal, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import {
  PlayOutline, PauseOutline, RefreshOutline, ResizeOutline,
  PricetagOutline, ArrowUndoOutline, TrashOutline, DocumentTextOutline,
  CloudDownloadOutline,
} from '@vicons/ionicons5'
import { onBeforeUnmount, onMounted } from 'vue'
import AppLayout from '../components/AppLayout.vue'
import { rowActions } from '../utils/actions'
import { api, ApiError } from '../api/client'
import type { ServiceInfo } from '../types'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()

/** Перейти на экран логов этого сервиса. */
function goLogs(svc: ServiceInfo) {
  router.push({ name: 'logs', query: { service: svc.id } })
}

const services = ref<ServiceInfo[]>([])
const loading = ref(false)

// Модал Scale
const scaleTarget = ref<ServiceInfo | null>(null)
const scaleValue = ref(1)
// Модал Update image
const imageTarget = ref<ServiceInfo | null>(null)
const imageValue = ref('')
// Модал Remove (двойное подтверждение с вводом имени, 3.4.3)
const removeTarget = ref<ServiceInfo | null>(null)
const removeConfirmName = ref('')

/** Загрузить список сервисов. */
async function load() {
  loading.value = true
  try {
    services.value = await api<ServiceInfo[]>('/services')
  } catch {
    message.error(t('common.loadFailed'))
  } finally {
    loading.value = false
  }
}

// Список сервисов обновляется на фиксированном интервале.
let timer: number | null = null
onMounted(() => {
  load()
  timer = window.setInterval(load, 10000)
})
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

/** Выполнить действие с обновлением списка и обработкой ошибок. */
async function run(action: () => Promise<unknown>, okMsg = 'OK') {
  try {
    await action()
    message.success(okMsg)
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
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

/** Цвет реплик: зелёный/красный (разд. 6.3). */
function replicasType(svc: ServiceInfo): 'success' | 'error' | 'warning' {
  if (svc.running >= svc.desired && svc.desired > 0) return 'success'
  if (svc.running === 0 && svc.desired === 0) return 'warning'
  return 'error'
}

const columns = computed<DataTableColumns<ServiceInfo>>(() => [
  {
    title: t('services.name'), key: 'name',
    render: (row) => h(RouterLink, { to: { name: 'service', params: { id: row.id } }, class: 'svc-link' }, { default: () => row.name }),
  },
  { title: t('services.stack'), key: 'stack', render: (row) => row.stack || '—' },
  { title: 'Image', key: 'image', ellipsis: true },
  { title: 'Mode', key: 'mode', width: 100 },
  {
    title: t('services.replicas'), key: 'replicas', width: 110,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: replicasType(row) },
        { default: () => `${row.running}/${row.desired}${row.update_state ? ' ⟳' : ''}` }),
  },
  { title: t('services.ports'), key: 'ports', render: (row) => (row.ports ?? []).join(', ') || '—' },
  {
    title: t('services.actions'), key: 'actions', width: 320,
    render: (row) =>
      rowActions([
        row.stopped
          ? { icon: PlayOutline, tip: t('services.startTip'), type: 'success', onClick: () => start(row) }
          : { icon: PauseOutline, tip: t('services.stopTip'), type: 'warning', disabled: row.mode === 'global', onClick: () => stop(row) },
        { icon: CloudDownloadOutline, tip: t('services.deployTip'), type: 'primary', onClick: () => deploy(row) },
        { icon: RefreshOutline, tip: t('services.redeployTip'), onClick: () => redeploy(row) },
        { icon: ResizeOutline, tip: t('services.scaleTip'), disabled: row.mode === 'global', onClick: () => openScale(row) },
        { icon: PricetagOutline, tip: t('services.imageTip'), onClick: () => openImage(row) },
        { icon: DocumentTextOutline, tip: t('nav.logs'), onClick: () => goLogs(row) },
        { icon: ArrowUndoOutline, tip: t('services.rollbackTip'), onClick: () => rollback(row) },
        { icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => openRemove(row) },
      ]),
  },
])
</script>

<template>
  <AppLayout>
    <n-card :title="t('nav.services')" size="small">
      <n-data-table
        :columns="columns" :data="services" :loading="loading"
        size="small" :bordered="false" :row-key="(r: ServiceInfo) => r.id"
      />
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
</style>
