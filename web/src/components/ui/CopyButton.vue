<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import AppButton from './AppButton.vue'

const props = withDefaults(
  defineProps<{ value: string; label?: string; copiedLabel?: string; disabled?: boolean }>(),
  { label: 'Copy', copiedLabel: 'Copied' },
)

const state = ref<'idle' | 'copied' | 'failed'>('idle')
let resetTimer: number | undefined

async function writeToClipboard(text: string): Promise<boolean> {
  if (window.isSecureContext && navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Fall through to the legacy path (e.g. clipboard permission denied).
    }
  }
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.top = '-1000px'
  area.style.opacity = '0'
  document.body.appendChild(area)
  let copied = false
  try {
    area.select()
    copied = document.execCommand('copy')
  } catch {
    copied = false
  } finally {
    area.remove()
  }
  return copied
}

async function handleCopy(): Promise<void> {
  const copied = await writeToClipboard(props.value)
  state.value = copied ? 'copied' : 'failed'
  window.clearTimeout(resetTimer)
  resetTimer = window.setTimeout(() => {
    state.value = 'idle'
  }, 2000)
}

onBeforeUnmount(() => window.clearTimeout(resetTimer))
</script>

<template>
  <AppButton variant="secondary" size="sm" :disabled="disabled" @click="handleCopy">
    <span aria-live="polite">
      {{ state === 'copied' ? copiedLabel : state === 'failed' ? 'Copy failed' : label }}
    </span>
  </AppButton>
</template>
