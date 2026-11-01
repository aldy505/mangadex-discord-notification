import { startAuthentication, startRegistration } from '@simplewebauthn/browser'
import {
  errorMessage,
  passkeyLoginBegin,
  passkeyLoginFinish,
  passkeyRegisterBegin,
  passkeyRegisterFinish,
} from './api'
import { applyAuthResult, refreshSession } from './auth'

/**
 * Session metadata (admin identity, factor list) is only available from
 * /api/session, so refresh after a ceremony. A failure here must not hide the
 * fact that the ceremony itself succeeded.
 */
async function refreshQuietly(): Promise<void> {
  try {
    await refreshSession()
  } catch {
    // Ignore: the session cookie is already established.
  }
}

export function webAuthnErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    switch (error.name) {
      case 'NotAllowedError':
        return 'The passkey prompt was dismissed, timed out, or was blocked.'
      case 'NotSupportedError':
        return 'This browser or device does not support passkeys.'
      case 'SecurityError':
        return 'Passkeys require a secure (HTTPS) origin.'
      case 'InvalidStateError':
        return 'This authenticator is already registered for the account.'
      case 'AbortError':
        return 'The passkey prompt was cancelled.'
    }
  }
  return errorMessage(error)
}

export async function signInWithPasskey(): Promise<void> {
  const options = await passkeyLoginBegin()
  const response = await startAuthentication({ optionsJSON: options })
  const result = await passkeyLoginFinish(response)
  applyAuthResult(result)
  await refreshQuietly()
}

export async function registerPasskey(name: string): Promise<void> {
  const options = await passkeyRegisterBegin(name)
  const response = await startRegistration({ optionsJSON: options })
  const result = await passkeyRegisterFinish(response)
  applyAuthResult(result)
  await refreshQuietly()
}
