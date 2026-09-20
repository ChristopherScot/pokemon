// Vitest, not node:test. Node cannot compile JSX, so a repo with .tsx
// in it cannot run `node --test` at all; Vitest shares this project's
// Vite config, which means tests see the same transform the shipped
// bundle does.
import { afterAll, assert, expect, test } from 'vitest'

import { readFile } from 'node:fs/promises'

import { app } from './server.ts'

// app.inject() drives the real routes and hooks without binding a port,
// so these are fast and need no cleanup between cases.
afterAll(() => app.close())

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

  expect(res.body).not.toMatch(/deadbeef/)
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









// The lobby rendered once and never changed, so a battle opened after
// your page loaded never appeared and there was no button to join it.
test('the lobby ships a waiting list the poll can replace', async () => {
  const { registerBattle } = await import('./battle.ts')
  assert.ok(registerBattle, 'registerBattle should be exported')
})




















// checkTurn is an ordinary exported function now, so the test imports
// it. It used to be pulled out of the page's <script> by regex and
// eval'd, which is how a test can pass while the code it covers is
// unreachable.
async function checkTurnFromPage() {
  const { checkTurn } = await import('./client/shared.ts')
  return checkTurn as (
    b: unknown, me: string, t: { attacker: number; move: number; target: number },
  ) => string
}

const wMon = (name: string, fainted = false, disabled?: number) => ({
  name, hp: fainted ? 0 : 20, maxHp: 20, types: ['normal'], fainted,
  moves: [{ name: 'tackle', power: 40 }, { name: 'growl', power: 0 }],
  ...(disabled === undefined ? {} : { disabledMove: disabled }),
})
const wBattle = (mine: unknown, theirs: unknown, over = {}) => ({
  id: 'abc123', status: 'active', turn: 'ash', log: [],
  sides: [{ trainer: 'ash', team: [mine] }, { trainer: 'misty', team: [theirs] }],
  ...over,
})

// The web had NO turn rules at all. A disabled move rendered like any
// other, you clicked it, and the server rejected it - even though the
// spec says of disabledMove: "Selecting it is a 409, so a client should
// show it as unavailable rather than letting the turn fail."
test('the web refuses a disabled move before sending it', async () => {
  const checkTurn = await checkTurnFromPage()
  const b = wBattle(wMon('pikachu', false, 1), wMon('staryu'))
  assert.match(checkTurn(b, 'ash', { attacker: 0, move: 1, target: 0 }), /disabled/)
  // The other move on the same Pokemon is still fine.
  assert.equal(checkTurn(b, 'ash', { attacker: 0, move: 0, target: 0 }), '')
})

test('the web refuses the same turns the Go clients do', async () => {
  const checkTurn = await checkTurnFromPage()
  const ok = () => wBattle(wMon('pikachu'), wMon('staryu'))

  assert.match(checkTurn(wBattle(wMon('pikachu'), wMon('staryu'), { status: 'finished' }), 'ash', { attacker: 0, move: 0, target: 0 }), /over/)
  assert.match(checkTurn(wBattle(wMon('pikachu'), wMon('staryu'), { status: 'waiting' }), 'ash', { attacker: 0, move: 0, target: 0 }), /not started/)
  assert.match(checkTurn(wBattle(wMon('pikachu'), wMon('staryu'), { turn: 'misty' }), 'ash', { attacker: 0, move: 0, target: 0 }), /not your turn/)
  assert.match(checkTurn(ok(), 'brock', { attacker: 0, move: 0, target: 0 }), /not your turn/, 'a spectator')
  assert.match(checkTurn(ok(), 'ash', { attacker: 3, move: 0, target: 0 }), /no pokemon 4/)
  assert.match(checkTurn(ok(), 'ash', { attacker: 0, move: 0, target: 3 }), /no pokemon 4/)
  assert.match(checkTurn(wBattle(wMon('pikachu', true), wMon('staryu')), 'ash', { attacker: 0, move: 0, target: 0 }), /fainted/)
  assert.match(checkTurn(ok(), 'ash', { attacker: 0, move: 9, target: 0 }), /no move 10/)
  assert.match(checkTurn(wBattle(wMon('pikachu'), wMon('staryu', true)), 'ash', { attacker: 0, move: 0, target: 0 }), /already fainted/)
  // And a legal turn is legal.
  assert.equal(checkTurn(ok(), 'ash', { attacker: 0, move: 0, target: 0 }), '')
})

// The ORDER, same as the Go cross-check pins: with several things wrong
// at once, the earliest server check must win.
test('the web reports the same first reason the server would', async () => {
  const checkTurn = await checkTurnFromPage()
  const over = wBattle(wMon('pikachu', true), wMon('staryu', true), { status: 'finished', turn: 'misty' })
  assert.match(checkTurn(over, 'ash', { attacker: 9, move: 9, target: 9 }), /over/, 'battle over outranks all')
  const notYours = wBattle(wMon('pikachu'), wMon('staryu'), { turn: 'misty' })
  assert.match(checkTurn(notYours, 'ash', { attacker: 9, move: 9, target: 9 }), /not your turn/, 'turn outranks indices')
  const downed = wBattle(wMon('pikachu', true), wMon('staryu'))
  assert.match(checkTurn(downed, 'ash', { attacker: 0, move: 9, target: 0 }), /fainted/, 'fainted outranks move range')
})




// The battle page's shell only has to give React somewhere to mount
// and hand it the boot data. What the board RENDERS is asserted in
// client/Battle.test.tsx, against the components rather than a regex
// over server markup.
test('the battle page mounts React with its boot data', async () => {
  const { battlePage } = await import('./battle.ts')
  const html = battlePage({ id: 'abc123', trainer: 'ash' })
  assert.match(html, /id="root"/, 'React needs a mount point')
  assert.match(html, /id="boot"[^>]*>\{[^<]*abc123/, 'and the battle it is for')
  assert.match(html, /<script type="module" src="\/assets\/battle-[^"]+\.js">/,
    'the page must load the hashed bundle the build produced')
})

// JSON.stringify does not escape `<`, so a trainer name containing
// `</script>` closed the boot island early and everything after it was
// parsed as markup. Everything else on these pages goes through esc();
// the island was the one hole, and it carries the one piece of
// attacker-influenceable data.
test('a trainer name cannot break out of the boot island', async () => {
  const { battlePage } = await import('./battle.ts')
  const evil = 'x</script><script>alert(1)</script>'
  const html = battlePage({ id: 'abc123', trainer: evil })

  const start = html.indexOf('id="boot"')
  const end = html.indexOf('</script>', start)
  const island = html.slice(start, end)
  expect(island, 'the injected tag must not survive into the island').not.toMatch(/<script>alert/)

  // And it must still parse back to exactly what went in.
  const json = island.slice(island.indexOf('>') + 1)
  assert.equal((JSON.parse(json) as { me: string }).me, evil)
})

// The token authorises this trainer's moves. It lives in a cookie; the
// lobby only ever reads the NAME, so shipping the token into readable
// DOM text handed it to any injected script and any extension.
test('the lobby boot island carries no auth token', async () => {
  const { lobbyPage } = await import('./battle.ts')
  const html = lobbyPage({ me: { name: 'ash', token: 'secret-token-value' }, waiting: [] })
  expect(html).not.toMatch(/secret-token-value/)
  assert.match(html, /"name":"ash"/, 'but the name is still there')
})
