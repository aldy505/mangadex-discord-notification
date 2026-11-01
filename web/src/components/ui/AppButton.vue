<script setup lang="ts">
import { computed } from 'vue'

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'sm' | 'md'

const props = withDefaults(
  defineProps<{
    variant?: Variant
    size?: Size
    type?: 'button' | 'submit' | 'reset'
    disabled?: boolean
    loading?: boolean
    block?: boolean
  }>(),
  {
    variant: 'primary',
    size: 'md',
    type: 'button',
    disabled: false,
    loading: false,
    block: false,
  },
)

const VARIANT_CLASSES: Record<Variant, string> = {
  primary: 'bg-accent text-accent-fg hover:bg-accent-hover',
  secondary: 'border border-border bg-surface-2 text-fg hover:bg-surface',
  ghost: 'text-muted hover:bg-surface-2 hover:text-fg',
  danger: 'border border-danger/40 bg-danger-soft text-danger hover:border-danger/70',
}

const SIZE_CLASSES: Record<Size, string> = {
  sm: 'h-8 gap-1.5 px-3 text-xs',
  md: 'h-10 gap-2 px-4 text-sm',
}

const classes = computed(() => [
  'inline-flex shrink-0 items-center justify-center rounded-lg font-medium transition-colors',
  'disabled:cursor-not-allowed disabled:opacity-50',
  SIZE_CLASSES[props.size],
  VARIANT_CLASSES[props.variant],
  props.block ? 'w-full' : '',
])
</script>

<template>
  <button :type="type" :disabled="disabled || loading" :class="classes">
    <svg
      v-if="loading"
      class="size-4 animate-spin"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-opacity="0.25" stroke-width="3" />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        stroke-width="3"
        stroke-linecap="round"
      />
    </svg>
    <slot />
  </button>
</template>
