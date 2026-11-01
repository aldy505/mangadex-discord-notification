<script setup lang="ts">
import { TabsContent, TabsList, TabsRoot, TabsTrigger } from 'reka-ui'
import { computed, ref, watch } from 'vue'
import {
  createSubscription,
  errorMessage,
  previewFeed,
  previewMangadex,
  type CreateFeedSubscription,
  type FeedPreview,
  type MangaPreview,
  type Subscription,
  type Webhook,
} from '../lib/api'
import { formatRelative } from '../lib/format'
import { notify } from '../lib/toast'
import AppButton from './ui/AppButton.vue'
import InlineAlert from './ui/InlineAlert.vue'
import ModalDialog from './ui/ModalDialog.vue'
import SelectField from './ui/SelectField.vue'
import TextField from './ui/TextField.vue'

const props = defineProps<{ webhooks: Webhook[] }>()
const emit = defineEmits<{ created: [Subscription] }>()

const open = defineModel<boolean>('open', { required: true })

const INTERVAL_OPTIONS = [
  { value: '300', label: 'Every 5 minutes' },
  { value: '900', label: 'Every 15 minutes' },
  { value: '3600', label: 'Hourly' },
  { value: '21600', label: 'Every 6 hours' },
  { value: '86400', label: 'Daily' },
  { value: 'custom', label: 'Custom interval…' },
]

const activeTab = ref('mangadex')

const webhookOptions = computed(() =>
  props.webhooks.map((webhook) => ({
    value: webhook.name,
    label: `${webhook.name} · ${webhook.host}`,
  })),
)

const defaultWebhook = computed(
  () => props.webhooks.find((webhook) => webhook.default)?.name ?? props.webhooks[0]?.name ?? '',
)

const hasWebhooks = computed(() => props.webhooks.length > 0)

/* ---------------------------------- manga --------------------------------- */

const mangaInput = ref('')
const mangaPreview = ref<MangaPreview | null>(null)
const mangaChecking = ref(false)
const mangaCheckError = ref('')
const coverFailed = ref(false)
const selectedLanguages = ref<string[]>(['en'])
const mangaWebhook = ref('')
const mangaInterval = ref('3600')
const mangaCustomMinutes = ref('')
const mangaFormError = ref('')
const mangaBusy = ref(false)

async function checkManga(): Promise<void> {
  const value = mangaInput.value.trim()
  mangaCheckError.value = ''
  mangaPreview.value = null
  coverFailed.value = false
  if (value.length === 0) {
    mangaCheckError.value = 'Paste a mangadex.org title URL or a manga ID.'
    return
  }
  mangaChecking.value = true
  try {
    const preview = await previewMangadex(value)
    mangaPreview.value = preview
    selectedLanguages.value = preview.available_languages.includes('en')
      ? ['en']
      : preview.available_languages.slice(0, 1)
  } catch (err) {
    mangaCheckError.value = errorMessage(err)
  } finally {
    mangaChecking.value = false
  }
}

function toggleLanguage(language: string): void {
  if (selectedLanguages.value.includes(language)) {
    selectedLanguages.value = selectedLanguages.value.filter((item) => item !== language)
  } else {
    selectedLanguages.value = [...selectedLanguages.value, language]
  }
}

function resolveInterval(choice: string, customMinutes: string): number | null {
  if (choice !== 'custom') return Number(choice)
  const minutes = Number.parseInt(customMinutes, 10)
  if (!Number.isFinite(minutes) || minutes < 1) return null
  return minutes * 60
}

async function createManga(): Promise<void> {
  mangaFormError.value = ''
  if (!mangaPreview.value) {
    mangaFormError.value = 'Check the manga first.'
    return
  }
  if (mangaWebhook.value.length === 0) {
    mangaFormError.value = 'Choose a webhook.'
    return
  }
  if (selectedLanguages.value.length === 0) {
    mangaFormError.value = 'Choose at least one language.'
    return
  }
  const seconds = resolveInterval(mangaInterval.value, mangaCustomMinutes.value)
  if (seconds === null) {
    mangaFormError.value = 'Enter a custom interval of at least 1 minute.'
    return
  }
  mangaBusy.value = true
  try {
    const subscription = await createSubscription({
      kind: 'mangadex',
      manga_id: mangaPreview.value.manga_id,
      webhook: mangaWebhook.value,
      languages: [...selectedLanguages.value],
      interval_seconds: seconds,
    })
    notify(`Subscribed to "${mangaPreview.value.title}".`, 'success')
    emit('created', subscription)
    open.value = false
  } catch (err) {
    mangaFormError.value = errorMessage(err)
  } finally {
    mangaBusy.value = false
  }
}

/* ----------------------------------- rss ---------------------------------- */

const feedUrl = ref('')
const feedPreview = ref<FeedPreview | null>(null)
const feedChecking = ref(false)
const feedCheckError = ref('')
const feedLabel = ref('')
const feedWebhook = ref('')
const feedInterval = ref('3600')
const feedCustomMinutes = ref('')
const feedFormError = ref('')
const feedBusy = ref(false)

async function checkFeed(): Promise<void> {
  const value = feedUrl.value.trim()
  feedCheckError.value = ''
  feedPreview.value = null
  if (value.length === 0) {
    feedCheckError.value = 'Enter the feed URL.'
    return
  }
  feedChecking.value = true
  try {
    feedPreview.value = await previewFeed(value)
  } catch (err) {
    feedCheckError.value = errorMessage(err)
  } finally {
    feedChecking.value = false
  }
}

async function createFeed(): Promise<void> {
  feedFormError.value = ''
  if (!feedPreview.value) {
    feedFormError.value = 'Check the feed first.'
    return
  }
  if (feedWebhook.value.length === 0) {
    feedFormError.value = 'Choose a webhook.'
    return
  }
  const seconds = resolveInterval(feedInterval.value, feedCustomMinutes.value)
  if (seconds === null) {
    feedFormError.value = 'Enter a custom interval of at least 1 minute.'
    return
  }
  const payload: CreateFeedSubscription = {
    kind: 'rss',
    url: feedUrl.value.trim(),
    webhook: feedWebhook.value,
    interval_seconds: seconds,
  }
  const label = feedLabel.value.trim()
  if (label.length > 0) payload.label = label

  feedBusy.value = true
  try {
    const subscription = await createSubscription(payload)
    notify('Feed subscription created.', 'success')
    emit('created', subscription)
    open.value = false
  } catch (err) {
    feedFormError.value = errorMessage(err)
  } finally {
    feedBusy.value = false
  }
}

/* ---------------------------------- reset --------------------------------- */

watch(open, (isOpen) => {
  if (!isOpen) return
  mangaInput.value = ''
  mangaPreview.value = null
  mangaCheckError.value = ''
  mangaChecking.value = false
  coverFailed.value = false
  selectedLanguages.value = ['en']
  mangaWebhook.value = defaultWebhook.value
  mangaInterval.value = '3600'
  mangaCustomMinutes.value = ''
  mangaFormError.value = ''
  activeTab.value = 'mangadex'

  feedUrl.value = ''
  feedPreview.value = null
  feedCheckError.value = ''
  feedChecking.value = false
  feedLabel.value = ''
  feedWebhook.value = defaultWebhook.value
  feedInterval.value = '3600'
  feedCustomMinutes.value = ''
  feedFormError.value = ''
})

watch(mangaInput, () => {
  mangaPreview.value = null
  mangaCheckError.value = ''
})

watch(feedUrl, () => {
  feedPreview.value = null
  feedCheckError.value = ''
})
</script>

<template>
  <ModalDialog
    v-model:open="open"
    title="Add subscription"
    description="Follow a MangaDex title or any RSS feed and post updates to a Discord webhook."
    size="lg"
  >
    <InlineAlert v-if="!hasWebhooks" tone="warning">
      No webhooks are configured on the server. Add one to the bot configuration before creating
      subscriptions.
    </InlineAlert>

    <TabsRoot v-else v-model="activeTab" class="flex flex-col gap-4">
      <TabsList class="grid grid-cols-2 gap-1 rounded-lg bg-surface-2 p-1" aria-label="Subscription type">
        <TabsTrigger
          value="mangadex"
          class="rounded-md px-3 py-1.5 text-sm text-muted transition-colors hover:text-fg data-[state=active]:bg-surface data-[state=active]:font-medium data-[state=active]:text-fg data-[state=active]:shadow-sm"
        >
          MangaDex
        </TabsTrigger>
        <TabsTrigger
          value="rss"
          class="rounded-md px-3 py-1.5 text-sm text-muted transition-colors hover:text-fg data-[state=active]:bg-surface data-[state=active]:font-medium data-[state=active]:text-fg data-[state=active]:shadow-sm"
        >
          RSS feed
        </TabsTrigger>
      </TabsList>

      <TabsContent value="mangadex" class="flex flex-col gap-4 focus:outline-none">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div class="min-w-0 flex-1">
            <TextField
              v-model="mangaInput"
              label="MangaDex URL or manga ID"
              placeholder="https://mangadex.org/title/… or UUID"
              autocomplete="off"
              @keydown.enter.prevent="checkManga"
            />
          </div>
          <AppButton variant="secondary" :loading="mangaChecking" @click="checkManga">Check</AppButton>
        </div>

        <InlineAlert v-if="mangaCheckError" tone="error">{{ mangaCheckError }}</InlineAlert>

        <div
          v-if="mangaPreview"
          class="flex gap-3 rounded-lg border border-border bg-surface-2/60 p-3"
        >
          <div class="h-24 w-16 shrink-0 overflow-hidden rounded-md border border-border bg-surface">
            <img
              v-if="!coverFailed && mangaPreview.cover_url"
              :src="mangaPreview.cover_url"
              :alt="`Cover of ${mangaPreview.title}`"
              referrerpolicy="no-referrer"
              loading="lazy"
              class="h-full w-full object-cover"
              @error="coverFailed = true"
            />
            <span
              v-else
              class="grid h-full w-full place-items-center text-lg font-semibold text-muted"
              aria-hidden="true"
            >
              {{ mangaPreview.title.slice(0, 1) }}
            </span>
          </div>
          <div class="min-w-0 flex-1 text-sm">
            <p class="font-medium text-fg">{{ mangaPreview.title }}</p>
            <p class="mt-1 text-xs text-muted">
              Latest chapter {{ mangaPreview.last_chapter || '—' }} · {{ mangaPreview.status }}
            </p>
            <p class="mt-1 break-all font-mono text-[11px] text-muted">{{ mangaPreview.manga_id }}</p>
          </div>
        </div>

        <fieldset v-if="mangaPreview" class="flex flex-col gap-2">
          <legend class="text-xs font-medium text-muted">Languages</legend>
          <div class="flex flex-wrap gap-1.5">
            <button
              v-for="language in mangaPreview.available_languages"
              :key="language"
              type="button"
              class="rounded-full border px-3 py-1 text-xs font-medium uppercase transition-colors"
              :class="
                selectedLanguages.includes(language)
                  ? 'border-accent bg-accent-soft text-accent'
                  : 'border-border bg-surface text-muted hover:text-fg'
              "
              :aria-pressed="selectedLanguages.includes(language)"
              @click="toggleLanguage(language)"
            >
              {{ language }}
            </button>
          </div>
          <p v-if="mangaPreview.available_languages.length === 0" class="text-xs text-muted">
            No languages reported for this title.
          </p>
        </fieldset>

        <div class="grid gap-3 sm:grid-cols-2">
          <SelectField
            v-model="mangaWebhook"
            label="Discord webhook"
            placeholder="Choose a webhook"
            :options="webhookOptions"
          />
          <SelectField v-model="mangaInterval" label="Check interval" :options="INTERVAL_OPTIONS" />
        </div>
        <TextField
          v-if="mangaInterval === 'custom'"
          v-model="mangaCustomMinutes"
          label="Custom interval (minutes)"
          inputmode="numeric"
          placeholder="45"
        />

        <InlineAlert v-if="mangaFormError" tone="error">{{ mangaFormError }}</InlineAlert>
      </TabsContent>

      <TabsContent value="rss" class="flex flex-col gap-4 focus:outline-none">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div class="min-w-0 flex-1">
            <TextField
              v-model="feedUrl"
              label="Feed URL"
              type="url"
              placeholder="https://example.com/feed.xml"
              autocomplete="off"
              @keydown.enter.prevent="checkFeed"
            />
          </div>
          <AppButton variant="secondary" :loading="feedChecking" @click="checkFeed">Check</AppButton>
        </div>

        <InlineAlert v-if="feedCheckError" tone="error">{{ feedCheckError }}</InlineAlert>

        <div v-if="feedPreview" class="rounded-lg border border-border bg-surface-2/60 p-3 text-sm">
          <p class="font-medium text-fg">{{ feedPreview.title }}</p>
          <p v-if="feedPreview.description" class="mt-1 line-clamp-2 text-xs text-muted">
            {{ feedPreview.description }}
          </p>
          <p class="mt-2 text-xs text-muted">
            {{ feedPreview.item_count }} {{ feedPreview.item_count === 1 ? 'item' : 'items' }}
            <template v-if="feedPreview.latest_item_at">
              · latest {{ formatRelative(feedPreview.latest_item_at) }}
            </template>
          </p>
          <a
            v-if="feedPreview.link"
            :href="feedPreview.link"
            target="_blank"
            rel="noopener noreferrer"
            class="mt-1 block truncate text-xs text-accent underline-offset-2 hover:underline"
          >
            {{ feedPreview.link }}
          </a>
        </div>

        <div class="grid gap-3 sm:grid-cols-2">
          <SelectField
            v-model="feedWebhook"
            label="Discord webhook"
            placeholder="Choose a webhook"
            :options="webhookOptions"
          />
          <SelectField v-model="feedInterval" label="Check interval" :options="INTERVAL_OPTIONS" />
        </div>
        <TextField
          v-if="feedInterval === 'custom'"
          v-model="feedCustomMinutes"
          label="Custom interval (minutes)"
          inputmode="numeric"
          placeholder="45"
        />
        <TextField
          v-model="feedLabel"
          label="Label (optional)"
          placeholder="Defaults to the feed title"
          :maxlength="80"
        />

        <InlineAlert v-if="feedFormError" tone="error">{{ feedFormError }}</InlineAlert>
      </TabsContent>
    </TabsRoot>

    <template #footer>
      <AppButton variant="secondary" @click="open = false">Cancel</AppButton>
      <AppButton
        v-if="hasWebhooks"
        :loading="activeTab === 'mangadex' ? mangaBusy : feedBusy"
        @click="activeTab === 'mangadex' ? createManga() : createFeed()"
      >
        {{ activeTab === 'mangadex' ? 'Add manga subscription' : 'Add feed subscription' }}
      </AppButton>
    </template>
  </ModalDialog>
</template>
