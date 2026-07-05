<script setup lang="ts">
// Экран первичной настройки: создание admin-аккаунта (FR-07 3.7.1).
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui'
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
  <div class="setup-wrap">
    <n-card :title="t('setup.title')" class="setup-card">
      <p>{{ t('setup.hint') }}</p>
      <n-form @submit.prevent="submit">
        <n-form-item :label="t('login.username')">
          <n-input v-model:value="username" autofocus />
        </n-form-item>
        <n-form-item :label="t('login.password')">
          <n-input v-model:value="password" type="password" show-password-on="click" />
        </n-form-item>
        <n-button type="primary" block :loading="loading" attr-type="submit">
          {{ t('setup.submit') }}
        </n-button>
      </n-form>
    </n-card>
  </div>
</template>

<style scoped>
/* Центрирование карточки настройки на весь экран */
.setup-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
}
.setup-card {
  width: 360px;
}
</style>
