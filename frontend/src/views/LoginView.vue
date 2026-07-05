<script setup lang="ts">
// Экран входа (разд. 6.2 ТЗ: минимальный, лого + форма).
// При первом запуске панель сообщает needs_setup → редирект на /setup.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui'
import { api } from '../api/client'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const { t } = useI18n()
const message = useMessage()
const auth = useAuthStore()

const username = ref('')
const password = ref('')
const loading = ref(false)

// Проверка первичной настройки: нет пользователей → setup-экран (3.7.1).
onMounted(async () => {
  try {
    const st = await api<{ needs_setup: boolean }>('/setup')
    if (st.needs_setup) router.replace({ name: 'setup' })
  } catch {
    /* панель недоступна — форма останется, ошибку покажет submit */
  }
})

/** Отправить форму входа. */
async function submit() {
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    router.replace({ name: 'dashboard' })
  } catch {
    message.error(t('login.failed'))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <n-card :title="t('login.title')" class="login-card">
      <n-form @submit.prevent="submit">
        <n-form-item :label="t('login.username')">
          <n-input v-model:value="username" autofocus />
        </n-form-item>
        <n-form-item :label="t('login.password')">
          <n-input v-model:value="password" type="password" show-password-on="click" @keyup.enter="submit" />
        </n-form-item>
        <n-button type="primary" block :loading="loading" attr-type="submit">
          {{ t('login.submit') }}
        </n-button>
      </n-form>
    </n-card>
  </div>
</template>

<style scoped>
/* Центрирование карточки входа на весь экран */
.login-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
}
.login-card {
  width: 360px;
}
</style>
