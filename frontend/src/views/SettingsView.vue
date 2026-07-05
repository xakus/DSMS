<script setup lang="ts">
// Settings (разд. 6.2 экран 12), этап 3: вкладка Registries (FR-13).
// Смена пароля, ротация agent-token, пороги алертов — этап 7.
import { computed, h, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NCard, NTabs, NTabPane, NDataTable, NButton, NSpace, NModal,
  NForm, NFormItem, NInput, useMessage,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import AppLayout from '../components/AppLayout.vue'
import { api, ApiError } from '../api/client'
import type { RegistryInfo } from '../types'

const { t } = useI18n()
const message = useMessage()

const registries = ref<RegistryInfo[]>([])
const showForm = ref(false)
/** null — создание; число — редактирование этого id. */
const editId = ref<number | null>(null)
const form = ref({ address: '', username: '', password: '', label: '' })

async function load() {
  try {
    registries.value = await api<RegistryInfo[]>('/registries')
  } catch (e) {
    // 503 — ключ шифрования не настроен: показываем подсказку
    message.warning(e instanceof ApiError ? e.message : t('common.loadFailed'))
  }
}
onMounted(load)

function openCreate() {
  editId.value = null
  form.value = { address: '', username: '', password: '', label: '' }
  showForm.value = true
}

function openEdit(reg: RegistryInfo) {
  editId.value = reg.id
  // Пароль пустой: при сохранении пустым — остаётся прежний (3.13.2)
  form.value = { address: reg.address, username: reg.username, password: '', label: reg.label }
  showForm.value = true
}

async function save() {
  try {
    if (editId.value === null) {
      await api('/registries', { method: 'POST', body: form.value })
    } else {
      await api(`/registries/${editId.value}`, { method: 'PUT', body: form.value })
    }
    showForm.value = false
    message.success('OK')
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  }
}

async function remove(reg: RegistryInfo) {
  try {
    await api(`/registries/${reg.id}`, { method: 'DELETE' })
    message.success('OK')
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  }
}

const columns = computed<DataTableColumns<RegistryInfo>>(() => [
  { title: t('settings.regAddress'), key: 'address' },
  { title: t('settings.regUser'), key: 'username' },
  { title: t('settings.regLabel'), key: 'label' },
  {
    title: '', key: 'actions', width: 120,
    render: (row) =>
      h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'tiny', onClick: () => openEdit(row) }, { default: () => '✏️' }),
          h(NButton, { size: 'tiny', type: 'error', onClick: () => remove(row) }, { default: () => '🗑' }),
        ],
      }),
  },
])
</script>

<template>
  <AppLayout>
    <n-card :title="t('nav.settings')" size="small">
      <n-tabs type="line">
        <n-tab-pane name="registries" :tab="t('settings.registries')">
          <n-space vertical>
            <n-space justify="end">
              <n-button size="small" type="primary" @click="openCreate">+ {{ t('settings.addRegistry') }}</n-button>
            </n-space>
            <n-data-table :columns="columns" :data="registries" size="small" :bordered="false" />
          </n-space>
        </n-tab-pane>
        <!-- Смена пароля / токены / пороги — этап 7 -->
      </n-tabs>
    </n-card>

    <n-modal v-model:show="showForm" preset="card" :title="t('settings.registries')" class="reg-modal">
      <n-form label-placement="top">
        <n-form-item :label="t('settings.regAddress')">
          <n-input v-model:value="form.address" placeholder="ghcr.io" />
        </n-form-item>
        <n-form-item :label="t('settings.regUser')">
          <n-input v-model:value="form.username" />
        </n-form-item>
        <n-form-item :label="t('settings.regPassword')">
          <n-input v-model:value="form.password" type="password" show-password-on="click"
                   :placeholder="editId !== null ? t('settings.regKeepPassword') : ''" />
        </n-form-item>
        <n-form-item :label="t('settings.regLabel')">
          <n-input v-model:value="form.label" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="save">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </AppLayout>
</template>

<style scoped>
/* Компактный модал формы реестра */
.reg-modal {
  max-width: 480px;
}
</style>
