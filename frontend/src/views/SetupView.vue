<script setup lang="ts">
// Экран первичной настройки: создание admin-аккаунта (FR-07 3.7.1).
// Тот же стиль, что и вход — через оболочку AuthShell.
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NForm, NFormItem, NInput, NButton, NIcon, useMessage } from 'naive-ui'
import { PersonOutline, LockClosedOutline } from '@vicons/ionicons5'
import AuthShell from '../components/AuthShell.vue'
import { api, ApiError } from '../api/client'

const router = useRouter()
const { t } = useI18n()
const message = useMessage()

const username = ref('')
const password = ref('')
const loading = ref(false)

/** Создать admin и уйти на экран входа. */
async function submit() {
  loading.value = true
  try {
    await api('/setup', { method: 'POST', body: { username: username.value, password: password.value } })
    router.replace({ name: 'login' })
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'error')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthShell :subtitle="t('setup.title')">
    <p class="hint">{{ t('setup.hint') }}</p>
    <n-form @submit.prevent="submit">
      <n-form-item :label="t('login.username')">
        <n-input v-model:value="username" size="large" autofocus :placeholder="t('login.username')">
          <template #prefix><n-icon :component="PersonOutline" /></template>
        </n-input>
      </n-form-item>
      <n-form-item :label="t('login.password')">
        <n-input
          v-model:value="password" type="password" size="large" show-password-on="click"
          :placeholder="t('login.password')" @keyup.enter="submit"
        >
          <template #prefix><n-icon :component="LockClosedOutline" /></template>
        </n-input>
      </n-form-item>
      <n-button type="primary" size="large" block :loading="loading" attr-type="submit">
        {{ t('setup.submit') }}
      </n-button>
    </n-form>
  </AuthShell>
</template>

<style scoped>
.hint {
  text-align: center;
  font-size: 14px;
  opacity: 0.7;
  margin: -12px 0 20px;
}
</style>
