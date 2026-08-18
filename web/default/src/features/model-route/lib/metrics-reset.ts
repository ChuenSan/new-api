/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type {
  MetricsActionRequest,
  ModelRouteMetrics,
  ModelRouteMetricsResponse,
} from '../types'

export type MetricsAction = MetricsActionRequest['action']

export const metricsActions = [
  'force_probe',
  'trip_open',
  'manual_disable',
  'restore_auto',
  'reset_unknown',
] as const satisfies readonly MetricsAction[]

export const batchMetricsActions = metricsActions
export const rowMetricsActions = metricsActions

export type BatchMetricsAction = MetricsAction

export function buildMetricsActionItems<T extends MetricsAction>(
  actions: readonly T[],
  labels: Record<T, string>
) {
  return actions.map((value) => ({ value, label: labels[value] }))
}

export function isBatchMetricsAction(
  value: unknown
): value is BatchMetricsAction {
  return isMetricsAction(value)
}

export function isMetricsAction(value: unknown): value is MetricsAction {
  return (
    typeof value === 'string' && metricsActions.includes(value as MetricsAction)
  )
}

export function metricsRowKey(
  row: Pick<ModelRouteMetrics, 'channel_id' | 'effective_model'>
) {
  return `${row.channel_id}:${row.effective_model}`
}

// Display-layer visibility predicate for metrics rows. Mirrors the filter in
// ModelRouteAdmin: rows for missing or disabled channels are hidden but kept
// in data. Single source of truth — the table filter and log-jump seed both
// call this instead of duplicating the condition. The enabled channel status
// value is passed in so this lib stays free of the channels feature constant.
export function isMetricsRowVisible(
  row: Pick<ModelRouteMetrics, 'channel_exists' | 'channel_status'>,
  enabledChannelStatus: number
): boolean {
  if (row.channel_exists === false) return false
  if (
    row.channel_status !== undefined &&
    row.channel_status !== enabledChannelStatus
  ) {
    return false
  }
  return true
}

// Locate the metrics rows a usage log refers to. Matches on channel id and a
// requested model, hiting either the row's effective_model or its backend-built
// requested_models reverse index. Exact compare — effective_model is a route
// key. Returns all hits so multi-row matches can all be selected.
export function findMetricsRowsForLog(
  rows: readonly ModelRouteMetrics[],
  channelId: number,
  requestedModel: string
): ModelRouteMetrics[] {
  if (!requestedModel) return []
  return rows.filter(
    (row) =>
      row.channel_id === channelId &&
      (row.effective_model === requestedModel ||
        (row.requested_models ?? []).includes(requestedModel))
  )
}

export function buildMetricsActionRequest<T extends MetricsAction>(
  row: Pick<ModelRouteMetrics, 'channel_id' | 'effective_model'>,
  action: T
) {
  return {
    channel_id: row.channel_id,
    effective_model: row.effective_model,
    action,
  }
}

export function getMetricsActionErrorMessage(
  error: unknown
): string | undefined {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const response = error.response
    if (
      typeof response === 'object' &&
      response !== null &&
      'data' in response &&
      typeof response.data === 'object' &&
      response.data !== null &&
      'message' in response.data &&
      typeof response.data.message === 'string'
    ) {
      return response.data.message
    }
  }
  return error instanceof Error && error.message ? error.message : undefined
}

export function patchMetricsResetUnknown(
  response: ModelRouteMetricsResponse | undefined,
  target: Pick<ModelRouteMetrics, 'channel_id' | 'effective_model'>
) {
  if (!response) return response
  let changed = false
  const data = response.data.map((row) => {
    if (
      row.channel_id !== target.channel_id ||
      row.effective_model !== target.effective_model
    ) {
      return row
    }
    changed = true
    return {
      ...row,
      route_state: 'UNKNOWN',
      role: 'NONE',
      backoff_level: 0,
      cooldown_until: null,
      last_error_class: '',
    }
  })
  return changed ? { ...response, data } : response
}

export function patchMetricsRateLimitThreshold(
  response: ModelRouteMetricsResponse | undefined,
  target: Pick<ModelRouteMetrics, 'channel_id' | 'effective_model'>,
  threshold: number | null
) {
  if (!response) return response
  let changed = false
  const data = response.data.map((row) => {
    if (
      row.channel_id !== target.channel_id ||
      row.effective_model !== target.effective_model
    ) {
      return row
    }
    changed = true
    return {
      ...row,
      rate_limit_circuit_breaker_threshold: threshold,
    }
  })
  return changed ? { ...response, data } : response
}
