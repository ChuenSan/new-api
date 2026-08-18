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
export type ModelRoutePolicy = {
  channel_id: number
  channel_name?: string
  base_url?: string
  channel_status?: number
  channel_exists?: boolean
  requested_model: string
  effective_model?: string
  manual_priority: number
  enabled: boolean
  source: string
  created_at?: number
  updated_at?: number
}

export type ModelRouteMetrics = {
  channel_id: number
  channel_name?: string
  base_url?: string
  channel_status?: number
  channel_exists?: boolean
  effective_model: string
  requested_models?: string[]
  rate_limit_circuit_breaker_threshold?: number | null
  rate_limit_circuit_breaker_effective_threshold?: number
  route_state: string
  role?: string
  is_stale?: boolean
  experience_score?: number | null
  production_success_ema?: number | null
  production_ttft_ema_ms?: number | null
  rate_limit_ema?: number | null
  stream_interruption_ema?: number | null
  backoff_level?: number
  cooldown_until?: number | null
  last_error_class?: string
  last_success_at?: number | null
  last_probe_at?: number | null
  last_request_at?: number | null
}

export type UpdatePolicyPriorityRequest = {
  channel_id: number
  requested_model: string
  manual_priority: number
  expected_manual_priority: number
  conflict_strategy: 'swap'
}

export type ModelPolicyPrioritySnapshot = {
  channel_id: number
  manual_priority: number
}

export type ModelPolicyPriorityChange = {
  channel_id: number
  manual_priority: number
}

export type ModelPolicyPriorityMutationData = {
  requested_model: string
  changed: ModelPolicyPriorityChange[]
  policies: ModelRoutePolicy[]
}

export type ModelPolicyPriorityMutationResponse = {
  success: boolean
  message: string
  code?: string
  data: ModelPolicyPriorityMutationData
}

export type ReorderModelRoutePoliciesRequest = {
  requested_model: string
  ordered_channel_ids: number[]
  expected: ModelPolicyPrioritySnapshot[]
  moved_channel_id?: number
}

export type MetricsActionRequest = {
  channel_id: number
  effective_model: string
  action:
    | 'trip_open'
    | 'force_probe'
    | 'manual_disable'
    | 'restore_auto'
    | 'reset_unknown'
}

export type UpdateRateLimitCircuitBreakerThresholdRequest = {
  channel_id: number
  effective_model: string
  threshold: number | null
}

export type ModelRouteMetricsResponse = {
  success: boolean
  message: string
  data: ModelRouteMetrics[]
}

export type ResetLearningRequest = {
  channel_id?: number
  effective_model?: string
  confirm?: boolean
}
