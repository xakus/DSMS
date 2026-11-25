<script setup lang="ts">
// Disk usage (FR-11, разд. 6.2 экран 9): занятое место по нодам и кнопки
// очистки. «Освободится» считает агент — ровно то, что удалит очистка
// (ТЗ 3.11.5): общие слои образов один раз, именованные тома не в счёт.
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NCard, NGrid, NGi, NButton, NFlex, NCheckbox, NTag, NIcon, NText, NSkeleton,
  useMessage, useDialog,
} from 'naive-ui'
import { RefreshOutline, SparklesOutline } from '@vicons/ionicons5'
import AppLayout from '../components/AppLayout.vue'
import { api, ApiError } from '../api/client'
import { fmtBytes } from '../utils/format'

/** Категория disk usage. Размеры — в байтах. */
interface DFSection {
  count: number
  /** Занято на диске (общие слои — один раз). */
  size: number
  /** Сколько объектов удалит «Очистить» (образы — только без тега). */
  prunable: number
  /** Сколько места «Очистить» освободит сразу. */
  reclaimable: number
  /** Образы: то же при флажке «все неиспользуемые». */
  prunable_all?: number
  reclaimable_all?: number
  /** Образы: сколько ещё освободит «Очистить всё» (слои держат контейнеры и кэш сборки). */
  later?: number
  later_all?: number
  /** Образы: оценка — нижняя граница, а не точное число. */
  approx?: boolean
  /** Тома: неиспользуемые именованные — очистка их не удаляет. */
  kept?: number
}
type SectionKey = 'images' | 'containers' | 'volumes' | 'build_cache'
/** Оценка полной очистки ноды, байты. */
interface DFTotal { reclaimable: number; reclaimable_all: number; approx?: boolean }
/** df одной ноды. */
interface NodeDF {
  node: string
  node_id: string
  df?: Record<SectionKey, DFSection> & { total: DFTotal }
  err?: string
}
/** Результат prune по одной цели. */
interface PruneResult { target: string; space_reclaimed: number; err?: string }

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

const nodes = ref<NodeDF[]>([])
const loading = ref(false)
/** Первая загрузка ещё не завершилась — показываем скелетон. */
const firstLoad = ref(true)
/** Удалять и образы с тегом — все без контейнеров (3.11.2). */
const allImages = ref(false)
/** Какая очистка сейчас идёт («нода:цель») — кнопка занята, двойной клик невозможен. */
const pruning = ref('')

async function load() {
  loading.value = true
  try {
    nodes.value = await api<NodeDF[]>('/system/df')
  } catch {
    message.error(t('common.loadFailed'))
  } finally {
    loading.value = false
    firstLoad.value = false
  }
}
onMounted(load)

/** Категории: ключ в ответе агента и цель очистки в его API. */
const sections: { key: SectionKey; target: string }[] = [
  { key: 'images', target: 'images' },
  { key: 'containers', target: 'containers' },
  { key: 'volumes', target: 'volumes' },
  { key: 'build_cache', target: 'build-cache' },
]

/** Пометка «очищается вся нода» в ключе идущей очистки. */
const ALL_TARGETS = 'all'

/**
 * Порядок полной очистки (его же выдерживает агент): остановленный контейнер
 * держит свой образ, а слои образа держит кэш сборки.
 */
const PRUNE_ALL_ORDER = ['containers', 'images', 'build-cache', 'volumes']

/** Сколько освободит кнопка «Очистить» с учётом флажка «все образы». */
function reclaimable(key: SectionKey, s: DFSection): number {
  if (key === 'images' && allImages.value) return s.reclaimable_all ?? 0
  return s.reclaimable
}

/** Сколько объектов удалит кнопка «Очистить»: ноль — чистить нечего. */
function prunable(key: SectionKey, s: DFSection): number {
  if (key === 'images' && allImages.value) return s.prunable_all ?? 0
  return s.prunable
}

/** Сколько места образов освободит только полная очистка. */
function later(s: DFSection): number {
  return (allImages.value ? s.later_all : s.later) ?? 0
}

/** Сколько освободит «Очистить всё» на ноде. */
function totalReclaimable(total: DFTotal): number {
  return allImages.value ? total.reclaimable_all : total.reclaimable
}

/** На ноде есть что чистить хотя бы в одной категории. */
function hasPrunable(df: Record<SectionKey, DFSection>): boolean {
  return sections.some((s) => prunable(s.key, df[s.key]) > 0)
}

/** Что именно удалит очистка — словами, для окна подтверждения. */
function whatText(key: SectionKey): string {
  return t(key === 'images' && allImages.value ? 'disk.what.imagesAll' : `disk.what.${key}`)
}

/** Очистка одной категории на ноде: подтверждение с точным числом (3.11.2). */
function prune(node: NodeDF, key: SectionKey, target: string, s: DFSection) {
  const size = fmtBytes(reclaimable(key, s))
  dialog.warning({
    title: t('disk.pruneConfirm', { node: node.node || node.node_id }),
    content: t(s.approx ? 'disk.pruneHintApprox' : 'disk.pruneHint', { what: whatText(key), size }),
    positiveText: t('disk.prune'),
    negativeText: t('common.cancel'),
    // Окно закрываем сразу: удаление больших образов идёт долго, ход виден на кнопке.
    onPositiveClick: () => { void runPrune(node, [target], target) },
  })
}

/**
 * Полная очистка ноды: контейнеры, образы, кэш сборки, анонимные тома.
 * Порядок выдерживает агент — контейнер держит образ, а слои образа держит
 * кэш сборки, поэтому по одной кнопке освобождается меньше.
 */
function pruneAll(node: NodeDF, total: DFTotal) {
  dialog.warning({
    title: t('disk.pruneAllConfirm', { node: node.node || node.node_id }),
    content: t(total.approx ? 'disk.pruneAllHintApprox' : 'disk.pruneAllHint', {
      images: whatText('images'), size: fmtBytes(totalReclaimable(total)),
    }),
    positiveText: t('disk.pruneAll'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => { void runPrune(node, PRUNE_ALL_ORDER, ALL_TARGETS) },
  })
}

/**
 * Выполнить очистку и показать, сколько освободилось на самом деле.
 *
 * @param targets цели очистки для агента
 * @param mark    что показать занятым: цель одной кнопки или ALL_TARGETS
 */
async function runPrune(node: NodeDF, targets: string[], mark: string) {
  pruning.value = `${node.node_id}:${mark}`
  try {
    const res = await api<PruneResult[]>('/system/prune', {
      method: 'POST',
      body: { node_id: node.node_id, targets, all_images: allImages.value },
    })
    const failed = res.find((r) => r.err)
    if (failed) {
      message.error(t('disk.pruneFailed', { err: failed.err }))
    } else {
      const freed = res.reduce((sum, r) => sum + (r.space_reclaimed ?? 0), 0)
      message.success(t('disk.pruned', { size: fmtBytes(freed) }))
    }
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('common.error'))
  } finally {
    pruning.value = ''
    await load()
  }
}
</script>

<template>
  <AppLayout>
    <n-flex justify="space-between" align="center" class="head">
      <h2 class="title">{{ t('nav.disk') }}</h2>
      <n-flex align="center" :size="16">
        <n-checkbox v-model:checked="allImages">{{ t('disk.allImages') }}</n-checkbox>
        <n-button size="small" :loading="loading" @click="load">
          <template #icon><n-icon :component="RefreshOutline" /></template>
          {{ t('disk.refresh') }}
        </n-button>
      </n-flex>
    </n-flex>
    <n-text depth="3" class="explain">{{ t('disk.explain') }}</n-text>

    <!-- Первая загрузка: скелетон вместо пустого экрана -->
    <n-card v-if="firstLoad" size="small">
      <n-skeleton text :repeat="4" />
    </n-card>

    <n-grid v-else cols="1 xxl:2" responsive="screen" :x-gap="16" :y-gap="16">
      <n-gi v-for="n in nodes" :key="n.node_id">
        <n-card :title="n.node || n.node_id" size="small">
          <!-- Полная очистка ноды: одна кнопка в правильном порядке -->
          <template v-if="n.df" #header-extra>
            <n-flex align="center" :size="12" :wrap="false">
              <span class="total">
                <n-text depth="3">
                  {{ t(n.df.total.approx ? 'disk.totalAtLeast' : 'disk.total') }}
                </n-text>
                {{ fmtBytes(totalReclaimable(n.df.total)) }}
              </span>
              <n-button
                size="small" type="warning"
                :disabled="!hasPrunable(n.df) || (pruning !== '' && pruning !== `${n.node_id}:${ALL_TARGETS}`)"
                :loading="pruning === `${n.node_id}:${ALL_TARGETS}`"
                @click="pruneAll(n, n.df.total)"
              >
                <template #icon><n-icon :component="SparklesOutline" /></template>
                {{ t('disk.pruneAll') }}
              </n-button>
            </n-flex>
          </template>
          <n-tag v-if="n.err" type="error" :bordered="false">{{ n.err }}</n-tag>
          <template v-else-if="n.df">
            <div v-for="s in sections" :key="s.key" class="row">
              <div class="line">
                <span class="label">{{ t(`disk.section.${s.key}`) }} ({{ n.df[s.key].count }})</span>
                <span class="num">
                  <n-text depth="3">{{ t('disk.size') }}</n-text>
                  {{ fmtBytes(n.df[s.key].size) }}
                </span>
                <span class="num">
                  <n-text depth="3">
                    {{ t(n.df[s.key].approx ? 'disk.reclaimableAtLeast' : 'disk.reclaimable') }}
                  </n-text>
                  {{ fmtBytes(reclaimable(s.key, n.df[s.key])) }}
                </span>
                <n-button
                  size="small" type="warning" secondary
                  :disabled="prunable(s.key, n.df[s.key]) === 0 || (pruning !== '' && pruning !== `${n.node_id}:${s.target}`)"
                  :loading="pruning === `${n.node_id}:${s.target}`"
                  @click="prune(n, s.key, s.target, n.df[s.key])"
                >
                  <template #icon><n-icon :component="SparklesOutline" /></template>
                  {{ t('disk.prune') }}
                </n-button>
              </div>
              <!-- Почему освободится меньше, чем кажется, и что с этим делать -->
              <n-text v-if="s.key === 'images' && later(n.df[s.key]) > 0" depth="3" class="hint">
                {{ t('disk.laterHint', { size: fmtBytes(later(n.df[s.key])) }) }}
              </n-text>
              <n-text v-if="n.df[s.key].kept" depth="3" class="hint">
                {{ t('disk.keptHint', { size: fmtBytes(n.df[s.key].kept) }) }}
              </n-text>
            </div>
          </template>
        </n-card>
      </n-gi>
    </n-grid>
    <n-text v-if="!firstLoad && !nodes.length" depth="3" class="empty">{{ t('disk.noAgents') }}</n-text>
  </AppLayout>
</template>

<style scoped>
.head {
  margin-bottom: 8px;
}
.title {
  margin: 0;
}
/* Пояснение, как считается «освободится» — ограничено по длине строки */
.explain {
  display: block;
  max-width: 100ch;
  margin-bottom: 16px;
  font-size: 14px;
}
/* Категория: строка с цифрами и кнопкой, под ней — пояснения */
.row {
  padding: 12px 0;
  border-bottom: 1px solid var(--n-border-color);
}
.row:last-child {
  border-bottom: none;
}
/* Узкий экран: название и «занято» в первой строке, «освободится» и кнопка — во второй */
.line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 16px;
}
.label {
  flex: 1 1 160px;
  font-weight: 500;
}
/* Цифры одной ширины и без переноса: при обновлении строка не «прыгает» */
.num {
  flex: 0 0 auto;
  min-width: 152px;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
/* Широкий экран: сетка из четырёх колонок — цифры категорий стоят друг под
   другом, как в таблице. Ширины: «занято 1023 MiB» и «освободится не меньше
   1023 MiB» помещаются целиком. */
@media (min-width: 960px) {
  .line {
    display: grid;
    grid-template-columns: minmax(160px, 1fr) 168px 272px 128px;
  }
  .line .n-button {
    justify-self: end;
  }
}
/* Итог полной очистки в шапке карточки ноды */
.total {
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.hint {
  display: block;
  margin-top: 4px;
  font-size: 14px;
}
.empty {
  display: block;
  text-align: center;
  margin-top: 48px;
}
</style>
