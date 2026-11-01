<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PasskeySetupCard from '../components/PasskeySetupCard.vue'
import TotpSetupCard from '../components/TotpSetupCard.vue'
import ConfirmDialog from '../components/ui/ConfirmDialog.vue'
import AppButton from '../components/ui/AppButton.vue'
import InlineAlert from '../components/ui/InlineAlert.vue'
import ModalDialog from '../components/ui/ModalDialog.vue'
import StatusBadge from '../components/ui/StatusBadge.vue'
import TextField from '../components/ui/TextField.vue'
import {
  ApiError,
  changePassword,
  deletePasskey,
  deleteTotp,
  errorMessage,
  getAccount,
  type AccountInfo,
  type PasskeyInfo,
} from '../lib/api'
import { refreshSession, session } from '../lib/auth'
import { formatDateTime, formatRelative } from '../lib/format'
import { notify } from '../lib/toast'

const account = ref<AccountInfo | null>(null)
const loading = ref(true)
const loadError = ref('')

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordBusy = ref(false)
const passwordError = ref('')
const fieldErrors = ref<Record<string, string>>({})

const addPasskeyOpen = ref(false)
const addTotpOpen = ref(false)

const totpRemoveOpen = ref(false)
const passkeyTarget = ref<PasskeyInfo | null>(null)
const passkeyRemoveOpen = ref(false)
const removing = ref(false)

const factorTotal = computed(() => {
  if (!account.value) return 0
  return (account.value.totp ? 1 : 0) + account.value.passkeys.length
})
const canRemoveFactor = computed(() => factorTotal.value > 1)

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    account.value = await getAccount()
  } catch (err) {
    loadError.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function reloadAccount(): Promise<void> {
  try {
    account.value = await getAccount()
  } catch (err) {
    notify(errorMessage(err), 'error')
  }
}

async function submitPassword(): Promise<void> {
  if (passwordBusy.value) return
  passwordError.value = ''
  fieldErrors.value = {}

  if (newPassword.value.length < 12) {
    fieldErrors.value = { new_password: 'Use at least 12 characters.' }
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    fieldErrors.value = { confirm_password: 'Passwords do not match.' }
    return
  }

  passwordBusy.value = true
  try {
    await changePassword(currentPassword.value, newPassword.value)
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    notify('Password updated.', 'success')
  } catch (err) {
    if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
      fieldErrors.value = err.fields
    } else {
      passwordError.value = errorMessage(err)
    }
  } finally {
    passwordBusy.value = false
  }
}

async function onPasskeyAdded(): Promise<void> {
  addPasskeyOpen.value = false
  await reloadAccount()
  await refreshQuietly()
}

async function onTotpAdded(): Promise<void> {
  addTotpOpen.value = false
  await reloadAccount()
  await refreshQuietly()
}

async function refreshQuietly(): Promise<void> {
  try {
    await refreshSession()
  } catch {
    // Session metadata refresh is cosmetic here.
  }
}

function askRemovePasskey(passkey: PasskeyInfo): void {
  passkeyTarget.value = passkey
  passkeyRemoveOpen.value = true
}

async function confirmRemovePasskey(): Promise<void> {
  const target = passkeyTarget.value
  if (!target || removing.value) return
  removing.value = true
  try {
    await deletePasskey(target.id)
    notify('Passkey removed.', 'success')
    passkeyRemoveOpen.value = false
    passkeyTarget.value = null
    await reloadAccount()
    await refreshQuietly()
  } catch (err) {
    notify(
      err instanceof ApiError && err.status === 409
        ? err.message
        : errorMessage(err),
      'error',
    )
  } finally {
    removing.value = false
  }
}

async function confirmRemoveTotp(): Promise<void> {
  if (removing.value) return
  removing.value = true
  try {
    await deleteTotp()
    notify('Authenticator app removed.', 'success')
    totpRemoveOpen.value = false
    await reloadAccount()
    await refreshQuietly()
  } catch (err) {
    notify(
      err instanceof ApiError && err.status === 409
        ? err.message
        : errorMessage(err),
      'error',
    )
  } finally {
    removing.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-6">
    <header>
      <h1 class="text-xl font-semibold">Account</h1>
      <p class="mt-1 text-sm text-muted">Your identity, password, and second factors.</p>
    </header>

    <div v-if="loading" class="flex items-center gap-2 py-10 text-sm text-muted">
      <span class="size-4 animate-spin rounded-full border-2 border-border border-t-accent" />
      Loading account…
    </div>

    <InlineAlert v-else-if="loadError" tone="error">
      <p>{{ loadError }}</p>
      <AppButton variant="secondary" size="sm" class="mt-2" @click="load">Retry</AppButton>
    </InlineAlert>

    <template v-else-if="account">
      <!-- Identity -->
      <section class="rounded-xl border border-border bg-surface p-5">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <p class="text-xs uppercase tracking-wide text-muted">Signed in as</p>
            <p class="mt-1 break-all text-base font-semibold text-fg">{{ account.admin.email }}</p>
            <p class="mt-1 break-all font-mono text-[11px] text-muted">ID {{ account.admin.id }}</p>
            <p class="mt-1 text-xs text-muted">Admin since {{ formatDateTime(account.admin.created_at) }}</p>
          </div>
          <StatusBadge :tone="factorTotal > 1 ? 'success' : 'accent'">
            {{ factorTotal }} {{ factorTotal === 1 ? 'second factor' : 'second factors' }}
          </StatusBadge>
        </div>
      </section>

      <!-- Password -->
      <section class="rounded-xl border border-border bg-surface p-5">
        <h2 class="text-sm font-semibold">Change password</h2>
        <p class="mt-1 text-xs text-muted">Minimum 12 characters. Other sessions are signed out when the password changes.</p>

        <form class="mt-4 grid gap-3 sm:grid-cols-3" @submit.prevent="submitPassword">
          <TextField
            v-model="currentPassword"
            label="Current password"
            type="password"
            autocomplete="current-password"
            :error="fieldErrors.current_password"
            required
          />
          <TextField
            v-model="newPassword"
            label="New password"
            type="password"
            autocomplete="new-password"
            :error="fieldErrors.new_password"
            required
          />
          <TextField
            v-model="confirmPassword"
            label="Confirm new password"
            type="password"
            autocomplete="new-password"
            :error="fieldErrors.confirm_password"
            required
          />

          <InlineAlert v-if="passwordError" tone="error" class="sm:col-span-3">
            {{ passwordError }}
          </InlineAlert>

          <div class="sm:col-span-3">
            <AppButton type="submit" :loading="passwordBusy">Update password</AppButton>
          </div>
        </form>
      </section>

      <!-- Factors -->
      <section class="flex flex-col gap-3">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-sm font-semibold">Second factors</h2>
            <p class="text-xs text-muted">
              At least one factor must stay registered. Sign-in asks for one of them.
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <AppButton
              v-if="session.passkeysAvailable"
              variant="secondary"
              size="sm"
              @click="addPasskeyOpen = true"
            >
              Add passkey
            </AppButton>
            <AppButton
              v-if="!account.totp"
              variant="secondary"
              size="sm"
              @click="addTotpOpen = true"
            >
              Add authenticator app
            </AppButton>
          </div>
        </div>

        <InlineAlert v-if="!canRemoveFactor" tone="info">
          This is your only second factor. Add another one before removing it.
        </InlineAlert>

        <div class="overflow-hidden rounded-xl border border-border bg-surface">
          <div class="divide-y divide-border">
            <div class="flex flex-wrap items-start justify-between gap-3 p-4">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <p class="text-sm font-medium text-fg">Authenticator app</p>
                  <StatusBadge :tone="account.totp ? 'success' : 'neutral'">
                    {{ account.totp ? 'Enabled' : 'Not set up' }}
                  </StatusBadge>
                </div>
                <p class="mt-1 text-xs text-muted">
                  6-digit TOTP codes from an app such as 1Password, Authy, or Google Authenticator.
                </p>
              </div>
              <div class="flex gap-2">
                <AppButton
                  v-if="!account.totp"
                  variant="secondary"
                  size="sm"
                  @click="addTotpOpen = true"
                >
                  Set up
                </AppButton>
                <AppButton
                  v-else
                  variant="ghost"
                  size="sm"
                  :disabled="!canRemoveFactor"
                  :title="canRemoveFactor ? undefined : 'Add another second factor first'"
                  @click="totpRemoveOpen = true"
                >
                  Remove
                </AppButton>
              </div>
            </div>

            <div
              v-for="passkey in account.passkeys"
              :key="passkey.id"
              class="flex flex-wrap items-start justify-between gap-3 p-4"
            >
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <p class="min-w-0 truncate text-sm font-medium text-fg">{{ passkey.name }}</p>
                  <StatusBadge tone="accent">Passkey</StatusBadge>
                </div>
                <p class="mt-1 text-xs text-muted">
                  Added {{ formatDateTime(passkey.created_at) }} ·
                  {{
                    passkey.last_used_at
                      ? `last used ${formatRelative(passkey.last_used_at)}`
                      : 'never used'
                  }}
                </p>
                <p class="mt-0.5 break-all font-mono text-[11px] text-muted">{{ passkey.id }}</p>
              </div>
              <AppButton
                variant="ghost"
                size="sm"
                :disabled="!canRemoveFactor"
                :title="canRemoveFactor ? undefined : 'Add another second factor first'"
                @click="askRemovePasskey(passkey)"
              >
                Remove
              </AppButton>
            </div>

            <div v-if="account.passkeys.length === 0" class="p-4 text-xs text-muted">
              No passkeys registered.
            </div>
          </div>
        </div>
      </section>
    </template>

    <ModalDialog
      v-model:open="addPasskeyOpen"
      title="Add a passkey"
      description="Register this device or a security key."
      size="md"
    >
      <PasskeySetupCard
        v-if="addPasskeyOpen"
        button-label="Register passkey"
        @done="onPasskeyAdded"
      />
    </ModalDialog>

    <ModalDialog
      v-model:open="addTotpOpen"
      title="Add an authenticator app"
      description="Scan the QR code, then confirm a code to finish."
      size="md"
    >
      <TotpSetupCard v-if="addTotpOpen" @done="onTotpAdded" />
    </ModalDialog>

    <ConfirmDialog
      v-model:open="totpRemoveOpen"
      title="Remove authenticator app?"
      message="You will no longer be able to sign in with TOTP codes."
      confirm-label="Remove app"
      :busy="removing"
      @confirm="confirmRemoveTotp"
    />

    <ConfirmDialog
      v-model:open="passkeyRemoveOpen"
      title="Remove passkey?"
      :message="
        passkeyTarget
          ? `“${passkeyTarget.name}” will no longer work for signing in.`
          : undefined
      "
      confirm-label="Remove passkey"
      :busy="removing"
      @confirm="confirmRemovePasskey"
    />
  </div>
</template>
