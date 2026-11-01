import { reactive } from 'vue'
import {
  getSession,
  setCsrfToken,
  type AdminIdentity,
  type AuthResult,
  type Factors,
  type SessionInfo,
  type SessionStage,
} from './api'

export interface SessionState {
  loaded: boolean
  stage: SessionStage
  authenticated: boolean
  admin: AdminIdentity | null
  factors: Factors
  publicBaseUrl: string
  passkeysAvailable: boolean
}

export const session = reactive<SessionState>({
  loaded: false,
  stage: 'anonymous',
  authenticated: false,
  admin: null,
  factors: { totp: false, passkeys: 0 },
  publicBaseUrl: '',
  passkeysAvailable: true,
})

export function applySession(info: SessionInfo): void {
  session.loaded = true
  session.stage = info.stage
  session.authenticated = info.authenticated
  session.admin = info.admin
  session.factors = { ...info.factors }
  session.publicBaseUrl = info.public_base_url
  session.passkeysAvailable = info.passkeys_available
  setCsrfToken(info.csrf_token)
}

export async function refreshSession(): Promise<SessionInfo> {
  const info = await getSession()
  applySession(info)
  return info
}

/**
 * Applies the payload of login / TOTP / passkey / invite-accept responses.
 * Those payloads carry a partial session, so callers that need the full
 * identity (admin email, factor list) follow up with `refreshSession()`.
 */
export function applyAuthResult(result: AuthResult): void {
  session.loaded = true
  session.stage = result.stage
  session.authenticated = result.stage === 'full'
  if (result.factors) session.factors = { ...result.factors }
  if (typeof result.csrf_token === 'string') setCsrfToken(result.csrf_token)
}

export function clearSession(): void {
  session.loaded = false
  session.stage = 'anonymous'
  session.authenticated = false
  session.admin = null
  session.factors = { totp: false, passkeys: 0 }
  session.publicBaseUrl = ''
  setCsrfToken(null)
}

export function factorCount(): number {
  return (session.factors.totp ? 1 : 0) + session.factors.passkeys
}
