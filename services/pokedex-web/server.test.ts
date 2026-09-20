import { afterAll, assert, expect, test } from 'vitest'

import { readFile } from 'node:fs/promises'

import { app } from './server.ts'

afterAll(() => app.close())

test('healthz reports ok', async () => {
  const res = await app.inject({ method: 'GET', url: '/healthz' })
  assert.equal(res.statusCode, 200)
  assert.equal(res.body.trim(), 'ok')
})

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

test('an unrouted path is never used as a label', async () => {
  await app.inject({ method: 'GET', url: '/secret-token-value/deadbeef' })
  const res = await app.inject({ method: 'GET', url: '/metrics' })

  expect(res.body).not.toMatch(/deadbeef/)
  assert.match(res.body, /http_requests_total\{[^}]*route="other"/)
})

test('the lobby reports 502 when the API is unreachable', async () => {
  const res = await app.inject({ method: 'GET', url: '/battle' })
  assert.equal(res.statusCode, 502)
})

test('a battle page redirects to the lobby when nobody is registered', async () => {
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
  const cookie = 'trainer=' + encodeURIComponent(JSON.stringify({ name: 'x', token: 't' }))
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

test('the lobby ships a waiting list the poll can replace', async () => {
  const { registerBattle } = await import('./battle.ts')
  assert.ok(registerBattle, 'registerBattle should be exported')
})

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

test('the web reports the same first reason the server would', async () => {
  const checkTurn = await checkTurnFromPage()
  const over = wBattle(wMon('pikachu', true), wMon('staryu', true), { status: 'finished', turn: 'misty' })
  assert.match(checkTurn(over, 'ash', { attacker: 9, move: 9, target: 9 }), /over/, 'battle over outranks all')
  const notYours = wBattle(wMon('pikachu'), wMon('staryu'), { turn: 'misty' })
  assert.match(checkTurn(notYours, 'ash', { attacker: 9, move: 9, target: 9 }), /not your turn/, 'turn outranks indices')
  const downed = wBattle(wMon('pikachu', true), wMon('staryu'))
  assert.match(checkTurn(downed, 'ash', { attacker: 0, move: 9, target: 0 }), /fainted/, 'fainted outranks move range')
})

test('the battle page mounts React with its boot data', async () => {
  const { battlePage } = await import('./battle.ts')
  const html = battlePage({ id: 'abc123', trainer: 'ash' })
  assert.match(html, /id="root"/, 'React needs a mount point')
  assert.match(html, /id="boot"[^>]*>\{[^<]*abc123/, 'and the battle it is for')
  assert.match(html, /<script type="module" src="\/assets\/battle-[^"]+\.js">/,
    'the page must load the hashed bundle the build produced')
})

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

test('the lobby boot island carries no auth token', async () => {
  const { lobbyPage } = await import('./battle.ts')
  const html = lobbyPage({ me: { name: 'ash', token: 'secret-token-value' }, waiting: [] })
  expect(html).not.toMatch(/secret-token-value/)
  assert.match(html, /"name":"ash"/, 'but the name is still there')
})
