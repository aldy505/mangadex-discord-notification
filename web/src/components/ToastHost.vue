<script setup lang="ts">
import { toasts, dismissToast, type ToastTone } from '../lib/toast'

const TONE_CLASSES: Record<ToastTone, string> = {
  success: 'border-success/50 bg-surface text-success',
  error: 'border-danger/50 bg-surface text-danger',
  info: 'border-border bg-surface text-fg',
}
</script>

<template>
  <div
    class="pointer-events-none fixed inset-x-3 bottom-3 z-[70] flex flex-col items-stretch gap-2 sm:left-auto sm:right-4 sm:w-96"
    aria-live="polite"
    aria-atomic="false"
  >
    <TransitionGroup
      enter-active-class="transition duration-150 ease-out"
      enter-from-class="translate-y-2 opacity-0"
      leave-active-class="transition duration-150 ease-in"
      leave-to-class="opacity-0"
    >
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="pointer-events-auto flex items-start gap-3 rounded-lg border px-3 py-2.5 text-sm shadow-lg"
        :class="TONE_CLASSES[toast.tone]"
        role="status"
      >
        <p class="min-w-0 flex-1 break-words">{{ toast.message }}</p>
        <button
          type="button"
          class="shrink-0 rounded p-0.5 text-current/70 transition-colors hover:text-current"
          aria-label="Dismiss notification"
          @click="dismissToast(toast.id)"
        >
          <svg class="size-4" viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <path
              d="m5 5 10 10M15 5 5 15"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
