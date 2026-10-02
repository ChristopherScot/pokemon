import { after, test } from 'node:test'
import assert from 'node:assert/strict'

import { app, floor } from './server.ts'

after(() => app.close())

test('healthz reports ok', async () => {
  const res = await app.inject({ method: 'GET', url: '/healthz' })
  assert.equal(res.statusCode, 200)
  assert.equal(res.body.trim(), 'ok')
})

test('an unknown path is a 404', async () => {
  const res = await app.inject({ method: 'GET', url: '/nope' })
  assert.equal(res.statusCode, 404)
})

test('metrics are exposed in Prometheus format', async () => {
  await app.inject({ method: 'GET', url: '/battle/rename' })
  const res = await app.inject({ method: 'GET', url: '/metrics' })

  assert.equal(res.statusCode, 200)
  assert.match(String(res.headers['content-type']), /text\/plain/)
  assert.match(res.body, /http_requests_total\{[^}]*route="\/battle\/rename"/)
  assert.match(res.body, /nodejs_heap_size_total_bytes/)
})

// A raw URL as a label would give Prometheus unbounded cardinality.
test('an unrouted path is never used as a label', async () => {
  await app.inject({ method: 'GET', url: '/secret-token-value/deadbeef' })
  const res = await app.inject({ method: 'GET', url: '/metrics' })

  assert.doesNotMatch(res.body, /deadbeef/)
  assert.match(res.body, /http_requests_total\{[^}]*route="other"/)
})

// Fail-open cases matter more than the refusing one: probes and scrapers send
// no version and blocking them would turn a client bug into a service outage.
test('the client floor refuses only what it should', async () => {
  const cases: Array<[string, string, string, number]> = [
    ['no floor set', '', '0.1.0', 200],
    ['above the floor', '0.3.0', '0.4.0', 200],
    ['exactly at the floor', '0.3.0', '0.3.0', 200],
    ['below the floor', '0.3.0', '0.2.9', 410],
    ['no version sent fails open', '0.3.0', '', 200],
    ['unparseable fails open', '0.3.0', 'banana', 200],
    ['a v-prefixed floor still compares', 'v0.3.0', '0.2.0', 410],
  ]
  const was = floor.minVersion
  try {
    for (const [name, min, sent, want] of cases) {
      floor.minVersion = min
      const res = await app.inject({
        method: 'GET',
        url: '/battle/rename',
        headers: sent ? { 'client-version': sent } : {},
      })
      assert.equal(res.statusCode, want, name)
    }
  } finally {
    floor.minVersion = was
  }
})

// /healthz and /metrics must skip the floor or a raised floor crashloops the
// pod and blinds Alloy exactly when it needs eyes on.
test('probes and metrics survive an impossible floor', async () => {
  const was = floor.minVersion
  floor.minVersion = '99.99.99'
  try {
    for (const url of ['/healthz', '/metrics']) {
      const res = await app.inject({
        method: 'GET',
        url,
        // A version is sent on purpose: fail-open would pass without it.
        headers: { 'client-version': '0.0.1' },
      })
      assert.notEqual(res.statusCode, 410, `${url} must never be refused`)
    }
  } finally {
    floor.minVersion = was
  }
})
