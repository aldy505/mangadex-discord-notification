import { reactive } from 'vue'

export type ToastTone = 'success' | 'error' | 'info'

export interface Toast {
  id: number
  tone: ToastTone
  message: string
}

export const toasts = reactive<Toast[]>([])

let nextToastId = 1
const DISMISS_AFTER_MS = 6000

export function dismissToast(id: number): void {
  const index = toasts.findIndex((toast) => toast.id === id)
  if (index !== -1) toasts.splice(index, 1)
}

export function notify(message: string, tone: ToastTone = 'info'): void {
  const id = nextToastId++
  toasts.push({ id, tone, message })
  window.setTimeout(() => dismissToast(id), DISMISS_AFTER_MS)
}
