<script setup lang="ts">
// Стеки (FR-08): список с агрегированным статусом и потреблением,
// действия над стеком целиком — остановить, запустить, деплой, передеплой,
// удаление (двойное подтверждение имени).
import { computed, h, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NDataTable, NTag, NButton, NIcon, NFlex } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import {
  RefreshOutline, TrashOutline, CloudDownloadOutline, DocumentTextOutline, AddOutline,
  PlayOutline, PauseOutline,
} from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import StackDeployModal from '../components/StackDeployModal.vue'
import StackRemoveModal from '../components/StackRemoveModal.vue'
import { rowActions } from '../utils/actions'
import { usageColumns, USAGE_COLS_WIDTH } from '../utils/usage'
import { stackStatus } from '../utils/stackOps'
import { api } from '../api/client'
import { usePolling } from '../composables/usePolling'
import { useStackActions } from '../composables/useStackActions'
import { useMediaQuery } from '../composables/useMediaQuery'
import type { StackInfo } from '../types'

/** Псевдо-стек для сервисов без стека: действий над ним нет (имя задаёт сервер). */
const NO_STACK = '(no stack)'

/**
 * Ширины колонок, px. Действий шесть: 6·28 + 5·6 + 24 (поля ячейки) = 222.
 * Вместе с потреблением таблица укладывается в 1280 px без прокрутки.
 */
const COL = { name: 160, services: 100, replicas: 130, actions: 225 } as const

/** Минимальная ширина таблицы: уже — прокрутка внутри карточки, а не страницы. */
const TABLE_MIN_WIDTH = COL.name + COL.services + COL.replicas + USAGE_COLS_WIDTH + COL.actions

/** С какой ширины экрана колонка действий закрепляется справа. */
const PIN_ACTIONS_QUERY = '(min-width: 1024px)'

const { t } = useI18n()

const stacks = ref<StackInfo[]>([])
const { loading, refresh } = usePolling(async () => {
  stacks.value = await api<StackInfo[]>('/stacks')
})
const actions = useStackActions(refresh)
const pinActions = useMediaQuery(PIN_ACTIONS_QUERY)

// Модал деплоя из файла (FR-14): editName=null — новый стек, иначе правка.
const deployShow = ref(false)
const deployEditName = ref<string | null>(null)
function openDeployNew() {
  deployEditName.value = null
  deployShow.value = true
}
function openDeployEdit(stack: StackInfo) {
  deployEditName.value = stack.name
  deployShow.value = true
}

// Удаление стека с вводом имени (3.8.3)
const removeTarget = ref<StackInfo | null>(null)
function applyRemove(name: string) {
  removeTarget.value = null
  void actions.remove(name)
}

const columns = computed<DataTableColumns<StackInfo>>(() => [
  {
    title: t('stacks.name'), key: 'name', minWidth: COL.name,
    render: (row) => {
      // Псевдо-стек — просто подпись: страницы у него нет.
      if (row.name === NO_STACK) return t('stacks.noStack')
      const link = h(RouterLink, { to: { name: 'stack', params: { name: row.name } }, class: 'row-link' },
        { default: () => row.name })
      if (!row.managed) return link
      return h(NFlex, { align: 'center', size: 8, wrap: false }, {
        default: () => [
          link,
          h(NTag, { size: 'tiny', bordered: false, type: 'info' }, { default: () => t('stacks.fromFile') }),
        ],
      })
    },
  },
  { title: t('stacks.services'), key: 'services', width: COL.services },
  {
    title: t('services.replicas'), key: 'replicas', width: COL.replicas,
    render: (row) => {
      const st = stackStatus(row)
      const text = st.label ? t(st.label) : `${row.running}/${row.desired}`
      const tag = h(NTag, { size: 'small', bordered: false, type: st.type }, { default: () => text })
      // Остановлена только часть стека (остальное работает, например
      // global-сервисы): числа зелёные, но об остановленных надо сказать.
      if (st.label || row.stopped === 0) return tag
      return h(NFlex, { vertical: true, size: 4, align: 'flex-start' }, {
        default: () => [
          tag,
          h(NTag, { size: 'tiny', bordered: false }, { default: () => t('stacks.stoppedCount', { count: row.stopped }) }),
        ],
      })
    },
  },
  ...usageColumns<StackInfo>(t),
  {
    title: t('services.actions'), key: 'actions', width: COL.actions,
    fixed: pinActions.value ? 'right' : undefined,
    render: (row) => row.name === NO_STACK ? null :
      rowActions([
        // Только для managed-стеков: правка исходника + передеплой из файла.
        ...(row.managed
          ? [{ icon: DocumentTextOutline, tip: t('stacks.editFileTip'), onClick: () => openDeployEdit(row) }]
          : []),
        {
          icon: PlayOutline, tip: t('stacks.startTip'), type: 'success' as const,
          disabled: row.stopped === 0, onClick: () => { void actions.start(row.name) },
        },
        {
          icon: PauseOutline, tip: t('stacks.stopTip'), type: 'warning' as const,
          disabled: row.stoppable === 0, onClick: () => actions.stop(row.name),
        },
        {
          icon: CloudDownloadOutline, tip: t('stacks.deployTip'), type: 'primary' as const,
          disabled: row.services === 0, onClick: () => actions.deploy(row.name),
        },
        {
          icon: RefreshOutline, tip: t('stacks.redeployTip'),
          disabled: row.services === 0, onClick: () => actions.redeploy(row.name),
        },
        { icon: TrashOutline, tip: t('common.remove'), type: 'error' as const, onClick: () => { removeTarget.value = row } },
      ]),
  },
])
</script>

<template>
  <AppLayout>
    <n-card :title="t('nav.stacks')" size="small">
      <template #header-extra>
        <n-button type="primary" size="small" @click="openDeployNew">
          <template #icon><n-icon :component="AddOutline" /></template>
          {{ t('stackDeploy.newButton') }}
        </n-button>
      </template>
      <n-data-table
        :columns="columns" :data="stacks" :loading="loading"
        size="small" :bordered="false" :row-key="(r: StackInfo) => r.name"
        :scroll-x="TABLE_MIN_WIDTH"
      >
        <template #empty>
          <n-flex vertical align="center" :size="12">
            <span>{{ t('stacks.empty') }}</span>
            <n-button size="small" type="primary" @click="openDeployNew">{{ t('stackDeploy.newButton') }}</n-button>
          </n-flex>
        </template>
      </n-data-table>
    </n-card>

    <!-- Деплой/правка стека из файла (FR-14) -->
    <StackDeployModal
      v-model:show="deployShow" :edit-name="deployEditName" @deployed="refresh"
    />

    <!-- Удаление стека: ввод имени (3.8.3) -->
    <StackRemoveModal :stack="removeTarget" @close="removeTarget = null" @confirm="applyRemove" />
  </AppLayout>
</template>

<style scoped>
/* Имя стека — ссылка на его страницу, цветом текста (как имена сервисов) */
:deep(.row-link) {
  color: inherit;
}
</style>
