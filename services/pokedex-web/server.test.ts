import { after, test } from 'node:test'
import assert from 'node:assert/strict'

import { app } from './server.ts'

// app.inject() drives the real routes and hooks without binding a port,
// so these are fast and need no cleanup between cases.
after(() => app.close())

test('healthz reports ok', async () => {
  const res = await app.inject({ method: 'GET', url: '/healthz' })
  assert.equal(res.statusCode, 200)
  assert.equal(res.body.trim(), 'ok')
})

// The root route renders the card grid from the pokedex API. With no API
// reachable - which is the case in CI - it must answer 502 rather than
// throw: the UI being down because its dependency is down is a state
// worth reporting, and a stack trace to the visitor is not.
//
// POKEDEX_URL points at a port that nothing is listening on, so this
// exercises the failure path deterministically instead of depending on
// how an unresolvable hostname behaves on the test machine.
test('the card grid reports 502 when the API is unreachable', async () => {
  const res = await app.inject({ method: 'GET', url: '/' })
  assert.equal(res.statusCode, 502)
  assert.match(res.body, /pokedex API unavailable/)
})

test('an unknown path is a 404', async () => {
  const res = await app.inject({ method: 'GET', url: '/nope' })
  assert.equal(res.statusCode, 404)
})

test('metrics are exposed in Prometheus format', async () => {
  await app.inject({ method: 'GET', url: '/' })
  const res = await app.inject({ method: 'GET', url: '/metrics' })

  assert.equal(res.statusCode, 200)
  // String(), because a header can be absent and strict mode says so.
  assert.match(String(res.headers['content-type']), /text\/plain/)
  // The counter the deployment's scrape annotation exists to collect.
  assert.match(res.body, /http_requests_total\{[^}]*route="\/"/)
  // prom-client's process and heap metrics.
  assert.match(res.body, /nodejs_heap_size_total_bytes/)
})

// The route label is what reaches the log aggregator and the metric
// labels. A raw URL there would write out whatever a caller put in the
// path - a token, an id - and give Prometheus unbounded cardinality.
test('an unrouted path is never used as a label', async () => {
  await app.inject({ method: 'GET', url: '/secret-token-value/deadbeef' })
  const res = await app.inject({ method: 'GET', url: '/metrics' })

  assert.doesNotMatch(res.body, /deadbeef/)
  assert.match(res.body, /http_requests_total\{[^}]*route="other"/)
})

// Battle mode. These drive the real routes through app.inject(), so the
// cookie handling and the API proxying are exercised rather than mocked.
test('the lobby reports 502 when the API is unreachable', async () => {
  // Same contract as the card grid: the tests run with no pokedex
  // behind them, so an unreachable API is the case that can actually be
  // asserted here. A lobby that 500s instead would page someone.
  const res = await app.inject({ method: 'GET', url: '/battle' })
  assert.equal(res.statusCode, 502)
})

test('a battle page redirects to the lobby when nobody is registered', async () => {
  // Without this a visitor following a shared link lands on a board
  // they cannot act on and nothing explains why.
  const res = await app.inject({ method: 'GET', url: '/battle/abc123' })
  assert.equal(res.statusCode, 302)
  assert.equal(res.headers.location, '/battle')
})

test('acting without a trainer is refused rather than passed to the API', async () => {
  for (const url of ['/battle/open', '/battle/abc/turn', '/battle/abc/join']) {
    const res = await app.inject({ method: 'POST', url, payload: {} })
    assert.equal(res.statusCode, 401, `${url} should need a trainer`)
  }
})

test('registering with no name is rejected before it reaches the API', async () => {
  const res = await app.inject({ method: 'POST', url: '/battle/register', payload: { name: '  ' } })
  assert.equal(res.statusCode, 400)
})

test('every battle route reports 502 when the API is unreachable', async () => {
  // openapi-fetch THROWS on a refused connection rather than returning
  // an error field, so a route without the shared guard 500s. One
  // ungarded route is enough to page someone at 3am for a dependency
  // being down.
  const cookie = 'trainer=' + encodeURIComponent(JSON.stringify({ name: 'x', token: 't' }))
  // `as const` so method narrows to Fastify's HTTPMethods rather than
  // widening to string, which its inject() signature rejects.
  for (const [method, url] of [
    ['GET', '/battle/abc/state'],
    ['POST', '/battle/open'],
    ['POST', '/battle/abc/join'],
    ['POST', '/battle/abc/turn'],
  ] as const) {
    const res = await app.inject({ method, url, headers: { cookie }, payload: {} })
    assert.equal(res.statusCode, 502, `${method} ${url} should be 502, got ${res.statusCode}`)
  }
})

// A trainer who is not in a battle must still get a board.
//
// render() found its own side with findIndex, which returns -1 for a
// spectator; `1 - (-1)` is 2, so BOTH sides came back undefined and the
// first `.team` threw. Nothing caught it, so the board sat on its
// initial "loading…" forever with no error the player could see.
//
// Two ordinary paths reach it: opening a battle link somebody sent you,
// and holding a trainer cookie across a deploy, since battles live in
// the server's memory and a rollout clears them.
//
// Runs the real browser script out of the page rather than matching its
// text - the arithmetic is the bug, and only executing it proves the
// guard works.
test('a spectator gets a board instead of an endless spinner', async () => {
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'not-playing' })

  const open = page.match(/<script[^>]*>/)
  assert.ok(open, 'the battle page should ship a script')
  const start = open.index! + open[0].length
  const script = page.slice(start, page.lastIndexOf('</script>'))

  // Minimal DOM: the script only needs these to render a board.
  const board: { innerHTML: string; className: string; scrollTop: number } = {
    innerHTML: 'loading…', className: '', scrollTop: 0,
  }
  const els: Record<string, unknown> = { board, log: board }
  const sandbox = {
    document: {
      getElementById: (id: string) => els[id] ?? null,
      addEventListener: () => {},
    },
    location: { pathname: '/battle/abc123', reload: () => {} },
    fetch: async () => ({ ok: false, status: 404, json: async () => ({}) }),
    setTimeout: () => 0,
    setInterval: () => 0,
    requestAnimationFrame: () => 0,
    console,
  }

  // Pull render() out of the script and call it with a battle whose
  // only side belongs to somebody else.
  const run = new Function(
    ...Object.keys(sandbox),
    script + '\n;return { render };',
  ) as (...a: unknown[]) => { render: (b: unknown) => void }

  const { render } = run(...Object.values(sandbox))
  render({
    id: 'abc123',
    status: 'waiting',
    version: 1,
    turn: null,
    winner: null,
    log: [],
    sides: [{
      trainer: 'someone-else',
      team: [{
        name: 'onix', types: ['rock'], hp: 95, maxHp: 95,
        fainted: false, sprite: '', moves: [],
      }],
    }],
  })

  assert.notEqual(board.innerHTML, 'loading…',
    'the board never rendered - a spectator is stuck on the spinner')
  assert.match(board.innerHTML, /someone-else/,
    'the spectator board should show the trainer already waiting')
})
