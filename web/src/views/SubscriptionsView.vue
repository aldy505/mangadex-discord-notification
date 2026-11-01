<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import AddSubscriptionDialog from '../components/AddSubscriptionDialog.vue'
import ConfirmDialog from '../components/ui/ConfirmDialog.vue'
import AppButton from '../components/ui/AppButton.vue'
import InlineAlert from '../components/ui/InlineAlert.vue'
import StatusBadge from '../components/ui/StatusBadge.vue'
import {
  deleteSubscription,
  errorMessage,
  listSubscriptions,
  listWebhooks,
  type Subscription,
  type Webhook,
} from '../lib/api'
import { formatDateTime, formatInterval, formatRelative } from '../lib/format'
import { notify } from '../lib/toast'

const subscriptions = ref<Subscription[]>([])
const webhooks = ref<Webhook[]>([])
const loading = ref(true)
const loadError = ref('')

const dialogOpen = ref(false)
const confirmOpen = ref(false)
const pendingDelete = ref<Subscription | null>(null)
const deleting = ref(false)

function askDelete(subscription: Subscription): void {
  pendingDelete.value = subscription
  confirmOpen.value = true
}

watch(confirmOpen, (isOpen) => {
  if (!isOpen) pendingDelete.value = null
})

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const [subs, hooks] = await Promise.all([listSubscriptions(), listWebhooks()])
    subscriptions.value = subs
    webhooks.value = hooks
  } catch (err) {
    loadError.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function refreshSubscriptions(): Promise<void> {
  try {
    subscriptions.value = await listSubscriptions()
  } catch (err) {
    notify(errorMessage(err), 'error')
  }
}

function statusTone(subscription: Subscription): 'success' | 'danger' | 'neutral' {
  if (!subscription.enabled) return 'neutral'
  if (subscription.last_error) return 'danger'
  return subscription.last_checked_at ? 'success' : 'neutral'
}

function statusLabel(subscription: Subscription): string {
  if (!subscription.enabled) return 'Disabled'
  if (subscription.last_error) return 'Error'
  return subscription.last_checked_at ? 'Healthy' : 'Pending'
}

async function confirmDelete(): Promise<void> {
  const target = pendingDelete.value
  if (!target || deleting.value) return
  deleting.value = true
  try {
    await deleteSubscription(target.id)
    subscriptions.value = subscriptions.value.filter((item) => item.id !== target.id)
    notify('Subscription deleted.', 'success')
    confirmOpen.value = false
    pendingDelete.value = null
  } catch (err) {
    notify(errorMessage(err), 'error')
  } finally {
    deleting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-5">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-xl font-semibold">Subscriptions</h1>
        <p class="mt-1 text-sm text-muted">
          MangaDex titles and RSS feeds watched for new chapters and posts.
        </p>
      </div>
      <div class="flex gap-2">
        <AppButton variant="secondary" :disabled="loading" @click="load">Refresh</AppButton>
        <AppButton @click="dialogOpen = true">Add subscription</AppButton>
      </div>
    </header>

    <div v-if="loading" class="flex items-center gap-2 py-10 text-sm text-muted">
      <span class="size-4 animate-spin rounded-full border-2 border-border border-t-accent" />
      Loading subscriptions…
    </div>

    <InlineAlert v-else-if="loadError" tone="error">
      <p>{{ loadError }}</p>
      <AppButton variant="secondary" size="sm" class="mt-2" @click="load">Retry</AppButton>
    </InlineAlert>

    <div
      v-else-if="subscriptions.length === 0"
      class="rounded-xl border border-dashed border-border bg-surface px-6 py-12 text-center"
    >
      <p class="text-sm font-medium text-fg">No subscriptions yet</p>
      <p class="mt-1 text-sm text-muted">
        Add a MangaDex title or an RSS feed to start posting to Discord.
      </p>
      <AppButton class="mt-4" @click="dialogOpen = true">Add subscription</AppButton>
    </div>

    <template v-else>
      <!-- Desktop table -->
      <div class="hidden rounded-xl border border-border bg-surface xl:block">
        <table class="w-full table-fixed text-left text-sm">
          <thead class="bg-surface-2 text-[11px] uppercase tracking-wide text-muted">
            <tr>
              <th scope="col" class="w-[24%] px-3 py-3 font-medium">Label</th>
              <th scope="col" class="w-[18%] px-3 py-3 font-medium">Source</th>
              <th scope="col" class="w-[9%] px-3 py-3 font-medium">Webhook</th>
              <th scope="col" class="w-[9%] px-3 py-3 font-medium">Languages</th>
              <th scope="col" class="w-[13%] px-3 py-3 font-medium">Interval</th>
              <th scope="col" class="w-[17%] px-3 py-3 font-medium">Status</th>
              <th scope="col" class="w-[10%] px-3 py-3 text-right font-medium">
                <span class="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border">
            <tr v-for="subscription in subscriptions" :key="subscription.id" class="align-top">
              <td class="px-3 py-3">
                <div class="flex items-center gap-2">
                  <StatusBadge :tone="subscription.kind === 'mangadex' ? 'accent' : 'neutral'">
                    {{ subscription.kind === 'mangadex' ? 'MangaDex' : 'RSS' }}
                  </StatusBadge>
                  <span class="min-w-0 truncate font-medium text-fg" :title="subscription.label">
                    {{ subscription.label }}
                  </span>
                </div>
                <p class="mt-1 truncate text-xs text-muted">
                  Created {{ formatDateTime(subscription.created_at) }}
                </p>
              </td>
              <td class="px-3 py-3">
                <span class="block truncate font-mono text-xs text-muted" :title="subscription.source">
                  {{ subscription.source }}
                </span>
              </td>
              <td class="px-3 py-3">
                <span class="block truncate text-muted" :title="subscription.webhook">
                  {{ subscription.webhook }}
                </span>
              </td>
              <td class="px-3 py-3">
                <span
                  class="block truncate text-xs uppercase text-muted"
                  :title="subscription.languages.join(', ')"
                >
                  {{ subscription.languages.length > 0 ? subscription.languages.join(', ') : '—' }}
                </span>
              </td>
              <td class="px-3 py-3">
                <span
                  class="block truncate text-xs text-muted"
                  :title="formatInterval(subscription.interval_seconds)"
                >
                  {{ formatInterval(subscription.interval_seconds) }}
                </span>
              </td>
              <td class="px-3 py-3">
                <StatusBadge :tone="statusTone(subscription)">{{ statusLabel(subscription) }}</StatusBadge>
                <p
                  v-if="subscription.last_error"
                  class="mt-1 truncate text-xs text-danger"
                  :title="subscription.last_error"
                >
                  {{ subscription.last_error }}
                </p>
                <p v-else-if="subscription.last_checked_at" class="mt-1 truncate text-xs text-muted">
                  Checked {{ formatRelative(subscription.last_checked_at) }}
                </p>
              </td>
              <td class="px-3 py-3 text-right">
                <AppButton variant="ghost" size="sm" @click="askDelete(subscription)">
                  Delete
                </AppButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Mobile / narrow cards -->
      <ul class="flex flex-col gap-3 xl:hidden">
        <li
          v-for="subscription in subscriptions"
          :key="subscription.id"
          class="rounded-xl border border-border bg-surface p-4"
        >
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <StatusBadge :tone="subscription.kind === 'mangadex' ? 'accent' : 'neutral'">
                  {{ subscription.kind === 'mangadex' ? 'MangaDex' : 'RSS' }}
                </StatusBadge>
                <span class="min-w-0 truncate text-sm font-medium text-fg">{{ subscription.label }}</span>
              </div>
              <p class="mt-1 break-all font-mono text-[11px] text-muted">{{ subscription.source }}</p>
            </div>
            <AppButton variant="ghost" size="sm" @click="askDelete(subscription)">Delete</AppButton>
          </div>

          <dl class="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
            <div>
              <dt class="text-muted">Webhook</dt>
              <dd class="mt-0.5 text-fg">{{ subscription.webhook }}</dd>
            </div>
            <div>
              <dt class="text-muted">Interval</dt>
              <dd class="mt-0.5 text-fg">{{ formatInterval(subscription.interval_seconds) }}</dd>
            </div>
            <div>
              <dt class="text-muted">Languages</dt>
              <dd class="mt-0.5 uppercase text-fg">
                {{ subscription.languages.length > 0 ? subscription.languages.join(', ') : '—' }}
              </dd>
            </div>
            <div>
              <dt class="text-muted">Created</dt>
              <dd class="mt-0.5 text-fg">{{ formatDateTime(subscription.created_at) }}</dd>
            </div>
          </dl>

          <div class="mt-3 flex flex-wrap items-center gap-2 border-t border-border pt-3">
            <StatusBadge :tone="statusTone(subscription)">{{ statusLabel(subscription) }}</StatusBadge>
            <span v-if="subscription.last_error" class="min-w-0 flex-1 truncate text-xs text-danger" :title="subscription.last_error">
              {{ subscription.last_error }}
            </span>
            <span v-else-if="subscription.last_checked_at" class="text-xs text-muted">
              Checked {{ formatRelative(subscription.last_checked_at) }}
            </span>
          </div>
        </li>
      </ul>
    </template>

    <AddSubscriptionDialog
      v-model:open="dialogOpen"
      :webhooks="webhooks"
      @created="refreshSubscriptions"
    />

    <ConfirmDialog
      v-model:open="confirmOpen"
      title="Delete subscription?"
      :message="
        pendingDelete
          ? `“${pendingDelete.label}” will stop posting to Discord. This cannot be undone.`
          : undefined
      "
      confirm-label="Delete subscription"
      :busy="deleting"
      @confirm="confirmDelete"
    />
  </div>
</template>
