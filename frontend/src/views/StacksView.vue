<script setup lang="ts">
// Стеки (FR-08): список с агрегированным статусом,
// redeploy/remove стека целиком (двойное подтверждение имени).
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NCard, NDataTable, NTag, NModal, NInput, useMessage, useDialog } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { RefreshOutline, TrashOutline, CloudDownloadOutline } from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import { rowActions } from '../utils/actions'
import { api, ApiError } from '../api/client'
import type { StackInfo } from '../types'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

const stacks = ref<StackInfo[]>([])
const loading = ref(false)

// Модал удаления стека с вводом имени (3.8.3)
const removeTarget = ref<StackInfo | null>(null)
const removeConfirmName = ref('')

async function load() {
  loading.value = true
  try {
    stacks.value = await api<StackInfo[]>('/stacks')
  } catch {
    message.error(t('common.loadFailed'))
  } finally {
    loading.value = false
  }
}

// Список стеков обновляется на фиксированном интервале.
let timer: number | null = null
onMounted(() => {
  load()
  timer = window.setInterval(load, 10000)
})
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

function redeploy(stack: StackInfo) {
  dialog.info({
    title: t('stacks.redeployConfirm', { name: stack.name }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await api(`/stacks/${stack.name}/redeploy`, { method: 'POST' })
        message.success('OK')
        await load()
      } catch (e) {
        message.error(e instanceof ApiError ? e.message : 'error')
      }
    },
  })
}

// Деплой всего стека: каждому сервису тянет свежий образ по тегу (в отличие
// от redeploy, который перезапускает те же digest'ы) и катит без простоя.
function deploy(stack: StackInfo) {
  dialog.info({
    title: t('stacks.deployConfirm', { name: stack.name }),
    content: t('stacks.deployHint'),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await api(`/stacks/${stack.name}/deploy`, { method: 'POST' })
        message.success('OK')
        await load()
      } catch (e) {
        message.error(e instanceof ApiError ? e.message : 'error')
      }
    },
  })
}

function openRemove(stack: StackInfo) {
  removeTarget.value = stack
  removeConfirmName.value = ''
}
async function applyRemove() {
  const stack = removeTarget.value
  if (!stack || removeConfirmName.value !== stack.name) return
  removeTarget.value = null
  try {
    await api(`/stacks/${stack.name}`, { method: 'DELETE' })
    message.success('OK')
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  }
}

/** Агрегированный статус стека: зелёный/жёлтый/красный (3.8.1). */
function stackType(s: StackInfo): 'success' | 'error' | 'warning' {
  if (s.running >= s.desired && s.desired > 0) return 'success'
  if (s.running === 0) return 'error'
  return 'warning'
}

const columns = computed<DataTableColumns<StackInfo>>(() => [
  { title: t('stacks.name'), key: 'name' },
  { title: t('stacks.services'), key: 'services', width: 110 },
  {
    title: t('services.replicas'), key: 'replicas', width: 120,
    render: (row) => h(NTag, { size: 'small', bordered: false, type: stackType(row) },
      { default: () => `${row.running}/${row.desired}` }),
  },
  {
    title: t('services.actions'), key: 'actions', width: 120,
    render: (row) => row.name === '(no stack)' ? null :
      rowActions([
        { icon: CloudDownloadOutline, tip: t('stacks.deployTip'), type: 'primary', onClick: () => deploy(row) },
        { icon: RefreshOutline, tip: t('stacks.redeployTip'), onClick: () => redeploy(row) },
        { icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => openRemove(row) },
      ]),
  },
])
</script>

<template>
  <AppLayout>
    <n-card :title="t('nav.stacks')" size="small">
      <n-data-table
        :columns="columns" :data="stacks" :loading="loading"
        size="small" :bordered="false" :row-key="(r: StackInfo) => r.name"
      />
    </n-card>

    <!-- Remove stack: ввод имени (3.8.3) -->
    <n-modal :show="!!removeTarget" preset="dialog" type="error" :title="t('stacks.removeTitle')"
             :positive-text="t('common.remove')" :negative-text="t('common.cancel')"
             :positive-button-props="{ disabled: removeConfirmName !== removeTarget?.name, type: 'error' }"
             @positive-click="applyRemove" @negative-click="removeTarget = null" @close="removeTarget = null">
      <p>{{ t('stacks.removeHint', { name: removeTarget?.name, count: removeTarget?.services }) }}</p>
      <n-input v-model:value="removeConfirmName" :placeholder="removeTarget?.name" />
    </n-modal>
  </AppLayout>
</template>
