<script setup lang="ts">
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { ref } from 'vue'
import { registerPasskey, webAuthnErrorMessage } from '../lib/webauthn'
import { notify } from '../lib/toast'
import AppButton from './ui/AppButton.vue'
import InlineAlert from './ui/InlineAlert.vue'
import TextField from './ui/TextField.vue'

const props = withDefaults(
  defineProps<{
    buttonLabel?: string
    defaultName?: string
    description?: string
  }>(),
  {
    buttonLabel: 'Set up passkey',
    defaultName: 'This device',
    description:
      'Passkeys use your device unlock (fingerprint, face, or PIN). They cannot be phished and need no code to type.',
  },
)

const emit = defineEmits<{ done: [] }>()

const supported = browserSupportsWebAuthn()
const name = ref(props.defaultName)
const busy = ref(false)
const error = ref('')

async function submit(): Promise<void> {
  if (busy.value) return
  error.value = ''
  const label = name.value.trim()
  if (label.length === 0) {
    error.value = 'Give this passkey a name so you can recognise it later.'
    return
  }
  busy.value = true
  try {
    await registerPasskey(label)
    notify('Passkey registered.', 'success')
    emit('done')
  } catch (err) {
    error.value = webAuthnErrorMessage(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <p class="text-sm text-muted">{{ description }}</p>

    <InlineAlert v-if="!supported" tone="warning">
      This browser does not support WebAuthn passkeys. Use an authenticator app instead.
    </InlineAlert>

    <InlineAlert v-if="error" tone="error">{{ error }}</InlineAlert>

    <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
      <div class="min-w-0 flex-1">
        <TextField
          v-model="name"
          label="Device name"
          placeholder="This device"
          autocomplete="off"
          :maxlength="60"
          @keydown.enter.prevent="submit"
        />
      </div>
      <AppButton :loading="busy" :disabled="!supported" @click="submit">
        {{ buttonLabel }}
      </AppButton>
    </div>
  </div>
</template>
