<script setup lang="ts">
// Dashboard-каркас (FR-01): сводка кластера. Карточки нод с графиками — этап 2.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NStatistic, NSpace, NButton } from 'naive-ui'
import { api } from '../api/client'
import { useAuthStore } from '../stores/auth'

/** Ответ /cluster: сводные показатели (3.1.3). */
interface ClusterSummary {
  nodes: number
  nodes_ready: number
  services: number
}

const router = useRouter()
const { t } = useI18n()
const auth = useAuthStore()
const summary = ref<ClusterSummary | null>(null)

onMounted(async () => {
  try {
    summary.value = await api<ClusterSummary>('/cluster')
  } catch {
    /* нет доступа к Docker API — сводка останется пустой */
  }
})

/** Выйти из панели. */
async function logout() {
  await auth.logout()
  router.replace({ name: 'login' })
}
</script>

<template>
  <div class="dash">
    <n-space justify="space-between" align="center" class="dash-header">
      <h2>{{ t('dashboard.title') }} — {{ auth.username }}</h2>
      <n-button quaternary @click="logout">{{ t('dashboard.logout') }}</n-button>
    </n-space>

    <n-space v-if="summary">
      <n-card><n-statistic :label="t('dashboard.nodes')" :value="`${summary.nodes_ready}/${summary.nodes}`" /></n-card>
      <n-card><n-statistic :label="t('dashboard.services')" :value="summary.services" /></n-card>
    </n-space>
  </div>
</template>

<style scoped>
/* Отступы контента дашборда */
.dash {
  padding: 16px 24px;
}
.dash-header {
  margin-bottom: 16px;
}
</style>
