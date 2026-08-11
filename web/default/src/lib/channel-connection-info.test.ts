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

import {
  encodeChannelConnectionInfo,
  parseChannelConnectionInfo,
} from './channel-connection-info'

describe('channel connection info', () => {
  test('encodes and parses connection info', () => {
    const encoded = encodeChannelConnectionInfo(
      'sk-test',
      'https://api.example.com/v1'
    )

    assert.deepEqual(parseChannelConnectionInfo(encoded), {
      key: 'sk-test',
      url: 'https://api.example.com/v1',
    })
  })

  test('parses JSON with surrounding whitespace', () => {
    assert.deepEqual(
      parseChannelConnectionInfo(
        '  {"_type":"newapi_channel_conn","key":"key","url":"url"}  '
      ),
      { key: 'key', url: 'url' }
    )
  })

  test('rejects missing, invalid, and malformed connection info', () => {
    const invalidValues: unknown[] = [
      null,
      undefined,
      '',
      'not json',
      JSON.stringify({ _type: 'other', key: 'key', url: 'url' }),
      JSON.stringify({ _type: 'newapi_channel_conn', url: 'url' }),
      JSON.stringify({ _type: 'newapi_channel_conn', key: 'key' }),
      JSON.stringify({ _type: 'newapi_channel_conn', key: 123, url: 'url' }),
      JSON.stringify({ _type: 'newapi_channel_conn', key: 'key', url: 123 }),
    ]

    for (const value of invalidValues) {
      assert.equal(
        parseChannelConnectionInfo(value as string | null | undefined),
        null
      )
    }
  })
})
