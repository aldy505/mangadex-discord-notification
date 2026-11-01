<script setup lang="ts">
withDefaults(defineProps<{ title: string; subtitle?: string; width?: 'sm' | 'md' }>(), {
  width: 'sm',
})
</script>

<template>
  <div class="grid min-h-dvh place-items-center px-4 py-10">
    <div class="w-full" :class="width === 'md' ? 'max-w-xl' : 'max-w-sm'">
      <div class="mb-6 flex flex-col items-center gap-3 text-center">
        <span
          class="grid size-11 place-items-center rounded-xl bg-accent text-lg font-bold text-accent-fg"
          aria-hidden="true"
        >
          M
        </span>
        <div>
          <h1 class="text-lg font-semibold">{{ title }}</h1>
          <p v-if="subtitle" class="mx-auto mt-1 max-w-prose text-sm text-muted">{{ subtitle }}</p>
        </div>
      </div>
      <div class="rounded-xl border border-border bg-surface p-5 shadow-sm">
        <slot />
      </div>
    </div>
  </div>
</template>
