<script setup lang="ts">
import { computed } from 'vue'

type Tone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger'

const props = withDefaults(defineProps<{ tone?: Tone }>(), { tone: 'neutral' })

const TONE_CLASSES: Record<Tone, string> = {
  neutral: 'border-border bg-surface-2 text-muted',
  accent: 'border-accent/40 bg-accent-soft text-accent',
  success: 'border-success/40 bg-success-soft text-success',
  warning: 'border-warning/40 bg-warning-soft text-warning',
  danger: 'border-danger/40 bg-danger-soft text-danger',
}

const classes = computed(() => [
  'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium leading-5',
  TONE_CLASSES[props.tone],
])
</script>

<template>
  <span :class="classes"><slot /></span>
</template>
