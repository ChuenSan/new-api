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

import type { ModelRouteMetricsResponse } from '../types'
import {
  batchMetricsActions,
  getMetricsActionErrorMessage,
  metricsRowKey,
  patchMetricsResetUnknown,
  rowMetricsActions,
} from './metrics-reset'

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
  test('places reset_unknown after restore_auto only in row actions', () => {
    assert.deepEqual(batchMetricsActions, [
      'force_probe',
      'trip_open',
      'manual_disable',
      'restore_auto',
    ])
    assert.deepEqual(rowMetricsActions, [
      'force_probe',
      'trip_open',
      'manual_disable',
      'restore_auto',
      'reset_unknown',
    ])
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
