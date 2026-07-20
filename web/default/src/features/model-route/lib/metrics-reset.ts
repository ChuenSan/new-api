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
import type { ModelRouteMetrics, ModelRouteMetricsResponse } from '../types'

export const batchMetricsActions = [
  'force_probe',
  'trip_open',
  'manual_disable',
  'restore_auto',
] as const

export const rowMetricsActions = [
  ...batchMetricsActions,
  'reset_unknown',
] as const

export type BatchMetricsAction = (typeof batchMetricsActions)[number]
export type MetricsAction = (typeof rowMetricsActions)[number]

export function isBatchMetricsAction(
  value: unknown
): value is BatchMetricsAction {
  return (
    typeof value === 'string' &&
    batchMetricsActions.includes(value as BatchMetricsAction)
  )
}

export function isMetricsAction(value: unknown): value is MetricsAction {
  return (
    typeof value === 'string' &&
    rowMetricsActions.includes(value as MetricsAction)
  )
}

export function metricsRowKey(
  row: Pick<ModelRouteMetrics, 'channel_id' | 'effective_model'>
) {
  return `${row.channel_id}:${row.effective_model}`
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
