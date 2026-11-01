<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import AppButton from '../components/ui/AppButton.vue'
import InlineAlert from '../components/ui/InlineAlert.vue'
import StatusBadge from '../components/ui/StatusBadge.vue'
import { errorMessage, listAudit, type AuditEntry } from '../lib/api'
import { formatDateTime, formatJson } from '../lib/format'

const LIMIT = 50

const entries = ref<AuditEntry[]>([])
const total = ref(0)
const offset = ref(0)
const loading = ref(true)
const loadError = ref('')

const showingFrom = computed(() => (entries.value.length === 0 ? 0 : offset.value + 1))
const showingTo = computed(() => offset.value + entries.value.length)
const hasPrev = computed(() => offset.value > 0)
const hasNext = computed(() => offset.value + LIMIT < total.value)

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const page = await listAudit(LIMIT, offset.value)
    entries.value = page.entries
    total.value = page.total
  } catch (err) {
    loadError.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

function goPrev(): void {
  offset.value = Math.max(0, offset.value - LIMIT)
}

function goNext(): void {
  if (hasNext.value) offset.value += LIMIT
}

function actionTone(action: string): 'danger' | 'success' | 'accent' | 'neutral' {
  const normalized = action.toLowerCase()
  if (normalized.includes('delete') || normalized.includes('revoke') || normalized.includes('fail')) {
    return 'danger'
  }
  if (normalized.includes('create') || normalized.includes('invite') || normalized.includes('enroll')) {
    return 'success'
  }
  if (normalized.includes('login') || normalized.includes('password') || normalized.includes('update')) {
    return 'accent'
  }
  return 'neutral'
}

watch(offset, load)

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-5">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-xl font-semibold">Audit log</h1>
        <p class="mt-1 text-sm text-muted">Every admin action, newest first.</p>
      </div>
      <AppButton variant="secondary" :disabled="loading" @click="load">Refresh</AppButton>
    </header>

    <div v-if="loading" class="flex items-center gap-2 py-10 text-sm text-muted">
      <span class="size-4 animate-spin rounded-full border-2 border-border border-t-accent" />
      Loading audit entries…
    </div>

    <InlineAlert v-else-if="loadError" tone="error">
      <p>{{ loadError }}</p>
      <AppButton variant="secondary" size="sm" class="mt-2" @click="load">Retry</AppButton>
    </InlineAlert>

    <div
      v-else-if="entries.length === 0"
      class="rounded-xl border border-dashed border-border bg-surface px-6 py-12 text-center text-sm text-muted"
    >
      No audit entries yet.
    </div>

    <template v-else>
      <!-- Desktop table -->
      <div class="hidden rounded-xl border border-border bg-surface xl:block">
        <table class="w-full table-fixed text-left text-sm">
          <thead class="bg-surface-2 text-[11px] uppercase tracking-wide text-muted">
            <tr>
              <th scope="col" class="w-[15%] px-3 py-3 font-medium">Time</th>
              <th scope="col" class="w-[19%] px-3 py-3 font-medium">Actor</th>
              <th scope="col" class="w-[17%] px-3 py-3 font-medium">Action</th>
              <th scope="col" class="w-[15%] px-3 py-3 font-medium">Target</th>
              <th scope="col" class="w-[24%] px-3 py-3 font-medium">Details</th>
              <th scope="col" class="w-[10%] px-3 py-3 font-medium">IP</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border">
            <tr v-for="entry in entries" :key="entry.id" class="align-top">
              <td class="truncate px-3 py-3 text-xs text-muted" :title="formatDateTime(entry.at)">
                {{ formatDateTime(entry.at) }}
              </td>
              <td class="px-3 py-3">
                <span class="block truncate text-fg" :title="entry.actor">{{ entry.actor }}</span>
              </td>
              <td class="px-3 py-3">
                <StatusBadge :tone="actionTone(entry.action)">
                  <span class="font-mono">{{ entry.action }}</span>
                </StatusBadge>
              </td>
              <td class="px-3 py-3">
                <span class="block truncate text-xs text-muted" :title="entry.target ?? undefined">
                  {{ entry.target ?? '—' }}
                </span>
              </td>
              <td class="px-3 py-3">
                <code
                  v-if="entry.details"
                  class="block truncate rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-muted"
                  :title="formatJson(entry.details)"
                >
                  {{ formatJson(entry.details) }}
                </code>
                <span v-else class="text-xs text-muted">—</span>
              </td>
              <td class="truncate px-3 py-3 font-mono text-[11px] text-muted">
                {{ entry.ip ?? '—' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Mobile cards -->
      <ul class="flex flex-col gap-2 xl:hidden">
        <li
          v-for="entry in entries"
          :key="entry.id"
          class="rounded-xl border border-border bg-surface p-4 text-sm"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <StatusBadge :tone="actionTone(entry.action)">
              <span class="font-mono">{{ entry.action }}</span>
            </StatusBadge>
            <span class="text-xs text-muted">{{ formatDateTime(entry.at) }}</span>
          </div>
          <p class="mt-2 break-words text-fg">{{ entry.actor }}</p>
          <p v-if="entry.target" class="mt-0.5 break-all text-xs text-muted">
            Target: <span class="font-mono">{{ entry.target }}</span>
          </p>
          <pre
            v-if="entry.details"
            class="mt-2 overflow-x-auto rounded bg-surface-2 p-2 font-mono text-[11px] leading-relaxed text-muted"
          >{{ formatJson(entry.details) }}</pre>
          <p v-if="entry.ip" class="mt-2 font-mono text-[11px] text-muted">{{ entry.ip }}</p>
        </li>
      </ul>

      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-xs text-muted">
          Showing {{ showingFrom }}–{{ showingTo }} of {{ total }}
        </p>
        <div class="flex gap-2">
          <AppButton variant="secondary" size="sm" :disabled="!hasPrev || loading" @click="goPrev">
            Previous
          </AppButton>
          <AppButton variant="secondary" size="sm" :disabled="!hasNext || loading" @click="goNext">
            Next
          </AppButton>
        </div>
      </div>
    </template>
  </div>
</template>
