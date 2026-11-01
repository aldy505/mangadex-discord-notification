<script setup lang="ts">
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { computed } from 'vue'

const open = defineModel<boolean>('open', { required: true })

const props = withDefaults(
  defineProps<{
    title: string
    description?: string
    size?: 'sm' | 'md' | 'lg'
    dismissible?: boolean
  }>(),
  { size: 'md', dismissible: true },
)

/** Blocks outside-click and Escape dismissal while an action is in flight. */
function keepOpen(event: Event): void {
  if (!props.dismissible) event.preventDefault()
}

const WIDTH_CLASSES = {
  sm: 'max-w-sm',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
} as const

const contentClasses = computed(() => [
  'fixed left-1/2 top-1/2 z-50 max-h-[90dvh] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2',
  'overflow-y-auto rounded-xl border border-border bg-surface p-5 shadow-2xl focus:outline-none',
  WIDTH_CLASSES[props.size],
])
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm" />
      <DialogContent
        :class="contentClasses"
        @pointer-down-outside="keepOpen"
        @escape-key-down="keepOpen"
        @interact-outside="keepOpen"
      >
        <div class="flex flex-col gap-1.5">
          <DialogTitle class="text-base font-semibold text-fg">{{ title }}</DialogTitle>
          <DialogDescription v-if="description" class="text-sm text-muted">
            {{ description }}
          </DialogDescription>
        </div>
        <div class="mt-4">
          <slot />
        </div>
        <div v-if="$slots.footer" class="mt-5 flex flex-wrap justify-end gap-2">
          <slot name="footer" />
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
