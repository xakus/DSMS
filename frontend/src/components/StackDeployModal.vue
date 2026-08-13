<script setup lang="ts">
// Редактор деплоя стека из файла (FR-14): YAML + .env + ручные переменные,
// кнопка «Проверить» (dry-run превью) и «Развернуть». Один компонент на два
// режима: создание нового стека и редактирование managed-стека (prefill).
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NModal, NCard, NInput, NButton, NSpace, NCheckbox, NAlert, NUpload,
  NDynamicInput, NTabs, NTabPane, NText, NPopover, NIcon, useMessage,
} from 'naive-ui'
import type { UploadFileInfo } from 'naive-ui'
import { HelpCircleOutline } from '@vicons/ionicons5'
import { api, ApiError } from '../api/client'
import type { StackPreview, StackDeployResult, StackSource } from '../types'

const props = defineProps<{
  show: boolean
  editName: string | null // null — создание нового; иначе редактируем стек
}>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'deployed'): void
}>()

const { t } = useI18n()
const message = useMessage()

const name = ref('')
const yaml = ref('')
const dotenv = ref('')
const manual = ref<{ key: string; value: string }[]>([])
const prune = ref(false)
const preview = ref<StackPreview | null>(null)
const error = ref('')
const busy = ref(false)

const isEdit = computed(() => props.editName !== null)

// При открытии — сброс/загрузка исходника для режима редактирования.
watch(
  () => props.show,
  async (open) => {
    if (!open) return
    preview.value = null
    error.value = ''
    prune.value = false
    if (props.editName) {
      name.value = props.editName
      await loadSource(props.editName)
    } else {
      name.value = ''
      yaml.value = ''
      dotenv.value = ''
      manual.value = []
    }
  },
)

async function loadSource(stack: string) {
  try {
    const src = await api<StackSource>(`/stacks/${stack}/source`)
    yaml.value = src.compose_yaml
    dotenv.value = src.env ?? ''
    manual.value = Object.entries(src.env_vars ?? {}).map(([key, value]) => ({ key, value }))
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : String(e)
  }
}

// Загрузка .yml-файла: читаем текст в поле YAML, на сервер не отправляем.
function onFile(data: { file: UploadFileInfo }): boolean {
  const f = data.file.file
  if (!f) return false
  const reader = new FileReader()
  reader.onload = () => { yaml.value = String(reader.result ?? '') }
  reader.readAsText(f)
  return false // отменяем реальную загрузку
}

/** Собрать тело запроса из полей формы. */
function body() {
  const env_vars: Record<string, string> = {}
  for (const p of manual.value) if (p.key) env_vars[p.key] = p.value
  return {
    name: name.value.trim(),
    compose_yaml: yaml.value,
    env: dotenv.value,
    env_vars,
    prune: prune.value,
  }
}

async function validate() {
  error.value = ''
  preview.value = null
  busy.value = true
  try {
    preview.value = await api<StackPreview>('/stacks/validate', { method: 'POST', body: body() })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function deploy() {
  error.value = ''
  busy.value = true
  try {
    const path = isEdit.value ? `/stacks/${props.editName}` : '/stacks'
    const method = isEdit.value ? 'PUT' : 'POST'
    const res = await api<StackDeployResult>(path, { method, body: body() })
    const c = res.created?.length ?? 0
    const u = res.updated?.length ?? 0
    const r = res.removed?.length ?? 0
    const f = res.failed?.length ?? 0
    if (f > 0) {
      // Часть сервисов не поднялась — показываем детали, окно не закрываем.
      error.value = res.failed!.map((x) => `${x.name}: ${x.error}`).join('\n')
      message.warning(t('stackDeploy.partial', { created: c, updated: u, removed: r, failed: f }))
    } else {
      message.success(t('stackDeploy.done', { created: c, updated: u, removed: r }))
      emit('deployed')
      emit('update:show', false)
    }
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

const canDeploy = computed(() => yaml.value.trim() !== '' && (isEdit.value || name.value.trim() !== ''))
</script>

<template>
  <n-modal :show="show" @update:show="(v: boolean) => emit('update:show', v)">
    <n-card
      class="deploy-card" :title="isEdit ? t('stackDeploy.editTitle', { name: editName }) : t('stackDeploy.newTitle')"
      :bordered="false" size="small" role="dialog" closable @close="emit('update:show', false)"
    >
      <n-space vertical size="large">
        <!-- Подсказка: как указывать env-переменные, чтобы интерполяция сработала. -->
        <n-popover trigger="click" placement="bottom-start" style="max-width: 480px">
          <template #trigger>
            <n-button text type="primary" size="small">
              <template #icon><n-icon :component="HelpCircleOutline" /></template>
              {{ t('stackDeploy.help.button') }}
            </n-button>
          </template>
          <div class="help-body">
            <p class="help-lead">{{ t('stackDeploy.help.intro') }}</p>

            <p><b>{{ t('stackDeploy.help.step1') }}</b></p>
            <pre class="help-code">image: ${REGISTRY}/app:${TAG:-latest}</pre>

            <p><b>{{ t('stackDeploy.help.step2') }}</b></p>
            <pre class="help-code">REGISTRY=myuser
TAG=v3</pre>

            <p><b>{{ t('stackDeploy.help.step3') }}</b></p>
            <pre class="help-code">image: myuser/app:v3</pre>

            <ul class="help-tips">
              <li>{{ t('stackDeploy.help.tipDefault') }}</li>
              <li>{{ t('stackDeploy.help.tipOverride') }}</li>
              <li>{{ t('stackDeploy.help.tipMissing') }}</li>
            </ul>
          </div>
        </n-popover>

        <!-- Имя стека — только при создании (при правке фиксировано). -->
        <n-input
          v-if="!isEdit" v-model:value="name" :placeholder="t('stackDeploy.namePlaceholder')"
        />

        <n-tabs type="line" animated>
          <!-- YAML -->
          <n-tab-pane name="yaml" :tab="t('stackDeploy.composeTab')">
            <n-space vertical>
              <n-upload :show-file-list="false" accept=".yml,.yaml" :on-before-upload="onFile">
                <n-button size="small" secondary>{{ t('stackDeploy.uploadFile') }}</n-button>
              </n-upload>
              <n-input
                v-model:value="yaml" type="textarea" :placeholder="t('stackDeploy.composePlaceholder')"
                :autosize="{ minRows: 12, maxRows: 22 }" class="mono"
              />
            </n-space>
          </n-tab-pane>

          <!-- Переменные окружения -->
          <n-tab-pane name="env" :tab="t('stackDeploy.envTab')">
            <n-space vertical size="large">
              <div>
                <n-text depth="3">{{ t('stackDeploy.dotenvHint') }}</n-text>
                <n-input
                  v-model:value="dotenv" type="textarea" placeholder="KEY=value"
                  :autosize="{ minRows: 5, maxRows: 12 }" class="mono"
                />
              </div>
              <div>
                <n-text depth="3">{{ t('stackDeploy.manualHint') }}</n-text>
                <n-dynamic-input
                  v-model:value="manual" :on-create="() => ({ key: '', value: '' })"
                >
                  <template #default="{ value }">
                    <n-space align="center" :wrap="false" style="width: 100%">
                      <n-input v-model:value="value.key" placeholder="KEY" />
                      <n-input v-model:value="value.value" placeholder="value" />
                    </n-space>
                  </template>
                </n-dynamic-input>
              </div>
            </n-space>
          </n-tab-pane>
        </n-tabs>

        <!-- Prune — опасная опция, по умолчанию выключена (3.14.6). -->
        <n-checkbox v-model:checked="prune">{{ t('stackDeploy.prune') }}</n-checkbox>

        <!-- Превью валидации -->
        <n-alert v-if="preview" type="success" :title="t('stackDeploy.previewTitle')">
          <div>{{ t('stackDeploy.previewNetworks') }}: {{ preview.networks?.join(', ') || '—' }}</div>
          <ul class="preview-list">
            <li v-for="s in preview.services" :key="s.name">
              <b>{{ s.name }}</b> — {{ s.image }}
              <span v-if="s.mode === 'global'">(global)</span>
              <span v-else>({{ s.replicas }})</span>
            </li>
          </ul>
        </n-alert>

        <!-- Ошибки парсинга/деплоя -->
        <n-alert v-if="error" type="error" :title="t('stackDeploy.errorTitle')">
          <pre class="err">{{ error }}</pre>
        </n-alert>
      </n-space>

      <template #footer>
        <n-space justify="end">
          <n-button :disabled="busy" @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
          <n-button :loading="busy" :disabled="!canDeploy" @click="validate">
            {{ t('stackDeploy.validate') }}
          </n-button>
          <n-button type="primary" :loading="busy" :disabled="!canDeploy" @click="deploy">
            {{ isEdit ? t('stackDeploy.update') : t('stackDeploy.deploy') }}
          </n-button>
        </n-space>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.deploy-card {
  width: min(860px, 94vw);
  max-height: 92vh;
  overflow: auto;
}
/* Моноширинный ввод для YAML/env — читаемее */
.mono :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
}
.preview-list {
  margin: 6px 0 0;
  padding-left: 18px;
}
.err {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
}
/* Справка по env-переменным */
.help-body {
  font-size: 13px;
  line-height: 1.5;
}
.help-body p {
  margin: 8px 0 4px;
}
.help-lead {
  margin-top: 0;
  opacity: 0.8;
}
.help-code {
  margin: 0 0 4px;
  padding: 6px 8px;
  border-radius: 6px;
  background: rgba(128, 128, 128, 0.14);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12.5px;
  white-space: pre-wrap;
  word-break: break-word;
}
.help-tips {
  margin: 8px 0 0;
  padding-left: 18px;
}
.help-tips li {
  margin: 3px 0;
  opacity: 0.85;
}
</style>
