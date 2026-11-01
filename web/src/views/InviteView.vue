<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AuthScreen from '../components/AuthScreen.vue'
import AppButton from '../components/ui/AppButton.vue'
import InlineAlert from '../components/ui/InlineAlert.vue'
import TextField from '../components/ui/TextField.vue'
import { acceptInvite, ApiError, errorMessage, getInvite, type InvitePreview } from '../lib/api'
import { applyAuthResult } from '../lib/auth'
import { formatExpiry } from '../lib/format'
import { notify } from '../lib/toast'

const route = useRoute()
const router = useRouter()

const token = computed(() => {
  const value = route.params.token
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '')
})

const invite = ref<InvitePreview | null>(null)
const loading = ref(true)
const loadError = ref('')

const password = ref('')
const confirmation = ref('')
const busy = ref(false)
const error = ref('')
const passwordError = ref('')
const confirmationError = ref('')

const MIN_PASSWORD_LENGTH = 12

async function loadInvite(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    invite.value = await getInvite(token.value)
    if (!invite.value.valid) {
      loadError.value = 'This invite is no longer valid. Ask an admin for a new one.'
    }
  } catch (err) {
    loadError.value =
      err instanceof ApiError && err.status === 404
        ? 'This invite link is unknown, already used, revoked, or expired.'
        : errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function submit(): Promise<void> {
  if (busy.value) return
  error.value = ''
  passwordError.value = ''
  confirmationError.value = ''

  if (password.value.length < MIN_PASSWORD_LENGTH) {
    passwordError.value = `Use at least ${MIN_PASSWORD_LENGTH} characters.`
    return
  }
  if (password.value !== confirmation.value) {
    confirmationError.value = 'Passwords do not match.'
    return
  }

  busy.value = true
  try {
    const result = await acceptInvite(token.value, password.value)
    applyAuthResult(result)
    password.value = ''
    confirmation.value = ''
    notify('Password set. Now add a second factor.', 'success')
    await router.replace({ name: 'enroll' })
  } catch (err) {
    if (err instanceof ApiError && err.fields.password) {
      passwordError.value = err.fields.password
    } else if (err instanceof ApiError && (err.status === 404 || err.status === 410)) {
      error.value = 'This invite is no longer valid. Ask an admin for a new one.'
    } else {
      error.value = errorMessage(err)
    }
  } finally {
    busy.value = false
  }
}

onMounted(loadInvite)
</script>

<template>
  <AuthScreen
    title="Accept your invite"
    subtitle="Choose a password for your admin account. You will add a second factor next."
  >
    <div v-if="loading" class="flex items-center justify-center gap-2 py-8 text-sm text-muted">
      <span class="size-4 animate-spin rounded-full border-2 border-border border-t-accent" />
      Checking invite…
    </div>

    <div v-else-if="loadError" class="flex flex-col gap-4">
      <InlineAlert tone="error">{{ loadError }}</InlineAlert>
      <AppButton variant="secondary" block @click="loadInvite">Retry</AppButton>
    </div>

    <form v-else-if="invite" class="flex flex-col gap-4" @submit.prevent="submit">
      <dl class="rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm">
        <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <dt class="text-xs uppercase tracking-wide text-muted">Email</dt>
          <dd class="break-all font-medium text-fg">{{ invite.email }}</dd>
        </div>
        <div class="mt-1.5 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <dt class="text-xs uppercase tracking-wide text-muted">Expires</dt>
          <dd class="text-fg">{{ formatExpiry(invite.expires_at) }}</dd>
        </div>
      </dl>

      <InlineAlert v-if="error" tone="error">{{ error }}</InlineAlert>

      <TextField
        v-model="password"
        label="New password"
        type="password"
        autocomplete="new-password"
        hint="At least 12 characters."
        :error="passwordError"
        required
        autofocus
      />
      <TextField
        v-model="confirmation"
        label="Confirm password"
        type="password"
        autocomplete="new-password"
        :error="confirmationError"
        required
      />

      <AppButton type="submit" block :loading="busy">Set password and continue</AppButton>
    </form>
  </AuthScreen>
</template>
