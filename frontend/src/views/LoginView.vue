<script setup lang="ts">
// Экран входа (разд. 6.2 ТЗ): оболочка AuthShell (анимированный фон + карточка).
// При первом запуске панель сообщает needs_setup → редирект на /setup (3.7.1).
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NForm, NFormItem, NInput, NButton, NIcon, useMessage } from 'naive-ui'
import { PersonOutline, LockClosedOutline } from '@vicons/ionicons5'
import AuthShell from '../components/AuthShell.vue'
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
  <AuthShell>
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
      <n-button type="primary" size="large" block :loading="loading" attr-type="submit" class="submit-btn">
        {{ t('login.submit') }}
      </n-button>
    </n-form>
  </AuthShell>
</template>

<style scoped>
.submit-btn {
  margin-top: 6px;
}
</style>
