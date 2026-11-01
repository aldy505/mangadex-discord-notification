import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
} from '@simplewebauthn/browser'

/* -------------------------------------------------------------------------- */
/* Types                                                                       */
/* -------------------------------------------------------------------------- */

export type SessionStage = 'anonymous' | 'password' | 'enroll' | 'full'

export interface Factors {
  totp: boolean
  passkeys: number
}

export interface AdminIdentity {
  id: string
  email: string
  created_at: number
}

export interface SessionInfo {
  stage: SessionStage
  authenticated: boolean
  admin: AdminIdentity | null
  csrf_token: string | null
  factors: Factors
  public_base_url: string
  /** False when public_base_url is not a domain, which WebAuthn requires. */
  passkeys_available: boolean
}

/** Shape returned by login, TOTP verification, passkey ceremonies and invite acceptance. */
export interface AuthResult {
  stage: SessionStage
  csrf_token?: string | null
  factors?: Factors
}

export interface InvitePreview {
  email: string
  expires_at: number
  valid: boolean
}

export type InviteState = 'pending' | 'used' | 'expired' | 'revoked'

export interface Invite {
  id: string
  email: string
  created_at: number
  expires_at: number
  state: InviteState
}

export interface CreatedInvite {
  id: string
  email: string
  created_at: number
  expires_at: number
  state: 'pending'
  url: string
}

export interface PasskeyInfo {
  id: string
  name: string
  created_at: number
  last_used_at: number | null
}

export interface AccountInfo {
  admin: AdminIdentity
  totp: boolean
  passkeys: PasskeyInfo[]
}

export interface AdminSummary {
  id: string
  email: string
  created_at: number
  totp: boolean
  passkey_count: number
}

export interface Webhook {
  name: string
  host: string
  masked_url: string
  default: boolean
}

export type SubscriptionKind = 'mangadex' | 'rss'

export interface Subscription {
  id: string
  kind: SubscriptionKind
  label: string
  source: string
  webhook: string
  languages: string[]
  interval_seconds: number
  enabled: boolean
  last_checked_at: number | null
  last_error: string | null
  created_at: number
}

export interface MangaPreview {
  manga_id: string
  title: string
  cover_url: string
  available_languages: string[]
  last_chapter: string
  status: string
}

export interface FeedPreview {
  title: string
  link: string
  description: string
  item_count: number
  latest_item_at: number | null
}

export interface AuditEntry {
  id: number
  at: number
  actor: string
  action: string
  target: string | null
  details: Record<string, unknown> | null
  ip: string | null
}

export interface AuditPage {
  entries: AuditEntry[]
  total: number
}

export interface TotpSetup {
  secret: string
  otpauth_url: string
  qr_png_data_url: string
}

export interface CreateMangaSubscription {
  kind: 'mangadex'
  manga_id: string
  webhook: string
  languages: string[]
  interval_seconds: number
}

export interface CreateFeedSubscription {
  kind: 'rss'
  url: string
  webhook: string
  interval_seconds: number
  label?: string
}

export type CreateSubscription = CreateMangaSubscription | CreateFeedSubscription

/* -------------------------------------------------------------------------- */
/* Errors                                                                      */
/* -------------------------------------------------------------------------- */

export class ApiError extends Error {
  readonly status: number
  readonly fields: Record<string, string>

  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.fields = fields
  }
}

/** Human readable message for any thrown value. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error && error.message) return error.message
  return 'Something went wrong. Please try again.'
}

/* -------------------------------------------------------------------------- */
/* CSRF + unauthorized plumbing                                                */
/* -------------------------------------------------------------------------- */

let csrfToken: string | null = null
let unauthorizedHandler: (() => void) | null = null

export function setCsrfToken(token: string | null): void {
  csrfToken = token
}

export function getCsrfToken(): string | null {
  return csrfToken
}

/** Registered once by the app bootstrap; fires when a session-bearing request 401s. */
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler
}

/* -------------------------------------------------------------------------- */
/* Transport                                                                   */
/* -------------------------------------------------------------------------- */

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
}

function isAuthPath(path: string): boolean {
  return path.startsWith('/api/auth/') || path === '/api/session'
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }

  const init: RequestInit = { method, credentials: 'same-origin', headers }

  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(options.body)
  }
  if (method !== 'GET' && csrfToken !== null) {
    headers['X-CSRF-Token'] = csrfToken
  }

  let response: Response
  try {
    response = await fetch(path, init)
  } catch {
    throw new ApiError(0, 'Network error. Check your connection and try again.')
  }

  if (response.status === 204) {
    return undefined as T
  }

  const text = await response.text()
  let payload: unknown = null
  if (text.length > 0) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }

  if (!response.ok) {
    // The API contract fixes the error envelope: {"error": string, "fields"?: object}.
    const record =
      typeof payload === 'object' && payload !== null && !Array.isArray(payload)
        ? (payload as { error?: unknown; fields?: unknown })
        : {}
    const message =
      typeof record.error === 'string' ? record.error : `Request failed (${response.status})`
    const fields: Record<string, string> = {}
    if (
      typeof record.fields === 'object' &&
      record.fields !== null &&
      !Array.isArray(record.fields)
    ) {
      for (const [field, text] of Object.entries(record.fields)) {
        if (typeof text === 'string') fields[field] = text
      }
    }
    if (response.status === 401 && !isAuthPath(path)) {
      unauthorizedHandler?.()
    }
    throw new ApiError(response.status, message, fields)
  }

  return payload as T
}

/* -------------------------------------------------------------------------- */
/* Session + auth                                                              */
/* -------------------------------------------------------------------------- */

export function getSession(): Promise<SessionInfo> {
  return request<SessionInfo>('/api/session')
}

export function login(email: string, password: string): Promise<AuthResult> {
  return request<AuthResult>('/api/auth/login', { method: 'POST', body: { email, password } })
}

export function verifyTotp(code: string): Promise<AuthResult> {
  return request<AuthResult>('/api/auth/totp', { method: 'POST', body: { code } })
}

export function logout(): Promise<void> {
  return request<void>('/api/auth/logout', { method: 'POST' })
}

export async function passkeyLoginBegin(): Promise<PublicKeyCredentialRequestOptionsJSON> {
  const result = await request<{ options: PublicKeyCredentialRequestOptionsJSON }>(
    '/api/auth/passkey/login/begin',
    { method: 'POST' },
  )
  return result.options
}

export function passkeyLoginFinish(
  response: AuthenticationResponseJSON,
): Promise<AuthResult> {
  // The raw ceremony response is forwarded verbatim; it is a JSON blob by design.
  return request<AuthResult>('/api/auth/passkey/login/finish', {
    method: 'POST',
    body: response,
  })
}

export async function totpSetup(): Promise<TotpSetup> {
  return request<TotpSetup>('/api/auth/totp/setup', { method: 'POST' })
}

export function totpSetupConfirm(code: string): Promise<AuthResult> {
  return request<AuthResult>('/api/auth/totp/setup/confirm', { method: 'POST', body: { code } })
}

export async function passkeyRegisterBegin(
  name: string,
): Promise<PublicKeyCredentialCreationOptionsJSON> {
  const result = await request<{ options: PublicKeyCredentialCreationOptionsJSON }>(
    '/api/auth/passkey/register/begin',
    { method: 'POST', body: { name } },
  )
  return result.options
}

export function passkeyRegisterFinish(
  response: RegistrationResponseJSON,
): Promise<AuthResult> {
  return request<AuthResult>('/api/auth/passkey/register/finish', {
    method: 'POST',
    body: response,
  })
}

/* -------------------------------------------------------------------------- */
/* Invites                                                                     */
/* -------------------------------------------------------------------------- */

export function getInvite(token: string): Promise<InvitePreview> {
  return request<InvitePreview>(`/api/invites/${encodeURIComponent(token)}`)
}

export function acceptInvite(token: string, password: string): Promise<AuthResult> {
  return request<AuthResult>(`/api/invites/${encodeURIComponent(token)}/accept`, {
    method: 'POST',
    body: { password },
  })
}

export function createInvite(email: string): Promise<CreatedInvite> {
  return request<CreatedInvite>('/api/invites', { method: 'POST', body: { email } })
}

export async function listInvites(): Promise<Invite[]> {
  const result = await request<{ invites: Invite[] }>('/api/invites')
  return result.invites
}

export function revokeInvite(id: string): Promise<void> {
  return request<void>(`/api/invites/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/* -------------------------------------------------------------------------- */
/* Account                                                                     */
/* -------------------------------------------------------------------------- */

export function getAccount(): Promise<AccountInfo> {
  return request<AccountInfo>('/api/account')
}

export function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  return request<void>('/api/account/password', {
    method: 'PUT',
    body: { current_password: currentPassword, new_password: newPassword },
  })
}

export function deleteTotp(): Promise<void> {
  return request<void>('/api/account/totp', { method: 'DELETE' })
}

export function deletePasskey(id: string): Promise<void> {
  return request<void>(`/api/account/passkeys/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/* -------------------------------------------------------------------------- */
/* Admins                                                                      */
/* -------------------------------------------------------------------------- */

export async function listAdmins(): Promise<AdminSummary[]> {
  const result = await request<{ admins: AdminSummary[] }>('/api/admins')
  return result.admins
}

export function deleteAdmin(id: string): Promise<void> {
  return request<void>(`/api/admins/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/* -------------------------------------------------------------------------- */
/* Webhooks + subscriptions                                                    */
/* -------------------------------------------------------------------------- */

export async function listWebhooks(): Promise<Webhook[]> {
  const result = await request<{ webhooks: Webhook[] }>('/api/webhooks')
  return result.webhooks
}

export async function listSubscriptions(): Promise<Subscription[]> {
  const result = await request<{ subscriptions: Subscription[] }>('/api/subscriptions')
  return result.subscriptions
}

export function createSubscription(payload: CreateSubscription): Promise<Subscription> {
  return request<Subscription>('/api/subscriptions', { method: 'POST', body: payload })
}

export function deleteSubscription(id: string): Promise<void> {
  return request<void>(`/api/subscriptions/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function previewMangadex(input: string): Promise<MangaPreview> {
  return request<MangaPreview>('/api/preview/mangadex', { method: 'POST', body: { input } })
}

export function previewFeed(url: string): Promise<FeedPreview> {
  return request<FeedPreview>('/api/preview/rss', { method: 'POST', body: { url } })
}

/* -------------------------------------------------------------------------- */
/* Audit                                                                       */
/* -------------------------------------------------------------------------- */

export function listAudit(limit: number, offset: number): Promise<AuditPage> {
  const query = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  return request<AuditPage>(`/api/audit?${query.toString()}`)
}
