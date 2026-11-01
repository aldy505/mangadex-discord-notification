<script setup lang="ts">
import { computed, useId } from 'vue'
import {
  SelectContent,
  SelectIcon,
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  SelectPortal,
  SelectRoot,
  SelectTrigger,
  SelectValue,
  SelectViewport,
} from 'reka-ui'

export interface SelectOption {
  value: string
  label: string
}

const model = defineModel<string>({ required: true })

const props = withDefaults(
  defineProps<{
    id?: string
    label?: string
    options: SelectOption[]
    placeholder?: string
    disabled?: boolean
    hint?: string
    error?: string
  }>(),
  {
    placeholder: 'Select…',
    disabled: false,
  },
)

const generatedId = useId()
const triggerId = computed(() => props.id ?? `select-${generatedId}`)

const triggerClasses = computed(() => [
  'inline-flex h-10 w-full items-center justify-between gap-2 rounded-lg border bg-surface px-3 text-sm text-fg shadow-sm transition-[border-color,box-shadow]',
  'disabled:cursor-not-allowed disabled:opacity-60',
  props.error ? 'border-danger' : 'border-border',
])

function onChange(value: unknown): void {
  model.value = typeof value === 'string' ? value : ''
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label v-if="label" :for="triggerId" class="text-xs font-medium text-muted">{{ label }}</label>
    <SelectRoot :model-value="model || undefined" :disabled="disabled" @update:model-value="onChange">
      <SelectTrigger :id="triggerId" :class="triggerClasses">
        <SelectValue :placeholder="placeholder" class="truncate text-left" />
        <SelectIcon class="text-muted">
          <svg class="size-4" viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <path
              d="m6 8 4 4 4-4"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </SelectIcon>
      </SelectTrigger>
      <SelectPortal>
        <SelectContent
          position="popper"
          :side-offset="6"
          class="z-50 max-h-72 min-w-[var(--reka-select-trigger-width)] overflow-hidden rounded-lg border border-border bg-surface p-1 shadow-xl"
        >
          <SelectViewport>
            <SelectItem
              v-for="option in options"
              :key="option.value"
              :value="option.value"
              class="relative flex cursor-pointer select-none items-center rounded-md py-2 pl-8 pr-3 text-sm text-fg outline-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[highlighted]:bg-surface-2"
            >
              <SelectItemIndicator class="absolute left-2 inline-flex items-center">
                <svg class="size-4 text-accent" viewBox="0 0 20 20" fill="none" aria-hidden="true">
                  <path
                    d="m5 10.5 3.5 3.5L15 6.5"
                    stroke="currentColor"
                    stroke-width="1.75"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                </svg>
              </SelectItemIndicator>
              <SelectItemText>{{ option.label }}</SelectItemText>
            </SelectItem>
          </SelectViewport>
        </SelectContent>
      </SelectPortal>
    </SelectRoot>
    <p v-if="error" class="text-xs text-danger">{{ error }}</p>
    <p v-else-if="hint" class="text-xs text-muted">{{ hint }}</p>
  </div>
</template>
