<script setup lang="ts">
import { computed, useId } from 'vue'

const model = defineModel<string>({ required: true })

const props = withDefaults(
  defineProps<{
    id?: string
    label?: string
    type?: 'text' | 'email' | 'password' | 'url' | 'search'
    name?: string
    autocomplete?: string
    placeholder?: string
    hint?: string
    error?: string
    required?: boolean
    disabled?: boolean
    readonly?: boolean
    mono?: boolean
    inputmode?: 'text' | 'numeric' | 'decimal' | 'email' | 'url' | 'search'
    maxlength?: number
    autofocus?: boolean
  }>(),
  {
    type: 'text',
    required: false,
    disabled: false,
    readonly: false,
    mono: false,
    autofocus: false,
  },
)

const generatedId = useId()
const inputId = computed(() => props.id ?? `field-${generatedId}`)
const describedBy = computed(() => (props.error || props.hint ? `${inputId.value}-help` : undefined))

const classes = computed(() => [
  'h-10 w-full rounded-lg border bg-surface px-3 text-sm text-fg shadow-sm transition-[border-color,box-shadow]',
  'placeholder:text-muted/70 disabled:cursor-not-allowed disabled:opacity-60',
  props.mono ? 'font-mono tracking-tight' : '',
  props.error ? 'border-danger' : 'border-border',
])
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label v-if="label" :for="inputId" class="text-xs font-medium text-muted">
      {{ label }}
      <span v-if="required" class="text-danger" aria-hidden="true">*</span>
    </label>
    <input
      :id="inputId"
      v-model="model"
      :type="type"
      :name="name"
      :autocomplete="autocomplete"
      :placeholder="placeholder"
      :required="required"
      :disabled="disabled"
      :readonly="readonly"
      :inputmode="inputmode"
      :maxlength="maxlength"
      :autofocus="autofocus"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="describedBy"
      :class="classes"
    />
    <p v-if="error" :id="`${inputId}-help`" class="text-xs text-danger">{{ error }}</p>
    <p v-else-if="hint" :id="`${inputId}-help`" class="text-xs text-muted">{{ hint }}</p>
  </div>
</template>
