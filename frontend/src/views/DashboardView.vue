<script setup lang="ts">
// Dashboard кластера (FR-01): сводка + сетка карточек нод,
// живые обновления по WS, модал Add Node с join-командами (3.3.4).
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NCard, NStatistic, NSpace, NButton, NGrid, NGi, NModal,
  NInput, NTabs, NTabPane, useMessage, useDialog,
} from 'naive-ui'
import AppLayout from '../components/AppLayout.vue'
import NodeCard from '../components/NodeCard.vue'
import { api } from '../api/client'
import { useMetricsStore } from '../stores/metrics'
import type { ClusterSummary, JoinTokens, NodeInfo } from '../types'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const metrics = useMetricsStore()

const summary = ref<ClusterSummary | null>(null)
const nodes = ref<NodeInfo[]>([])
const showAddNode = ref(false)
const tokens = ref<JoinTokens | null>(null)

/** Обновить сводку и список нод. */
async function refresh() {
  try {
    ;[summary.value, nodes.value] = await Promise.all([
      api<ClusterSummary>('/cluster'),
      api<NodeInfo>('/nodes') as unknown as Promise<NodeInfo[]>,
    ])
    // Затравка метрик из REST-ответа, дальше — живые по WS.
    for (const n of nodes.value) {
      if (n.metrics) metrics.latest.set(n.id, n.metrics)
    }
  } catch {
    message.error(t('common.loadFailed'))
  }
}

onMounted(() => {
  metrics.start()
  refresh()
  // Периодическое обновление состава нод (join/remove) — раз в 15с.
  window.setInterval(refresh, 15000)
})

/** Открыть модал Add Node: подтянуть актуальные join-токены. */
async function openAddNode() {
  tokens.value = await api<JoinTokens>('/swarm/join-tokens')
  showAddNode.value = true
}

/** Команда join для роли. */
function joinCmd(role: 'worker' | 'manager'): string {
  if (!tokens.value) return ''
  const token = role === 'worker' ? tokens.value.worker : tokens.value.manager
  return `docker swarm join --token ${token} ${tokens.value.manager_addr}`
}

/** Скопировать команду в буфер обмена (кнопка Copy, 3.3.4). */
async function copy(text: string) {
  await navigator.clipboard.writeText(text)
  message.success(t('common.copied'))
}

/** Ротация join-токенов с подтверждением. */
function rotate() {
  dialog.warning({
    title: t('nodes.rotateTitle'),
    content: t('nodes.rotateConfirm'),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      await api('/swarm/join-tokens/rotate', { method: 'POST' })
      tokens.value = await api<JoinTokens>('/swarm/join-tokens')
      message.success(t('nodes.rotated'))
    },
  })
}
</script>

<template>
  <AppLayout>
    <n-space justify="space-between" align="center" class="mb">
      <h2>{{ t('dashboard.title') }}</h2>
      <n-button type="primary" @click="openAddNode">+ {{ t('nodes.addNode') }}</n-button>
    </n-space>

    <!-- Сводка кластера (3.1.3) -->
    <n-space v-if="summary" class="mb">
      <n-card><n-statistic :label="t('dashboard.nodes')" :value="`${summary.nodes_ready}/${summary.nodes}`" /></n-card>
      <n-card><n-statistic :label="t('dashboard.services')" :value="summary.services" /></n-card>
    </n-space>

    <!-- Сетка карточек нод (3.1.1) -->
    <n-grid cols="1 s:2 m:3 l:4" responsive="screen" :x-gap="12" :y-gap="12">
      <n-gi v-for="n in nodes" :key="n.id">
        <NodeCard :node="n" />
      </n-gi>
    </n-grid>

    <!-- Модал Add Node (3.3.4) -->
    <n-modal v-model:show="showAddNode" preset="card" :title="t('nodes.addNode')" class="add-modal">
      <n-tabs type="line">
        <n-tab-pane v-for="role in (['worker', 'manager'] as const)" :key="role" :name="role" :tab="role">
          <n-space vertical>
            <n-input :value="joinCmd(role)" type="textarea" readonly :autosize="{ minRows: 2 }" />
            <n-space>
              <n-button size="small" @click="copy(joinCmd(role))">📋 {{ t('common.copy') }}</n-button>
            </n-space>
          </n-space>
        </n-tab-pane>
      </n-tabs>
      <template #footer>
        <n-button quaternary type="warning" size="small" @click="rotate">
          🔄 {{ t('nodes.rotateTokens') }}
        </n-button>
      </template>
    </n-modal>
  </AppLayout>
</template>

<style scoped>
/* Вертикальные отступы между блоками */
.mb {
  margin-bottom: 16px;
}
.add-modal {
  max-width: 640px;
}
</style>
