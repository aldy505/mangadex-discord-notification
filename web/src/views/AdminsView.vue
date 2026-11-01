<script setup lang="ts">
import { onMounted, ref } from 'vue'
import ConfirmDialog from '../components/ui/ConfirmDialog.vue'
import AppButton from '../components/ui/AppButton.vue'
import CopyButton from '../components/ui/CopyButton.vue'
import InlineAlert from '../components/ui/InlineAlert.vue'
import ModalDialog from '../components/ui/ModalDialog.vue'
import StatusBadge from '../components/ui/StatusBadge.vue'
import TextField from '../components/ui/TextField.vue'
import {
  ApiError,
  createInvite,
  deleteAdmin,
  errorMessage,
  listAdmins,
  listInvites,
  revokeInvite,
  type AdminSummary,
  type CreatedInvite,
  type Invite,
  type InviteState,
} from '../lib/api'
import { session } from '../lib/auth'
import { formatDateTime, formatExpiry } from '../lib/format'
import { notify } from '../lib/toast'

const invites = ref<Invite[]>([])
const admins = ref<AdminSummary[]>([])
const loading = ref(true)
const loadError = ref('')

const email = ref('')
const creating = ref(false)
const createError = ref('')
const created = ref<CreatedInvite | null>(null)
const createdDialogOpen = ref(false)

const revokeTarget = ref<Invite | null>(null)
const revokeOpen = ref(false)
const revoking = ref(false)

const adminTarget = ref<AdminSummary | null>(null)
const adminOpen = ref(false)
const deletingAdmin = ref(false)

const INVITE_TONES: Record<InviteState, 'success' | 'neutral' | 'warning' | 'danger'> = {
  pending: 'success',
  used: 'neutral',
  expired: 'warning',
  revoked: 'danger',
}

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const [inviteList, adminList] = await Promise.all([listInvites(), listAdmins()])
    invites.value = inviteList
    admins.value = adminList
  } catch (err) {
    loadError.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function submitInvite(): Promise<void> {
  createError.value = ''
  const address = email.value.trim()
  if (address.length === 0) {
    createError.value = 'Enter an email address.'
    return
  }
  creating.value = true
  try {
    const invite = await createInvite(address)
    created.value = invite
    createdDialogOpen.value = true
    email.value = ''
    invites.value = [invite, ...invites.value]
    notify(`Invite created for ${invite.email}.`, 'success')
  } catch (err) {
    createError.value =
      err instanceof ApiError && err.fields.email ? err.fields.email : errorMessage(err)
  } finally {
    creating.value = false
  }
}

function askRevoke(invite: Invite): void {
  revokeTarget.value = invite
  revokeOpen.value = true
}

async function confirmRevoke(): Promise<void> {
  const target = revokeTarget.value
  if (!target || revoking.value) return
  revoking.value = true
  try {
    await revokeInvite(target.id)
    invites.value = invites.value.map((invite) =>
      invite.id === target.id ? { ...invite, state: 'revoked' } : invite,
    )
    notify('Invite revoked.', 'success')
    revokeOpen.value = false
    revokeTarget.value = null
  } catch (err) {
    notify(errorMessage(err), 'error')
  } finally {
    revoking.value = false
  }
}

function askDeleteAdmin(admin: AdminSummary): void {
  adminTarget.value = admin
  adminOpen.value = true
}

async function confirmDeleteAdmin(): Promise<void> {
  const target = adminTarget.value
  if (!target || deletingAdmin.value) return
  deletingAdmin.value = true
  try {
    await deleteAdmin(target.id)
    admins.value = admins.value.filter((admin) => admin.id !== target.id)
    notify(`${target.email} removed.`, 'success')
    adminOpen.value = false
    adminTarget.value = null
  } catch (err) {
    notify(errorMessage(err), 'error')
  } finally {
    deletingAdmin.value = false
  }
}

function factorSummary(admin: AdminSummary): string {
  const parts: string[] = []
  if (admin.totp) parts.push('TOTP')
  if (admin.passkey_count > 0) {
    parts.push(`${admin.passkey_count} ${admin.passkey_count === 1 ? 'passkey' : 'passkeys'}`)
  }
  return parts.length > 0 ? parts.join(' · ') : 'No second factor'
}

function isSelf(admin: AdminSummary): boolean {
  return session.admin?.id === admin.id
}

function selectAll(event: Event): void {
  if (event.target instanceof HTMLInputElement) event.target.select()
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-6">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-xl font-semibold">Admins</h1>
        <p class="mt-1 text-sm text-muted">Invite teammates and manage admin access.</p>
      </div>
      <AppButton variant="secondary" :disabled="loading" @click="load">Refresh</AppButton>
    </header>

    <div v-if="loading" class="flex items-center gap-2 py-10 text-sm text-muted">
      <span class="size-4 animate-spin rounded-full border-2 border-border border-t-accent" />
      Loading admins…
    </div>

    <InlineAlert v-else-if="loadError" tone="error">
      <p>{{ loadError }}</p>
      <AppButton variant="secondary" size="sm" class="mt-2" @click="load">Retry</AppButton>
    </InlineAlert>

    <template v-else>
      <section class="flex flex-col gap-3">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-sm font-semibold">Pending invites</h2>
            <p class="text-xs text-muted">Invite links are single use and expire automatically.</p>
          </div>
        </div>

        <form
          class="flex flex-col gap-2 rounded-xl border border-border bg-surface p-4 sm:flex-row sm:items-end"
          @submit.prevent="submitInvite"
        >
          <div class="min-w-0 flex-1">
            <TextField
              v-model="email"
              label="Invite an admin by email"
              type="email"
              autocomplete="off"
              placeholder="teammate@example.com"
              :error="createError"
            />
          </div>
          <AppButton type="submit" :loading="creating">Create invite</AppButton>
        </form>

        <div
          v-if="invites.length === 0"
          class="rounded-xl border border-dashed border-border bg-surface px-4 py-8 text-center text-sm text-muted"
        >
          No invites yet.
        </div>

        <div v-else class="hidden rounded-xl border border-border bg-surface xl:block">
          <table class="w-full table-fixed text-left text-sm">
            <thead class="bg-surface-2 text-[11px] uppercase tracking-wide text-muted">
              <tr>
                <th scope="col" class="w-[38%] px-3 py-3 font-medium">Email</th>
                <th scope="col" class="w-[30%] px-3 py-3 font-medium">Expires</th>
                <th scope="col" class="w-[18%] px-3 py-3 font-medium">State</th>
                <th scope="col" class="w-[14%] px-3 py-3 text-right font-medium">
                  <span class="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              <tr v-for="invite in invites" :key="invite.id">
                <td class="px-3 py-3">
                  <span class="block truncate font-medium text-fg" :title="invite.email">
                    {{ invite.email }}
                  </span>
                </td>
                <td class="truncate px-3 py-3 text-xs text-muted" :title="formatExpiry(invite.expires_at)">{{ formatExpiry(invite.expires_at) }}</td>
                <td class="px-3 py-3">
                  <StatusBadge :tone="INVITE_TONES[invite.state]">{{ invite.state }}</StatusBadge>
                </td>
                <td class="px-3 py-3 text-right">
                  <AppButton
                    v-if="invite.state === 'pending'"
                    variant="ghost"
                    size="sm"
                    @click="askRevoke(invite)"
                  >
                    Revoke
                  </AppButton>
                  <span v-else class="text-xs text-muted">—</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <ul v-if="invites.length > 0" class="flex flex-col gap-2 xl:hidden">
          <li
            v-for="invite in invites"
            :key="invite.id"
            class="rounded-xl border border-border bg-surface p-4"
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <p class="truncate text-sm font-medium text-fg">{{ invite.email }}</p>
                <p class="mt-1 text-xs text-muted">{{ formatExpiry(invite.expires_at) }}</p>
              </div>
              <StatusBadge :tone="INVITE_TONES[invite.state]">{{ invite.state }}</StatusBadge>
            </div>
            <div v-if="invite.state === 'pending'" class="mt-2 flex justify-end">
              <AppButton variant="ghost" size="sm" @click="askRevoke(invite)">Revoke</AppButton>
            </div>
          </li>
        </ul>
      </section>

      <section class="flex flex-col gap-3">
        <div>
          <h2 class="text-sm font-semibold">Admins</h2>
          <p class="text-xs text-muted">Deleting an admin revokes their sessions immediately.</p>
        </div>

        <div class="hidden rounded-xl border border-border bg-surface xl:block">
          <table class="w-full table-fixed text-left text-sm">
            <thead class="bg-surface-2 text-[11px] uppercase tracking-wide text-muted">
              <tr>
                <th scope="col" class="w-[38%] px-3 py-3 font-medium">Email</th>
                <th scope="col" class="w-[28%] px-3 py-3 font-medium">Second factor</th>
                <th scope="col" class="w-[20%] px-3 py-3 font-medium">Created</th>
                <th scope="col" class="w-[14%] px-3 py-3 text-right font-medium">
                  <span class="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              <tr v-for="admin in admins" :key="admin.id">
                <td class="px-3 py-3">
                  <span class="block truncate font-medium text-fg" :title="admin.email">
                    {{ admin.email }}
                  </span>
                  <span v-if="isSelf(admin)" class="text-xs text-muted">You</span>
                </td>
                <td class="px-3 py-3">
                  <span class="block truncate text-xs" :class="admin.totp || admin.passkey_count > 0 ? 'text-fg' : 'text-warning'">
                    {{ factorSummary(admin) }}
                  </span>
                </td>
                <td class="truncate px-3 py-3 text-xs text-muted">
                  {{ formatDateTime(admin.created_at) }}
                </td>
                <td class="px-3 py-3 text-right">
                  <AppButton
                    variant="ghost"
                    size="sm"
                    :disabled="isSelf(admin)"
                    :title="isSelf(admin) ? 'You cannot delete your own account' : undefined"
                    @click="askDeleteAdmin(admin)"
                  >
                    Delete
                  </AppButton>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <ul class="flex flex-col gap-2 xl:hidden">
          <li
            v-for="admin in admins"
            :key="admin.id"
            class="rounded-xl border border-border bg-surface p-4"
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <p class="truncate text-sm font-medium text-fg">{{ admin.email }}</p>
                <p class="mt-1 text-xs text-muted">{{ factorSummary(admin) }}</p>
                <p class="mt-1 text-xs text-muted">Created {{ formatDateTime(admin.created_at) }}</p>
              </div>
              <AppButton
                variant="ghost"
                size="sm"
                :disabled="isSelf(admin)"
                :title="isSelf(admin) ? 'You cannot delete your own account' : undefined"
                @click="askDeleteAdmin(admin)"
              >
                Delete
              </AppButton>
            </div>
          </li>
        </ul>
      </section>
    </template>

    <ModalDialog
      v-model:open="createdDialogOpen"
      title="Invite link ready"
      description="Share this link with the new admin. It can only be used once."
      size="md"
    >
      <div v-if="created" class="flex flex-col gap-3">
        <p class="text-sm text-muted">
          Invite for <span class="font-medium text-fg">{{ created.email }}</span>, expires
          {{ formatExpiry(created.expires_at) }}.
        </p>
        <div class="flex flex-col gap-2 sm:flex-row">
          <input
            :value="created.url"
            readonly
            class="h-10 min-w-0 flex-1 rounded-lg border border-border bg-surface-2 px-3 font-mono text-xs text-fg"
            aria-label="Invite URL"
            @focus="selectAll"
          />
          <CopyButton :value="created.url" label="Copy link" />
        </div>
      </div>
      <template #footer>
        <AppButton @click="createdDialogOpen = false">Done</AppButton>
      </template>
    </ModalDialog>

    <ConfirmDialog
      v-model:open="revokeOpen"
      title="Revoke invite?"
      :message="revokeTarget ? `The link sent to ${revokeTarget.email} will stop working.` : undefined"
      confirm-label="Revoke invite"
      :busy="revoking"
      @confirm="confirmRevoke"
    />

    <ConfirmDialog
      v-model:open="adminOpen"
      title="Delete admin?"
      :message="
        adminTarget
          ? `${adminTarget.email} will lose access immediately. Their subscriptions stay in place.`
          : undefined
      "
      confirm-label="Delete admin"
      :busy="deletingAdmin"
      @confirm="confirmDeleteAdmin"
    />
  </div>
</template>
