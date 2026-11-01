<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { applyAuthResult, refreshSession } from '../lib/auth'
import { errorMessage, totpSetup, totpSetupConfirm, type TotpSetup } from '../lib/api'
import { notify } from '../lib/toast'
import AppButton from './ui/AppButton.vue'
import CopyButton from './ui/CopyButton.vue'
import InlineAlert from './ui/InlineAlert.vue'
import TextField from './ui/TextField.vue'

const emit = defineEmits<{ done: [] }>()

const setup = ref<TotpSetup | null>(null)
const loading = ref(true)
const loadError = ref('')
const code = ref('')
const busy = ref(false)
const submitError = ref('')

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    setup.value = await totpSetup()
  } catch (err) {
    loadError.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

function onCodeInput(value: string): void {
  code.value = value.replace(/\D/g, '').slice(0, 6)
}

async function confirm(): Promise<void> {
  if (busy.value) return
  submitError.value = ''
  if (code.value.length !== 6) {
    submitError.value = 'Enter the 6-digit code from your authenticator app.'
    return
  }
  busy.value = true
  try {
    const result = await totpSetupConfirm(code.value)
    applyAuthResult(result)
    try {
      // Pull the admin identity and updated factor list now that the session is full.
      await refreshSession()
    } catch {
      // The factor is already enabled; session metadata is cosmetic here.
    }
    notify('Authenticator app linked.', 'success')
    emit('done')
  } catch (err) {
    submitError.value = errorMessage(err)
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div v-if="loading" class="flex items-center gap-2 text-sm text-muted">
      <span class="size-4 animate-spin rounded-full border-2 border-border border-t-accent" />
      Generating a new secret…
    </div>

    <template v-else-if="setup">
      <ol class="flex flex-col gap-4 text-sm text-muted">
        <li class="flex flex-col gap-2">
          <span class="font-medium text-fg">1. Scan this QR code</span>
          <img
            :src="setup.qr_png_data_url"
            alt="QR code for the authenticator app"
            width="160"
            height="160"
            class="size-40 rounded-lg border border-border bg-white p-2"
          />
        </li>
        <li class="flex flex-col gap-2">
          <span class="font-medium text-fg">2. Or enter this secret manually</span>
          <div class="flex flex-wrap items-center gap-2">
            <code
              class="min-w-0 break-all rounded-lg border border-border bg-surface-2 px-3 py-2 font-mono text-xs text-fg"
            >
              {{ setup.secret }}
            </code>
            <CopyButton :value="setup.secret" label="Copy secret" />
          </div>
        </li>
        <li class="flex flex-col gap-2">
          <span class="font-medium text-fg">3. Enter the 6-digit code it shows</span>
          <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div class="sm:w-44">
              <TextField
                :model-value="code"
                label="Verification code"
                inputmode="numeric"
                autocomplete="one-time-code"
                placeholder="123456"
                mono
                :maxlength="6"
                @update:model-value="onCodeInput"
                @keydown.enter.prevent="confirm"
              />
            </div>
            <AppButton :loading="busy" @click="confirm">Verify and enable</AppButton>
          </div>
        </li>
      </ol>

      <InlineAlert v-if="submitError" tone="error">{{ submitError }}</InlineAlert>
    </template>

    <InlineAlert v-else tone="error">
      <p>{{ loadError || 'Could not start authenticator setup.' }}</p>
      <AppButton variant="secondary" size="sm" class="mt-2" @click="load">Try again</AppButton>
    </InlineAlert>
  </div>
</template>
