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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { ModelRouteMetrics, ModelRouteMetricsResponse } from '../types'
import {
  batchMetricsActions,
  buildMetricsActionItems,
  buildMetricsActionRequest,
  findMetricsRowsForLog,
  getMetricsActionErrorMessage,
  isMetricsRowVisible,
  metricsRowKey,
  patchMetricsRateLimitThreshold,
  patchMetricsResetUnknown,
  rowMetricsActions,
} from './metrics-reset'

// CHANNEL_STATUS.ENABLED — kept as a literal here so the test stays free of the
// channels feature import (node --test has no path-alias loader); the runtime
// caller passes the real constant from @/features/channels/constants.
const ENABLED = 1

const response: ModelRouteMetricsResponse = {
  success: true,
  message: '',
  data: [
    {
      channel_id: 1,
      effective_model: 'target',
      route_state: 'OPEN',
      role: 'PRIMARY',
      backoff_level: 3,
      cooldown_until: 100,
      last_error_class: 'DETERMINISTIC',
    },
    {
      channel_id: 1,
      effective_model: 'other',
      route_state: 'HEALTHY',
      role: 'OVERFLOW',
    },
  ],
}

describe('model route metrics reset helpers', () => {
  test('shares one action definition between row and batch selectors', () => {
    assert.equal(batchMetricsActions, rowMetricsActions)
    assert.deepEqual(rowMetricsActions, [
      'force_probe',
      'trip_open',
      'manual_disable',
      'restore_auto',
      'reset_unknown',
    ])
  })

  test('builds localized select items without exposing action values', () => {
    const labels = {
      force_probe: '强制探测',
      trip_open: '立即熔断',
      manual_disable: '手动禁用',
      restore_auto: '恢复自动',
      reset_unknown: '重置为未知',
    }

    assert.deepEqual(buildMetricsActionItems(rowMetricsActions, labels), [
      { value: 'force_probe', label: '强制探测' },
      { value: 'trip_open', label: '立即熔断' },
      { value: 'manual_disable', label: '手动禁用' },
      { value: 'restore_auto', label: '恢复自动' },
      { value: 'reset_unknown', label: '重置为未知' },
    ])
  })

  test('builds exact reset requests for the selected channel and model', () => {
    for (const channel_id of [35, 104]) {
      assert.deepEqual(
        buildMetricsActionRequest(
          {
            channel_id,
            effective_model: 'gpt-5.5',
          },
          'reset_unknown'
        ),
        {
          channel_id,
          effective_model: 'gpt-5.5',
          action: 'reset_unknown',
        }
      )
    }
  })

  test('patches only the target row without mutating cached data', () => {
    const patched = patchMetricsResetUnknown(response, {
      channel_id: 1,
      effective_model: 'target',
    })

    assert.notEqual(patched, response)
    assert.notEqual(patched?.data, response.data)
    assert.notEqual(patched?.data[0], response.data[0])
    assert.equal(patched?.data[1], response.data[1])
    assert.deepEqual(patched?.data[0], {
      ...response.data[0],
      route_state: 'UNKNOWN',
      role: 'NONE',
      backoff_level: 0,
      cooldown_until: null,
      last_error_class: '',
    })
    assert.equal(response.data[0].route_state, 'OPEN')
  })

  test('keeps the original cache reference when the target is absent', () => {
    assert.equal(
      patchMetricsResetUnknown(response, {
        channel_id: 2,
        effective_model: 'target',
      }),
      response
    )
  })

  test('patches only one route threshold and supports clearing the override', () => {
    const patched = patchMetricsRateLimitThreshold(
      response,
      { channel_id: 1, effective_model: 'target' },
      9
    )
    assert.notEqual(patched, response)
    assert.equal(patched?.data[0].rate_limit_circuit_breaker_threshold, 9)
    assert.equal(patched?.data[1], response.data[1])

    const cleared = patchMetricsRateLimitThreshold(
      patched,
      { channel_id: 1, effective_model: 'target' },
      null
    )
    assert.equal(cleared?.data[0].rate_limit_circuit_breaker_threshold, null)
    assert.equal(
      response.data[0].rate_limit_circuit_breaker_threshold,
      undefined
    )
  })

  test('builds isolated pending keys for channel and effective model', () => {
    assert.notEqual(
      metricsRowKey({ channel_id: 1, effective_model: 'target' }),
      metricsRowKey({ channel_id: 2, effective_model: 'target' })
    )
    assert.notEqual(
      metricsRowKey({ channel_id: 1, effective_model: 'target' }),
      metricsRowKey({ channel_id: 1, effective_model: 'other' })
    )
  })

  test('prefers the backend response message for request failures', () => {
    assert.equal(
      getMetricsActionErrorMessage({
        message: 'Request failed',
        response: { data: { message: 'metrics row not found' } },
      }),
      'metrics row not found'
    )
    assert.equal(
      getMetricsActionErrorMessage(new Error('network failed')),
      'network failed'
    )
  })
})

describe('isMetricsRowVisible', () => {
  test('hides rows whose channel no longer exists', () => {
    assert.equal(
      isMetricsRowVisible(
        { channel_exists: false, channel_status: undefined },
        ENABLED
      ),
      false
    )
  })

  test('hides rows whose channel is not enabled', () => {
    assert.equal(
      isMetricsRowVisible(
        {
          channel_exists: true,
          channel_status: 2,
        },
        ENABLED
      ),
      false
    )
  })

  test('shows rows with an undefined channel_status', () => {
    assert.equal(
      isMetricsRowVisible(
        { channel_exists: true, channel_status: undefined },
        ENABLED
      ),
      true
    )
  })

  test('shows rows whose channel is enabled', () => {
    assert.equal(
      isMetricsRowVisible(
        { channel_exists: true, channel_status: ENABLED },
        ENABLED
      ),
      true
    )
  })
})

describe('findMetricsRowsForLog', () => {
  const rows: ModelRouteMetrics[] = [
    {
      channel_id: 16,
      effective_model: 'grok-4.5',
      requested_models: ['grok-4.5', 'grok-4.6'],
      route_state: 'HEALTHY',
    },
    {
      channel_id: 16,
      effective_model: 'gpt-5',
      requested_models: ['gpt-5'],
      route_state: 'HEALTHY',
    },
    {
      channel_id: 17,
      effective_model: 'grok-4.5',
      requested_models: ['grok-4.5'],
      route_state: 'HEALTHY',
    },
    {
      channel_id: 16,
      effective_model: 'claude-opus',
      requested_models: ['claude-opus'],
      route_state: 'HEALTHY',
    },
  ]

  test('returns [] when the requested model is empty', () => {
    assert.deepEqual(findMetricsRowsForLog(rows, 16, ''), [])
  })

  test('hits a row by effective_model', () => {
    const hits = findMetricsRowsForLog(rows, 16, 'gpt-5')
    assert.deepEqual(
      hits.map((r) => r.effective_model),
      ['gpt-5']
    )
  })

  test('hits a row by requested_models when effective_model differs', () => {
    const hits = findMetricsRowsForLog(rows, 16, 'grok-4.6')
    assert.deepEqual(
      hits.map((r) => r.effective_model),
      ['grok-4.5']
    )
  })

  test('selects every row on the channel that declares the requested model', () => {
    // Two rows on channel 16 both list grok-4.5 (one as effective, one in requested_models).
    const hits = findMetricsRowsForLog(rows, 16, 'grok-4.5')
    assert.deepEqual(
      hits.map((r) => r.effective_model),
      ['grok-4.5']
    )
  })

  test('does not cross channel boundaries', () => {
    const hits = findMetricsRowsForLog(rows, 17, 'gpt-5')
    assert.deepEqual(hits, [])
  })

  test('returns [] when no row matches', () => {
    assert.deepEqual(findMetricsRowsForLog(rows, 16, 'nope'), [])
  })
})
