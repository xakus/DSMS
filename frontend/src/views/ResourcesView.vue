<script setup lang="ts">
// Resources (разд. 6.2 экран 8): вкладки Secrets / Configs / Networks / Volumes
// (FR-09, FR-10). Значение secret вводится один раз и не читается обратно.
import { computed, h, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NCard, NTabs, NTabPane, NDataTable, NButton, NSpace, NModal, NInput,
  NForm, NFormItem, NSwitch, NTag, NIcon, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { EyeOutline, TrashOutline, AddOutline } from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import { rowActions } from '../utils/actions'
import { api, ApiError } from '../api/client'
import { fmtBytes } from '../utils/format'

/** Secret/config в списке. */
interface SecretRow { id: string; name: string; created: string; used_by: string[] | null; size?: number }
/** Сеть в списке. */
interface NetworkRow { id: string; name: string; driver: string; scope: string; attachable: boolean; subnet: string }
/** Том (per-node). */
interface VolumeRow { node: string; node_id: string; name: string; driver: string; size: number; in_use: boolean }

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

const secrets = ref<SecretRow[]>([])
const configs = ref<SecretRow[]>([])
const networks = ref<NetworkRow[]>([])
const volumes = ref<VolumeRow[]>([])

// Модал создания secret/config
const createKind = ref<'secret' | 'config' | null>(null)
const createForm = ref({ name: '', data: '' })
// Модал создания сети
const showNetForm = ref(false)
const netForm = ref({ name: '', driver: 'overlay', attachable: false, subnet: '' })
// Просмотр config
const configView = ref<{ name: string; data: string } | null>(null)

async function loadAll() {
  const [s, c, n, v] = await Promise.allSettled([
    api<SecretRow[]>('/secrets'),
    api<SecretRow[]>('/configs'),
    api<NetworkRow[]>('/networks'),
    api<VolumeRow[]>('/volumes'),
  ])
  if (s.status === 'fulfilled') secrets.value = s.value
  if (c.status === 'fulfilled') configs.value = c.value
  if (n.status === 'fulfilled') networks.value = n.value
  if (v.status === 'fulfilled') volumes.value = v.value
}
onMounted(loadAll)

/** Выполнить мутацию + перезагрузка + ошибки. */
async function run(action: () => Promise<unknown>) {
  try {
    await action()
    message.success('OK')
    await loadAll()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  }
}

function openCreate(kind: 'secret' | 'config') {
  createForm.value = { name: '', data: '' }
  createKind.value = kind
}
function applyCreate() {
  const kind = createKind.value
  if (!kind) return
  createKind.value = null
  run(() => api(`/${kind}s`, { method: 'POST', body: createForm.value }))
}

function removeSecret(row: SecretRow) {
  const used = (row.used_by?.length ?? 0) > 0
  dialog.warning({
    title: used ? t('resources.removeUsedConfirm', { name: row.name }) : t('resources.removeConfirm', { name: row.name }),
    positiveText: t('common.remove'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => run(() => api(`/secrets/${row.id}${used ? '?force=true' : ''}`, { method: 'DELETE' })),
  })
}

function removeConfig(row: SecretRow) {
  dialog.warning({
    title: t('resources.removeConfirm', { name: row.name }),
    positiveText: t('common.remove'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => run(() => api(`/configs/${row.id}`, { method: 'DELETE' })),
  })
}

async function viewConfig(row: SecretRow) {
  configView.value = await api(`/configs/${row.id}`)
}

function applyNet() {
  showNetForm.value = false
  run(() => api('/networks', { method: 'POST', body: netForm.value }))
}

function removeNetwork(row: NetworkRow) {
  dialog.warning({
    title: t('resources.removeConfirm', { name: row.name }),
    positiveText: t('common.remove'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => run(() => api(`/networks/${row.id}`, { method: 'DELETE' })),
  })
}

function removeVolume(row: VolumeRow) {
  dialog.warning({
    title: row.in_use ? t('resources.removeUsedConfirm', { name: row.name }) : t('resources.removeConfirm', { name: row.name }),
    positiveText: t('common.remove'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => run(() =>
      api(`/volumes/${row.name}?node=${row.node_id}${row.in_use ? '&force=true' : ''}`, { method: 'DELETE' })),
  })
}

/** used_by → теги. */
const usedByCol = (row: SecretRow) =>
  (row.used_by?.length ?? 0) === 0
    ? '—'
    : h(NSpace, { size: 4 }, { default: () => row.used_by!.map((s) => h(NTag, { size: 'tiny', bordered: false }, { default: () => s })) })

const secretColumns = computed<DataTableColumns<SecretRow>>(() => [
  { title: t('services.name'), key: 'name' },
  { title: t('resources.usedBy'), key: 'used_by', render: usedByCol },
  { title: t('services.created'), key: 'created', width: 180, render: (r) => new Date(r.created).toLocaleString() },
  {
    title: t('services.actions'), key: 'a', width: 80,
    render: (r) => rowActions([{ icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => removeSecret(r) }]),
  },
])

const configColumns = computed<DataTableColumns<SecretRow>>(() => [
  { title: t('services.name'), key: 'name' },
  { title: t('resources.size'), key: 'size', width: 90, render: (r) => fmtBytes(r.size ?? 0) },
  { title: t('resources.usedBy'), key: 'used_by', render: usedByCol },
  {
    title: t('services.actions'), key: 'a', width: 110,
    render: (r) => rowActions([
      { icon: EyeOutline, tip: t('resources.view'), onClick: () => viewConfig(r) },
      { icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => removeConfig(r) },
    ]),
  },
])

const networkColumns = computed<DataTableColumns<NetworkRow>>(() => [
  { title: t('services.name'), key: 'name' },
  { title: 'Driver', key: 'driver', width: 100 },
  { title: 'Scope', key: 'scope', width: 90 },
  { title: 'Subnet', key: 'subnet', width: 150, render: (r) => r.subnet || '—' },
  { title: 'Attachable', key: 'attachable', width: 100, render: (r) => (r.attachable ? '✓' : '—') },
  {
    title: t('services.actions'), key: 'a', width: 80,
    render: (r) => rowActions([{ icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => removeNetwork(r) }]),
  },
])

const volumeColumns = computed<DataTableColumns<VolumeRow>>(() => [
  { title: t('services.node'), key: 'node', width: 140 },
  { title: t('services.name'), key: 'name', ellipsis: true },
  { title: 'Driver', key: 'driver', width: 90 },
  { title: t('resources.size'), key: 'size', width: 100, render: (r) => (r.size >= 0 ? fmtBytes(r.size) : '—') },
  {
    title: t('resources.inUse'), key: 'in_use', width: 100,
    render: (r) => h(NTag, { size: 'small', bordered: false, type: r.in_use ? 'success' : 'default' },
      { default: () => (r.in_use ? t('resources.used') : t('resources.unused')) }),
  },
  {
    title: t('services.actions'), key: 'a', width: 80,
    render: (r) => rowActions([{ icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => removeVolume(r) }]),
  },
])
</script>

<template>
  <AppLayout>
    <n-card size="small">
      <n-tabs type="line">
        <n-tab-pane name="secrets" :tab="t('resources.secrets')">
          <n-space vertical>
            <n-space justify="end">
              <n-button size="small" type="primary" @click="openCreate('secret')">
                <template #icon><n-icon :component="AddOutline" /></template>
                {{ t('resources.addSecret') }}
              </n-button>
            </n-space>
            <n-data-table :columns="secretColumns" :data="secrets" size="small" :bordered="false" />
          </n-space>
        </n-tab-pane>
        <n-tab-pane name="configs" :tab="t('resources.configs')">
          <n-space vertical>
            <n-space justify="end">
              <n-button size="small" type="primary" @click="openCreate('config')">
                <template #icon><n-icon :component="AddOutline" /></template>
                {{ t('resources.addConfig') }}
              </n-button>
            </n-space>
            <n-data-table :columns="configColumns" :data="configs" size="small" :bordered="false" />
          </n-space>
        </n-tab-pane>
        <n-tab-pane name="networks" :tab="t('resources.networks')">
          <n-space vertical>
            <n-space justify="end">
              <n-button size="small" type="primary" @click="showNetForm = true">
                <template #icon><n-icon :component="AddOutline" /></template>
                {{ t('resources.addNetwork') }}
              </n-button>
            </n-space>
            <n-data-table :columns="networkColumns" :data="networks" size="small" :bordered="false" />
          </n-space>
        </n-tab-pane>
        <n-tab-pane name="volumes" :tab="t('resources.volumes')">
          <n-data-table :columns="volumeColumns" :data="volumes" size="small" :bordered="false" />
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <!-- Создание secret/config: значение secret уходит один раз (3.9.2) -->
    <n-modal :show="!!createKind" preset="card" :title="createKind === 'secret' ? '+ Secret' : '+ Config'" class="res-modal"
             @close="createKind = null" @mask-click="createKind = null">
      <n-form label-placement="top">
        <n-form-item :label="t('services.name')">
          <n-input v-model:value="createForm.name" />
        </n-form-item>
        <n-form-item :label="t('resources.value')">
          <n-input v-model:value="createForm.data" type="textarea" :autosize="{ minRows: 4, maxRows: 16 }" />
        </n-form-item>
      </n-form>
      <p v-if="createKind === 'secret'" class="hint">{{ t('resources.secretHint') }}</p>
      <template #footer>
        <n-space justify="end">
          <n-button @click="createKind = null">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="applyCreate">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Создание overlay-сети (3.10.3) -->
    <n-modal v-model:show="showNetForm" preset="card" title="+ Network" class="res-modal">
      <n-form label-placement="top">
        <n-form-item :label="t('services.name')"><n-input v-model:value="netForm.name" /></n-form-item>
        <n-form-item label="Driver"><n-input v-model:value="netForm.driver" /></n-form-item>
        <n-form-item label="Attachable"><n-switch v-model:value="netForm.attachable" /></n-form-item>
        <n-form-item label="Subnet (optional)"><n-input v-model:value="netForm.subnet" placeholder="10.10.0.0/24" /></n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showNetForm = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="applyNet">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Просмотр config (3.9.3) -->
    <n-modal :show="!!configView" preset="card" :title="configView?.name" class="res-modal"
             @close="configView = null" @mask-click="configView = null">
      <pre class="cfg-view">{{ configView?.data }}</pre>
    </n-modal>
  </AppLayout>
</template>

<style scoped>
/* Модалы ресурсов и просмотр config */
.res-modal {
  max-width: 560px;
}
.cfg-view {
  max-height: 60vh;
  overflow: auto;
  font-size: 12px;
}
.hint {
  font-size: 12px;
  opacity: 0.65;
}
</style>
