<script setup lang="ts">
import { computed } from 'vue'

type Tone = 'error' | 'success' | 'info' | 'warning'

const props = withDefaults(defineProps<{ tone?: Tone; title?: string }>(), { tone: 'error' })

const TONE_CLASSES: Record<Tone, string> = {
  error: 'border-danger/40 bg-danger-soft text-danger',
  success: 'border-success/40 bg-success-soft text-success',
  info: 'border-border bg-surface-2 text-fg',
  warning: 'border-warning/40 bg-warning-soft text-warning',
}

const classes = computed(() => [
  'rounded-lg border px-3 py-2 text-sm leading-relaxed',
  TONE_CLASSES[props.tone],
])
</script>

<template>
  <div :class="classes" :role="tone === 'error' ? 'alert' : 'status'">
    <p v-if="title" class="font-medium">{{ title }}</p>
    <slot />
  </div>
</template>
