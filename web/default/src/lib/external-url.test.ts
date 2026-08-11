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

import { normalizeExternalUrl } from './external-url'

describe('normalizeExternalUrl', () => {
  test('preserves HTTP(S) URLs and normalizes protocol-relative and bare hosts', () => {
    assert.equal(
      normalizeExternalUrl(' https://api.example.com/v1 '),
      'https://api.example.com/v1'
    )
    assert.equal(
      normalizeExternalUrl('http://api.example.com:8080/v1'),
      'http://api.example.com:8080/v1'
    )
    assert.equal(
      normalizeExternalUrl('//api.example.com/v1'),
      'https://api.example.com/v1'
    )
    assert.equal(
      normalizeExternalUrl('api.example.com:8443/v1'),
      'https://api.example.com:8443/v1'
    )
  })

  test('rejects empty, malformed, and non-HTTP(S) values', () => {
    for (const value of [
      undefined,
      '',
      'not a host',
      'localhost:3000',
      'javascript:alert(1)',
      'data:text/html,hello',
      'file:///tmp/example',
    ]) {
      assert.equal(normalizeExternalUrl(value), '')
    }
  })
})
