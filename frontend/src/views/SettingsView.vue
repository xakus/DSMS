<script setup lang="ts">
// Settings (разд. 6.2 экран 12): General (пароль, пороги алертов,
// инструкция ротации agent-token) + Registries (FR-13).
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCard, NTabs, NTabPane, NDataTable, NButton, NSpace, NModal,
  NForm, NFormItem, NInput, NInputNumber, NAlert, NIcon, useMessage,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { CreateOutline, TrashOutline, AddOutline } from '@vicons/ionicons5'
import { rowActions } from '../utils/actions'
import AppLayout from '../components/AppLayout.vue'
import { api, ApiError } from '../api/client'
import { useAuthStore } from '../stores/auth'
import type { RegistryInfo } from '../types'

const { t } = useI18n()
const message = useMessage()
const router = useRouter()
const auth = useAuthStore()

// --- General: смена пароля + порог disk-алерта + ретенция истории ---
const pwForm = ref({ old: '', new: '' })
const diskPct = ref(90)
const retentionDays = ref(7)

/** Загрузить настройки панели. */
async function loadSettings() {
  try {
    const s = await api<{ alert_disk_pct: number; metrics_retention_days: number }>('/settings')
    diskPct.value = s.alert_disk_pct
    retentionDays.value = s.metrics_retention_days
  } catch { /* настройки недоступны — оставляем дефолт */ }
}

/** Сменить пароль: после успеха — relogin. */
async function changePassword() {
  try {
    await api('/settings/password', { method: 'POST', body: pwForm.value })
    message.success(t('settings.pwChanged'))
    await auth.logout()
    router.replace({ name: 'login' })
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  }
}

/** Сохранить пороги и ретенцию (применяются сразу, без рестарта). */
async function saveThreshold() {
  try {
    await api('/settings', {
      method: 'PUT',
      body: { alert_disk_pct: diskPct.value, metrics_retention_days: retentionDays.value },
    })
    message.success('OK')
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  }
}

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
onMounted(() => {
  load()
  loadSettings()
})

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
    title: t('services.actions'), key: 'actions', width: 110,
    render: (row) =>
      rowActions([
        { icon: CreateOutline, tip: t('common.edit'), onClick: () => openEdit(row) },
        { icon: TrashOutline, tip: t('common.remove'), type: 'error', onClick: () => remove(row) },
      ]),
  },
])
</script>

<template>
  <AppLayout>
    <n-card :title="t('nav.settings')" size="small">
      <n-tabs type="line">
        <n-tab-pane name="general" :tab="t('settings.general')">
          <n-space vertical size="large" class="general">
            <!-- Смена пароля -->
            <n-card :title="t('settings.password')" size="small">
              <n-form label-placement="top">
                <n-form-item :label="t('settings.oldPassword')">
                  <n-input v-model:value="pwForm.old" type="password" show-password-on="click" />
                </n-form-item>
                <n-form-item :label="t('settings.newPassword')">
                  <n-input v-model:value="pwForm.new" type="password" show-password-on="click" />
                </n-form-item>
                <n-button type="primary" @click="changePassword">{{ t('common.save') }}</n-button>
              </n-form>
            </n-card>

            <!-- Мониторинг: пороги алертов + ретенция истории (применяется на лету) -->
            <n-card :title="t('settings.monitoring')" size="small">
              <n-form label-placement="top">
                <n-form-item :label="t('settings.diskThreshold')">
                  <n-input-number v-model:value="diskPct" :min="50" :max="99" class="num" />
                </n-form-item>
                <n-form-item :label="t('settings.retention')">
                  <n-input-number v-model:value="retentionDays" :min="1" :max="90" class="num">
                    <template #suffix>{{ t('settings.days') }}</template>
                  </n-input-number>
                </n-form-item>
                <n-button type="primary" @click="saveThreshold">{{ t('common.save') }}</n-button>
              </n-form>
            </n-card>

            <!-- Ротация agent-token: только через Docker CLI -->
            <n-alert type="info" :title="t('settings.tokenRotation')">
              <pre class="cli">docker secret rm agent_token
openssl rand -hex 32 | docker secret create agent_token -
docker service update --secret-rm agent_token --secret-add agent_token dsms_panel
docker service update --secret-rm agent_token --secret-add agent_token dsms_agent</pre>
            </n-alert>
          </n-space>
        </n-tab-pane>
        <n-tab-pane name="registries" :tab="t('settings.registries')">
          <n-space vertical>
            <n-space justify="end">
              <n-button size="small" type="primary" @click="openCreate">
                <template #icon><n-icon :component="AddOutline" /></template>
                {{ t('settings.addRegistry') }}
              </n-button>
            </n-space>
            <n-data-table :columns="columns" :data="registries" size="small" :bordered="false" />
          </n-space>
        </n-tab-pane>
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
/* Компактный модал формы реестра и блоки General */
.reg-modal {
  max-width: 480px;
}
/* Формы настроек: разумная ширина, по центру — не липнут к левому краю */
.general {
  max-width: 720px;
  margin: 0 auto;
}
.num {
  width: 200px;
}
.cli {
  font-size: 12px;
  overflow-x: auto;
  white-space: pre-wrap;
}
</style>
