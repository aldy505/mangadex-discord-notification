<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, errorMessage, login, verifyTotp } from '../lib/api'
import { applyAuthResult, clearSession, refreshSession, session } from '../lib/auth'
import { notify } from '../lib/toast'
import { signInWithPasskey, webAuthnErrorMessage } from '../lib/webauthn'
import AppButton from '../components/ui/AppButton.vue'
import InlineAlert from '../components/ui/InlineAlert.vue'
import TextField from '../components/ui/TextField.vue'

const router = useRouter()
const route = useRoute()

const step = ref<'credentials' | 'second-factor'>(
  session.stage === 'password' ? 'second-factor' : 'credentials',
)

const email = ref('')
const password = ref('')
const code = ref('')
const busy = ref(false)
const error = ref('')
const passwordError = ref('')

const hasPasskey = computed(() => session.factors.passkeys > 0)
const hasTotp = computed(() => session.factors.totp)
const passkeyCount = computed(() =>
  session.factors.passkeys === 1 ? '1 passkey' : `${session.factors.passkeys} passkeys`,
)

function redirectTarget(): string {
  const redirect = route.query.redirect
  return typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')
    ? redirect
    : '/'
}

async function settleSession(): Promise<void> {
  try {
    await refreshSession()
  } catch {
    // Non-fatal: stage/identity from the auth response are already applied.
  }
}

async function afterAuthenticated(): Promise<void> {
  await settleSession()
  if (session.stage === 'enroll') {
    await router.replace({ name: 'enroll' })
    return
  }
  notify('Signed in.', 'success')
  await router.replace(redirectTarget())
}

async function submitCredentials(): Promise<void> {
  if (busy.value) return
  error.value = ''
  passwordError.value = ''
  busy.value = true
  try {
    const result = await login(email.value.trim(), password.value)
    password.value = ''
    applyAuthResult(result)
    if (result.stage === 'enroll') {
      await afterAuthenticated()
      return
    }
    await settleSession()
    step.value = 'second-factor'
  } catch (err) {
    error.value =
      err instanceof ApiError && err.status === 429
        ? 'Too many attempts. Wait a minute and try again.'
        : errorMessage(err)
    if (err instanceof ApiError && err.fields.password) {
      passwordError.value = err.fields.password
    }
  } finally {
    busy.value = false
  }
}

async function submitPasskey(): Promise<void> {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    await signInWithPasskey()
    await afterAuthenticated()
  } catch (err) {
    error.value = webAuthnErrorMessage(err)
  } finally {
    busy.value = false
  }
}

async function submitTotp(): Promise<void> {
  if (busy.value) return
  error.value = ''
  if (code.value.length !== 6) {
    error.value = 'Enter the 6-digit code from your authenticator app.'
    return
  }
  busy.value = true
  try {
    const result = await verifyTotp(code.value)
    applyAuthResult(result)
    await afterAuthenticated()
  } catch (err) {
    error.value = err instanceof ApiError && err.status === 401 ? 'That code is not valid.' : errorMessage(err)
  } finally {
    busy.value = false
  }
}

function startOver(): void {
  clearSession()
  step.value = 'credentials'
  code.value = ''
  error.value = ''
}

function onCodeInput(value: string): void {
  code.value = value.replace(/\D/g, '').slice(0, 6)
}
</script>

<template>
  <div class="grid min-h-dvh place-items-center px-4 py-10">
    <div class="w-full max-w-sm">
      <div class="mb-6 flex flex-col items-center gap-3 text-center">
        <span
          class="grid size-11 place-items-center rounded-xl bg-accent text-lg font-bold text-accent-fg"
          aria-hidden="true"
        >
          M
        </span>
        <div>
          <h1 class="text-lg font-semibold">MangaDex Discord Notifications</h1>
          <p class="mt-1 text-sm text-muted">
            {{ step === 'credentials' ? 'Sign in to manage subscriptions.' : 'One more step to finish signing in.' }}
          </p>
        </div>
      </div>

      <div class="rounded-xl border border-border bg-surface p-5 shadow-sm">
        <form v-if="step === 'credentials'" class="flex flex-col gap-4" @submit.prevent="submitCredentials">
          <InlineAlert v-if="error" tone="error">{{ error }}</InlineAlert>

          <TextField
            v-model="email"
            label="Email"
            type="email"
            autocomplete="username"
            placeholder="you@example.com"
            required
            autofocus
          />
          <TextField
            v-model="password"
            label="Password"
            type="password"
            autocomplete="current-password"
            :error="passwordError"
            required
          />
          <AppButton type="submit" block :loading="busy">Continue</AppButton>
        </form>

        <div v-else class="flex flex-col gap-4">
          <InlineAlert v-if="error" tone="error">{{ error }}</InlineAlert>

          <div v-if="hasPasskey" class="flex flex-col gap-2">
            <AppButton block :loading="busy" @click="submitPasskey">
              <svg class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
                <circle cx="12" cy="9" r="3.5" />
                <path d="M5 20a7 7 0 0 1 14 0" stroke-linecap="round" />
              </svg>
              Sign in with passkey
            </AppButton>
            <p class="text-center text-xs text-muted">Use {{ passkeyCount }} registered on your account.</p>
          </div>

          <div v-if="hasPasskey && hasTotp" class="flex items-center gap-3 text-xs uppercase tracking-wide text-muted">
            <span class="h-px flex-1 bg-border" />
            or
            <span class="h-px flex-1 bg-border" />
          </div>

          <form v-if="hasTotp" class="flex flex-col gap-3" @submit.prevent="submitTotp">
            <TextField
              :model-value="code"
              label="Authenticator code"
              inputmode="numeric"
              autocomplete="one-time-code"
              placeholder="123456"
              hint="6 digits from your authenticator app."
              mono
              :maxlength="6"
              autofocus
              @update:model-value="onCodeInput"
            />
            <AppButton type="submit" :variant="hasPasskey ? 'secondary' : 'primary'" block :loading="busy">
              Verify code
            </AppButton>
          </form>

          <InlineAlert v-if="!hasPasskey && !hasTotp" tone="warning">
            No second factor is registered on this account. Ask another admin to reset it.
          </InlineAlert>

          <button
            type="button"
            class="self-center text-xs text-muted underline-offset-2 hover:text-fg hover:underline"
            @click="startOver"
          >
            Use a different account
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
