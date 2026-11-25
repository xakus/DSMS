<script setup lang="ts">
// Подтверждение удаления стека вводом его имени (FR-08 3.8.3).
// Общее для списка стеков и страницы стека.
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NModal, NInput, NFormItem } from 'naive-ui'

const props = defineProps<{
  /** Стек к удалению; null — окно закрыто. */
  stack: { name: string; services: number } | null
}>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', name: string): void
}>()

const { t } = useI18n()
const typed = ref('')

// Каждое открытие — с пустым полем: имя прошлого стека не должно «подойти».
watch(() => props.stack?.name, () => { typed.value = '' })

function confirm() {
  if (!props.stack || typed.value !== props.stack.name) return false
  emit('confirm', props.stack.name)
  return true
}
</script>

<template>
  <n-modal
    :show="!!stack" preset="dialog" type="error" :title="t('stacks.removeTitle')"
    :positive-text="t('common.remove')" :negative-text="t('common.cancel')"
    :positive-button-props="{ disabled: typed !== stack?.name, type: 'error' }"
    @positive-click="confirm" @negative-click="emit('close')" @close="emit('close')"
    @mask-click="emit('close')" @esc="emit('close')"
  >
    <p>{{ t('stacks.removeHint', { name: stack?.name, count: stack?.services }) }}</p>
    <n-form-item :label="t('stacks.removeLabel')" :show-feedback="false">
      <n-input v-model:value="typed" :placeholder="stack?.name" autocomplete="off" @keyup.enter="confirm" />
    </n-form-item>
  </n-modal>
</template>
