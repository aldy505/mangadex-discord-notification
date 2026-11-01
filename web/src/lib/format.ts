const dateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})

const relativeFormatter = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

const PLACEHOLDER = '—'

export function formatDateTime(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return PLACEHOLDER
  return dateTimeFormatter.format(new Date(ms))
}

export function formatRelative(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return PLACEHOLDER
  const delta = ms - Date.now()
  const magnitude = Math.abs(delta)

  if (magnitude < 45_000) return relativeFormatter.format(0, 'second')
  if (magnitude < 45 * 60_000) return relativeFormatter.format(Math.round(delta / 60_000), 'minute')
  if (magnitude < 22 * 3_600_000) return relativeFormatter.format(Math.round(delta / 3_600_000), 'hour')
  if (magnitude < 26 * 86_400_000) return relativeFormatter.format(Math.round(delta / 86_400_000), 'day')
  return relativeFormatter.format(Math.round(delta / 2_592_000_000), 'month')
}

/** Relative time plus an absolute timestamp, e.g. "in 3 days (Nov 4, 2026, 09:00)". */
export function formatExpiry(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return PLACEHOLDER
  return `${formatRelative(ms)} (${formatDateTime(ms)})`
}

export function formatInterval(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return PLACEHOLDER
  if (seconds % 86_400 === 0) {
    const days = seconds / 86_400
    return days === 1 ? 'Daily' : `Every ${days} days`
  }
  if (seconds % 3_600 === 0) {
    const hours = seconds / 3_600
    return hours === 1 ? 'Hourly' : `Every ${hours} hours`
  }
  if (seconds % 60 === 0) {
    const minutes = seconds / 60
    return minutes === 1 ? 'Every minute' : `Every ${minutes} minutes`
  }
  return `Every ${seconds} seconds`
}

export function formatJson(value: unknown): string {
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}
