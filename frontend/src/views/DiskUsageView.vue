<script setup lang="ts">
// Disk usage (FR-11, разд. 6.2 экран 9): docker system df по нодам
// + кнопки prune с предпросмотром освобождаемого места и подтверждением.
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NCard, NGrid, NGi, NButton, NSpace, NCheckbox, NTag, useMessage, useDialog,
} from 'naive-ui'
import AppLayout from '../components/AppLayout.vue'
import { api, ApiError } from '../api/client'
import { fmtBytes } from '../utils/format'

/** Секция df: count/size/reclaimable. */
interface DFSection { count: number; size: number; reclaimable: number }
/** df одной ноды. */
interface NodeDF {
  node: string
  node_id: string
  df?: { images: DFSection; containers: DFSection; volumes: DFSection; build_cache: DFSection }
  err?: string
}
/** Результат prune. */
interface PruneResult { target: string; space_reclaimed: number; err?: string }

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

const nodes = ref<NodeDF[]>([])
const loading = ref(false)
/** «Все неиспользуемые образы» вместо только dangling (3.11.2). */
const allImages = ref(false)

async function load() {
  loading.value = true
  try {
    nodes.value = await api<NodeDF[]>('/system/df')
  } catch {
    message.error(t('common.loadFailed'))
  } finally {
    loading.value = false
  }
}
onMounted(load)

/** Секции для отображения. */
const sections = [
  { key: 'images', label: 'Images', target: 'images' },
  { key: 'containers', label: 'Containers', target: 'containers' },
  { key: 'volumes', label: 'Volumes', target: 'volumes' },
  { key: 'build_cache', label: 'Build cache', target: 'build-cache' },
] as const

/** Prune одной цели на ноде: подтверждение с предпросмотром (3.11.2). */
function prune(node: NodeDF, target: string, reclaimable: number) {
  dialog.warning({
    title: t('disk.pruneConfirm', { target, node: node.node }),
    content: t('disk.pruneHint', { size: fmtBytes(reclaimable) }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        const res = await api<PruneResult[]>('/system/prune', {
          method: 'POST',
          body: { node_id: node.node_id, targets: [target], all_images: allImages.value },
        })
        const freed = res.reduce((a, r) => a + (r.space_reclaimed ?? 0), 0)
        message.success(t('disk.pruned', { size: fmtBytes(freed) }))
        await load()
      } catch (e) {
        message.error(e instanceof ApiError ? e.message : 'error')
      }
    },
  })
}
</script>

<template>
  <AppLayout>
    <n-space justify="space-between" align="center" class="mb">
      <h2>{{ t('nav.disk') }}</h2>
      <n-space align="center">
        <n-checkbox v-model:checked="allImages">{{ t('disk.allImages') }}</n-checkbox>
        <n-button size="small" :loading="loading" @click="load">↻</n-button>
      </n-space>
    </n-space>

    <n-grid cols="1 m:2" responsive="screen" :x-gap="12" :y-gap="12">
      <n-gi v-for="n in nodes" :key="n.node_id">
        <n-card :title="n.node || n.node_id" size="small">
          <n-tag v-if="n.err" type="error" :bordered="false">{{ n.err }}</n-tag>
          <template v-else-if="n.df">
            <div v-for="s in sections" :key="s.key" class="df-row">
              <span class="df-label">{{ s.label }} ({{ n.df[s.key].count }})</span>
              <span class="df-size">{{ fmtBytes(n.df[s.key].size) }}</span>
              <span class="df-rec">{{ t('disk.reclaimable') }}: {{ fmtBytes(n.df[s.key].reclaimable) }}</span>
              <n-button size="tiny" type="warning" :disabled="n.df[s.key].reclaimable === 0"
                        @click="prune(n, s.target, n.df[s.key].reclaimable)">
                🧹 Prune
              </n-button>
            </div>
          </template>
        </n-card>
      </n-gi>
    </n-grid>
    <p v-if="!nodes.length && !loading" class="empty">{{ t('disk.noAgents') }}</p>
  </AppLayout>
</template>

<style scoped>
/* Строки категорий df */
.mb {
  margin-bottom: 16px;
}
.df-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 0;
  border-bottom: 1px solid rgba(128, 128, 128, 0.12);
}
.df-label {
  flex: 1;
  font-weight: 500;
}
.df-size {
  width: 90px;
  text-align: right;
}
.df-rec {
  width: 190px;
  font-size: 12px;
  opacity: 0.7;
}
.empty {
  opacity: 0.6;
  text-align: center;
  margin-top: 48px;
}
</style>
