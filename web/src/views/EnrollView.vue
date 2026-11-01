<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import AuthScreen from '../components/AuthScreen.vue'
import PasskeySetupCard from '../components/PasskeySetupCard.vue'
import TotpSetupCard from '../components/TotpSetupCard.vue'
import AppButton from '../components/ui/AppButton.vue'
import InlineAlert from '../components/ui/InlineAlert.vue'
import StatusBadge from '../components/ui/StatusBadge.vue'
import { session } from '../lib/auth'

const router = useRouter()
const showTotp = ref(false)

async function finish(): Promise<void> {
  if (router.currentRoute.value.name === 'subscriptions') return
  await router.replace({ name: 'subscriptions' })
}

// Completing enrollment flips the session to `full`, which swaps the app shell
// and unmounts this view before a child's `done` event can navigate. Watching
// the stage guarantees the redirect happens either way.
watch(
  () => session.stage,
  (stage) => {
    if (stage === 'full') void finish()
  },
  { immediate: true },
)
</script>

<template>
  <AuthScreen
    title="Protect your account"
    subtitle="Your password is set. Add a second factor now — this screen cannot be skipped."
    width="md"
  >
    <div class="flex flex-col gap-5">
      <InlineAlert tone="warning">
        You must set up at least one second factor to continue.
      </InlineAlert>

      <section v-if="session.passkeysAvailable" class="rounded-lg border border-border bg-surface-2/60 p-4">
        <header class="mb-3 flex flex-wrap items-center gap-2">
          <h2 class="text-sm font-semibold text-fg">Passkey</h2>
          <StatusBadge tone="accent">Recommended</StatusBadge>
        </header>
        <PasskeySetupCard button-label="Set up passkey" @done="finish" />
      </section>

      <div
        v-if="session.passkeysAvailable"
        class="flex items-center gap-3 text-xs uppercase tracking-wide text-muted"
      >
        <span class="h-px flex-1 bg-border" />
        or
        <span class="h-px flex-1 bg-border" />
      </div>

      <section class="rounded-lg border border-border p-4">
        <header class="flex flex-wrap items-center justify-between gap-2">
          <h2 class="text-sm font-semibold text-fg">Authenticator app</h2>
          <AppButton v-if="!showTotp" variant="secondary" size="sm" @click="showTotp = true">
            Use an authenticator app instead
          </AppButton>
          <AppButton v-else variant="ghost" size="sm" @click="showTotp = false">Hide</AppButton>
        </header>
        <p v-if="!showTotp" class="mt-2 text-sm text-muted">
          Get 6-digit codes from an app such as 1Password, Authy, or Google Authenticator.
        </p>
        <div v-else class="mt-3">
          <TotpSetupCard @done="finish" />
        </div>
      </section>
    </div>
  </AuthScreen>
</template>
