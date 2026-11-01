<script setup lang="ts">
import AppButton from './AppButton.vue'
import ModalDialog from './ModalDialog.vue'

const open = defineModel<boolean>('open', { required: true })

withDefaults(
  defineProps<{
    title: string
    message?: string
    confirmLabel?: string
    cancelLabel?: string
    tone?: 'danger' | 'primary'
    busy?: boolean
  }>(),
  {
    confirmLabel: 'Confirm',
    cancelLabel: 'Cancel',
    tone: 'danger',
    busy: false,
  },
)

const emit = defineEmits<{ confirm: [] }>()
</script>

<template>
  <ModalDialog
    v-model:open="open"
    :title="title"
    :description="message"
    size="sm"
    :dismissible="!busy"
  >
    <div v-if="$slots.default" class="text-sm text-muted">
      <slot />
    </div>
    <template #footer>
      <AppButton variant="secondary" :disabled="busy" @click="open = false">
        {{ cancelLabel }}
      </AppButton>
      <AppButton
        :variant="tone === 'danger' ? 'danger' : 'primary'"
        :loading="busy"
        @click="emit('confirm')"
      >
        {{ confirmLabel }}
      </AppButton>
    </template>
  </ModalDialog>
</template>
