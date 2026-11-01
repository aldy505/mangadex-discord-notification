<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { logout } from '../lib/api'
import { clearSession, session } from '../lib/auth'
import { notify } from '../lib/toast'
import NavIcon from './NavIcon.vue'

const route = useRoute()
const router = useRouter()

const mobileNavOpen = ref(false)
const signingOut = ref(false)

const NAV_ITEMS = [
  { name: 'subscriptions', label: 'Subscriptions', to: '/', icon: 'subscriptions' },
  { name: 'admins', label: 'Admins', to: '/admins', icon: 'admins' },
  { name: 'audit', label: 'Audit log', to: '/audit', icon: 'audit' },
  { name: 'account', label: 'Account', to: '/account', icon: 'account' },
] as const

const email = computed(() => session.admin?.email ?? 'Signed in')
const initial = computed(() => (session.admin?.email ?? '?').slice(0, 1).toUpperCase())

watch(
  () => route.fullPath,
  () => {
    mobileNavOpen.value = false
  },
)

async function signOut(): Promise<void> {
  if (signingOut.value) return
  signingOut.value = true
  try {
    await logout()
  } catch {
    // The cookie may already be gone; the local state is cleared either way.
  } finally {
    clearSession()
    signingOut.value = false
    notify('Signed out.', 'success')
    await router.replace({ name: 'login' })
  }
}
</script>

<template>
  <div class="min-h-dvh bg-canvas">
    <header
      class="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-border bg-surface/95 px-3 backdrop-blur md:hidden"
    >
      <button
        type="button"
        class="rounded-lg p-2 text-muted transition-colors hover:bg-surface-2 hover:text-fg"
        :aria-expanded="mobileNavOpen"
        aria-label="Toggle navigation"
        @click="mobileNavOpen = !mobileNavOpen"
      >
        <NavIcon :name="mobileNavOpen ? 'close' : 'menu'" />
      </button>
      <span class="truncate text-sm font-semibold">MangaDex Notifications</span>
    </header>

    <nav
      v-if="mobileNavOpen"
      class="sticky top-14 z-20 border-b border-border bg-surface p-3 md:hidden"
      aria-label="Primary"
    >
      <ul class="flex flex-col gap-1">
        <li v-for="item in NAV_ITEMS" :key="item.name">
          <RouterLink
            :to="item.to"
            class="flex items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted transition-colors hover:bg-surface-2 hover:text-fg"
            :exact-active-class="item.to === '/' ? 'bg-surface-2 font-medium text-fg' : undefined"
            :active-class="item.to === '/' ? '' : 'bg-surface-2 font-medium text-fg'"
          >
            <NavIcon :name="item.icon" />
            {{ item.label }}
          </RouterLink>
        </li>
      </ul>
      <div class="mt-3 flex items-center justify-between gap-2 border-t border-border pt-3">
        <span class="truncate text-xs text-muted">{{ email }}</span>
        <button
          type="button"
          class="rounded-lg px-2 py-1 text-xs font-medium text-danger transition-colors hover:bg-danger-soft"
          :disabled="signingOut"
          @click="signOut"
        >
          Sign out
        </button>
      </div>
    </nav>

    <div class="mx-auto flex w-full max-w-7xl">
      <aside
        class="sticky top-0 hidden h-dvh w-60 shrink-0 flex-col border-r border-border bg-surface px-3 py-4 md:flex"
      >
        <RouterLink to="/" class="flex items-center gap-2 rounded-lg px-2 py-1.5">
          <span
            class="grid size-8 place-items-center rounded-lg bg-accent text-sm font-bold text-accent-fg"
            aria-hidden="true"
          >
            M
          </span>
          <span class="text-sm font-semibold leading-tight">MangaDex<br />Notifications</span>
        </RouterLink>

        <ul class="mt-6 flex flex-col gap-1" aria-label="Primary">
          <li v-for="item in NAV_ITEMS" :key="item.name">
            <RouterLink
              :to="item.to"
              class="flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted transition-colors hover:bg-surface-2 hover:text-fg"
              :active-class="item.to === '/' ? '' : 'bg-surface-2 font-medium text-fg'"
              :exact-active-class="item.to === '/' ? 'bg-surface-2 font-medium text-fg' : undefined"
            >
              <NavIcon :name="item.icon" />
              {{ item.label }}
            </RouterLink>
          </li>
        </ul>

        <div class="mt-auto border-t border-border pt-3">
          <div class="flex items-center gap-2.5 px-2">
            <span
              class="grid size-8 shrink-0 place-items-center rounded-full bg-surface-2 text-xs font-semibold text-fg"
              aria-hidden="true"
            >
              {{ initial }}
            </span>
            <span class="min-w-0 flex-1 truncate text-xs text-muted" :title="email">{{ email }}</span>
          </div>
          <button
            type="button"
            class="mt-2 flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted transition-colors hover:bg-danger-soft hover:text-danger disabled:opacity-60"
            :disabled="signingOut"
            @click="signOut"
          >
            <NavIcon name="logout" />
            Sign out
          </button>
        </div>
      </aside>

      <main class="min-w-0 flex-1 px-4 py-5 sm:px-6 sm:py-7">
        <slot />
      </main>
    </div>
  </div>
</template>
