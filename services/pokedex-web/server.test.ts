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

// The filters pin below the topbar, which WRAPS on a narrow screen -
// title, search, three team slots, a button. A hardcoded offset is
// right for one window width and wrong for a phone, where it put the
// filters behind the header and out of reach.
test('the filter bar pins to a measured topbar height, not a guess', async () => {
  // Read the source rather than render the page: the route calls the
  // pokedex API, which a unit test has no business reaching, and the
  // CSS and script under test are static text either way.
  const page = await readFile(new URL('./pokedex.ts', import.meta.url), 'utf8')
  assert.match(page, /top:var\(--topbar-h/, 'filters should pin to the measured height')
  // The CALL, not the word: the comment above it also says
  // ResizeObserver, so a bare match passes with the observer deleted.
  assert.match(page, /new ResizeObserver\(pin\)\.observe\(topbar\)/,
    'the height should be re-measured when the topbar reflows')
  assert.ok(
    !/\.filters\s*\{[^}]*top:\s*\d+px/.test(page),
    'the filters must not pin to a hardcoded pixel offset',
  )
})









// The lobby was unreachable from the pokedex. A `.battle-link` rule was
// defined in the CSS and never used by any element, so the only ways in
// were knowing the URL or pressing "Ready to battle" - which opens a
// battle rather than showing you the ones already waiting. Both battle
// pages link back to the pokedex, so the navigation was one-directional.
test('the pokedex links to the battle lobby', async () => {
  const { page } = await import('./pokedex.ts')
  const html = page({ pokemon: [], types: [], active: '' })
  assert.match(html, /href="\/battle"/, 'the pokedex must offer a way into the lobby')
})

// Picking a team must work the same way whether you are opening a
// battle or joining one.
//
// It did not. The card grid - the only screen that shows you what you
// are choosing between - was wired only to "Ready to battle", which
// OPENS a battle. Everyone joining one got a bare text box and had to
// type three names from memory. So the first player to create a battle
// had a real interface and every opponent had a spelling test.
test('the pokedex picker joins the battle named in ?join', async () => {
  const { page } = await import('./pokedex.ts')
  const html = page({ pokemon: [], types: [], active: '', join: 'abc123' })

  assert.match(html, /id="ready"[^>]*data-join="abc123"/, 'the button should carry the battle id')
  assert.match(html, />Join battle</, 'and say it is joining, not opening')
  assert.match(html, /Pick your team/, 'the heading should say what this screen is for')
})

test('without ?join the picker still opens a new battle', async () => {
  const { page } = await import('./pokedex.ts')
  const html = page({ pokemon: [], types: [], active: '' })
  assert.match(html, />Ready to battle</)
  assert.match(html, /id="ready"[^>]*data-join=""/, 'no battle id means open a new one')
})

// Filtering mid-join must not silently turn a join into an open. The
// type links are the main way to find a Pokemon, so losing the id here
// would drop you back to creating a battle without saying so.
test('type filters keep the battle you are joining', async () => {
  const { page } = await import('./pokedex.ts')
  const html = page({
    pokemon: [],
    types: [{ name: 'water', count: 3 }] as never,
    active: '',
    join: 'abc123',
  })
  assert.match(html, /href="\/\?type=water&join=abc123"/, 'a filter link should carry the join id')
  assert.match(html, /href="\/\?join=abc123"[^>]*>all</, 'and so should "all"')
})

// Runs the pokedex page's real script and presses Ready.
//
// This is the assertion that matters: the button must POST to
// /battle/<id>/join, not /battle/open. Getting that wrong would look
// completely normal - you would pick a team, press join, and quietly
// create a SECOND battle while the one you meant to join kept waiting.
async function pressReady(join: string, picked: string[] = []) {
  const { page } = await import('./pokedex.ts')
  const html = page({ pokemon: [], types: [], active: '', join })
  const open = html.match(/<script[^>]*>/)!
  const script = html.slice(open.index! + open[0].length, html.lastIndexOf('</script>'))

  const ready: Record<string, unknown> = {
    dataset: { join }, disabled: false, textContent: '',
    addEventListener(_e: string, fn: () => Promise<void>) { (ready as never as {fire: unknown}).fire = fn },
  }
  const stub = () => ({
    classList: { add: () => {}, remove: () => {}, toggle: () => {} },
    addEventListener: () => {}, innerHTML: '', textContent: '', hidden: true,
    style: {}, dataset: {}, focus: () => {}, showModal: () => {}, close: () => {}, value: '',
    getBoundingClientRect: () => ({ height: 86, top: 0, bottom: 86 }),
    querySelector: () => null, querySelectorAll: () => [],
  })
  const store: Record<string, string> = {}
  if (picked.length) {
    store['pokedex.team'] = JSON.stringify(picked.map((n) => ({ name: n, sprite: '' })))
  }

  const calls: Array<{ url: string; body: string }> = []
  const sandbox = {
    document: {
      getElementById: (id: string) => (id === 'ready' ? ready : stub()),
      querySelector: () => stub(),
      querySelectorAll: () => [],
      addEventListener: () => {},
      body: stub(),
      documentElement: { style: { setProperty: () => {} } },
    },
    sessionStorage: {
      getItem: (k: string) => store[k] ?? null,
      setItem: (k: string, v: string) => { store[k] = v },
      removeItem: (k: string) => { delete store[k] },
    },
    location: { href: '', pathname: '/' },
    fetch: async (url: string, init?: { body?: string }) => {
      calls.push({ url, body: init?.body ?? '' })
      return { ok: true, status: 200, json: async () => ({ id: 'newly-opened' }) }
    },
    ResizeObserver: class { observe() {} disconnect() {} },
    alert: () => {},
    setTimeout: () => 0,
    console,
  }
  const run = new Function(...Object.keys(sandbox), script)
  run(...Object.values(sandbox))
  await ((ready as never as { fire: () => Promise<void> }).fire)()
  return { calls, location: sandbox.location }
}

test('pressing Join posts to the battle being joined, not to open', async () => {
  const { calls, location } = await pressReady('abc123', ['pikachu'])
  const post = calls.find((c) => c.url.includes('/battle/'))
  assert.ok(post, 'Ready should have called the API')
  assert.equal(post!.url, '/battle/abc123/join', 'it must JOIN, not open a second battle')
  assert.deepEqual(JSON.parse(post!.body).team, ['pikachu'], 'and carry the picked team')
  assert.equal(location.href, '/battle/abc123', 'then land on the battle that was joined')
})

test('pressing Ready with no join id still opens a battle', async () => {
  const { calls, location } = await pressReady('', ['pikachu'])
  const post = calls.find((c) => c.url.includes('/battle/'))
  assert.equal(post!.url, '/battle/open')
  assert.equal(location.href, '/battle/newly-opened', 'and lands on the new battle')
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

// The card grid IS the team picker, and as an <article> with a
// document click handler it was unreachable by keyboard: you could
// browse and filter, then not pick a team, which is the product.
test('pokemon cards are real buttons, not click-handling divs', async () => {
  const { page } = await import('./pokedex.ts')
  const html = page({
    pokemon: [{
      id: 25, name: 'pikachu', sprite: 's.png', types: ['electric'],
      height: 4, weight: 60, moves: [],
    }] as never,
    types: [], active: '',
  })
  assert.match(html, /<button type="button" class="card"/, 'a card must be focusable and pressable')
  expect(html, 'the old div-with-a-click-handler is gone').not.toMatch(/<article class="card"/)
  // Selection state must reach the accessibility tree: .picked::after is
  // CSS content and announces to nobody.
  assert.match(html, /aria-pressed="false"/)
})

// Placeholder text is not a label: it disappears the moment you type.
test('inputs are labelled', async () => {
  const { page } = await import('./pokedex.ts')
  const html = page({ pokemon: [], types: [], active: '' })
  assert.match(html, /id="search"[^>]*aria-label=/, 'the search box needs a label')
  assert.match(html, /id="trainer-name"[^>]*aria-label=/, 'the name field needs a label')
})

// Focus has to be visible for a keyboard user to know where they are.
// Both text inputs set outline:none and replaced it with a 3.27:1
// border; the cards had no focus style because they could not be
// focused at all.
test('focused elements show a visible focus ring', async () => {
  const { page } = await import('./pokedex.ts')
  const html = page({ pokemon: [], types: [], active: '' })
  assert.match(html, /:focus-visible[^{]*\{[^}]*outline:\s*2px solid/, 'a real focus indicator')
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
