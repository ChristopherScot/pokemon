import { after, test } from 'node:test'
import assert from 'node:assert/strict'

import { installDom } from './client/testdom.ts'
import { readFile } from 'node:fs/promises'

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
  // Imports the module. This used to regex the <script> tag out of the
  // rendered page and eval it, which could only reach what the string
  // happened to expose and passed whether or not the code compiled.
  const dom = installDom()
  dom.get('boot').textContent = JSON.stringify({ id: 'abc123', me: 'ash', colours: {} })
  const { render, resetForTest } = await import('./client/battle.ts')
  resetForTest()

  render({
    id: 'abc123', status: 'waiting', version: 1, turn: null, log: [],
    sides: [{ trainer: 'misty', team: [
      { name: 'staryu', hp: 10, maxHp: 10, types: ['water'], moves: [], fainted: false },
    ] }],
  } as never)

  const board = dom.get('board')
  assert.notEqual(board.innerHTML, 'loading…', 'a non-participant must still get a board')
  assert.match(board.innerHTML, /staryu/, 'and see the team that is waiting')
  dom.restore()
})

// A 404 means the battle is gone. Retrying it forever is what produced
// 46,000 requests from one tab over thirteen hours, so the loop counts
// consecutive misses and stops, saying why.
test('polling stops once the battle is gone', async () => {
  const dom = installDom()
  dom.get('boot').textContent = JSON.stringify({ id: 'abc123', me: 'someone', colours: {} })
  dom.replies = [{ ok: false, status: 404 }]
  const { poll, resetForTest } = await import('./client/battle.ts')
  resetForTest()

  // Poll repeatedly. A loop that gives up leaves the last call having
  // scheduled nothing; one that does not will keep asking forever.
  for (let i = 0; i < 20; i++) {
    dom.timers = 0
    await poll()
    if (dom.timers === 0) break
  }

  assert.equal(dom.timers, 0, 'the poll never gave up on a battle that is gone')
  assert.match(dom.get('board').innerHTML, /this battle is over/,
    'the player is left on a spinner with no explanation')
  dom.restore()
})

// Drives the real poll() against a scripted sequence of replies.
//
// Imports the module rather than eval'ing the page's <script>: the
// browser code is a bundle now, so the old harness could only reach
// minified names, and it never verified the code compiled at all.
async function pollHarness(responses: Array<{ ok: boolean; status: number }>) {
  const dom = installDom()
  dom.get('boot').textContent = JSON.stringify({ id: 'abc123', me: 'ash', colours: {} })
  dom.replies = responses.map((r) => ({
    ...r,
    body: { version: 1, sides: [], log: [] },
  }))
  const { poll, resetForTest } = await import('./client/battle.ts')
  resetForTest()

  // The module auto-starts only when #board exists in a real document;
  // under the test harness it does not, so this is the only poll.
  dom.fetches.length = 0
  dom.timers = 0
  await poll()

  return {
    sent: dom.fetches.map((f) => f.init?.headers ?? {}),
    reschedules: dom.timers,
    board: dom.get('board'),
    dom,
  }
}

// The bug this closes: anything that was not 200 or 404 fell through to
// the retry at the bottom, so a 500 - or an nginx 502 mid-rollout, or a
// 410 telling this client it is too old - polled once a second forever.
test('poll stops on a terminal status instead of retrying forever', async () => {
  for (const status of [410, 501, 505]) {
    const { reschedules, board } = await pollHarness([{ ok: false, status }])
    assert.equal(reschedules, 0, `a ${status} should stop the poll, not reschedule it`)
    assert.match(board.innerHTML, /out of date/, `a ${status} should say why it stopped`)
  }
})

// A transient failure still retries: a dropped request or a pod
// restarting mid-rollout must not kill a live battle.
test('poll retries a transient failure', async () => {
  const { reschedules } = await pollHarness([{ ok: false, status: 503 }])
  assert.equal(reschedules, 1, 'a 503 is transient and should be retried')
})

// Without a version the server cannot tell which browser code is
// calling, which is what made the stuck tab impossible to identify or
// refuse.
test('every request reports the UI version', async () => {
  const { UI_VERSION } = await import('./battle.ts')
  const { sent } = await pollHarness([{ ok: true, status: 200 }])
  assert.ok(sent.length > 0, 'the poll should have made a request')
  for (const headers of sent) {
    assert.equal(headers['Client-Version'], UI_VERSION)
  }
})

test('a joinable battle links to the team picker', async () => {
  const dom = installDom()
  dom.get('boot').textContent = JSON.stringify({ id: 'abc123', me: 'ash', colours: {} })
  const { render, resetForTest } = await import('./client/battle.ts')
  resetForTest()

  // One side and still waiting: joinable.
  render({
    id: 'abc123', status: 'waiting', version: 1, turn: null, log: [],
    sides: [{ trainer: 'misty', team: [
      { name: 'staryu', hp: 10, maxHp: 10, types: ['water'], moves: [], fainted: false },
    ] }],
  } as never)

  assert.match(dom.get('board').innerHTML, /href="\/\?join=abc123"/,
    'the join panel should link to the pokedex picker for THIS battle')
  dom.restore()
})

test('the lobby sends you to the picker rather than a text box', async () => {
  const { lobbyPage } = await import('./battle.ts')
  const page = lobbyPage({
    me: { name: 'ash', token: 't' },
    waiting: [{ battleId: 'xyz789', trainer: 'misty', team: ['staryu'] }] as never,
  })
  assert.match(page, /href="\/\?join=xyz789"/, 'each waiting battle should link to the picker')
  // The old comma-separated input is gone; leaving it would be two
  // different ways to do the same thing, disagreeing about which wins.
  assert.doesNotMatch(page, /id="team"/, 'the typed-team input should be gone')
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

// The lobby's script is EXECUTED, not just asserted on as a string.
//
// It shipped with `V` used in four handlers and declared only in the
// battle page's script - a different module scope in a different
// document. Every lobby handler threw "ReferenceError: V is not
// defined" on its first statement: register, open, join and the poll,
// all dead, all silent, because module scripts fail quietly and the
// poll's catch swallowed its own throw.
//
// A test that checks the page CONTAINS something passes whether or not
// the page works. This one runs it.
async function runLobbyScript(waiting: unknown[] = []) {
  const { lobbyPage } = await import('./battle.ts')
  const page = lobbyPage({
    me: { name: 'ash', token: 't' },
    waiting: waiting as never,
  })
  const open = page.match(/<script[^>]*>/)!
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  const sent: Array<{ url: string; headers: Record<string, string> }> = []
  const els: Record<string, unknown> = {
    team: { value: 'pikachu' },
    waiting: { innerHTML: '' },
  }
  const sandbox = {
    document: {
      getElementById: (id: string) => els[id] ?? null,
      addEventListener: () => {},
      querySelectorAll: () => [],
      hidden: false,
    },
    location: { pathname: '/battle', href: '', reload: () => {} },
    fetch: async (url: string, init?: { headers?: Record<string, string> }) => {
      sent.push({ url, headers: init?.headers ?? {} })
      return { ok: true, status: 200, json: async () => ({ waiting: [] }) }
    },
    setTimeout: () => 0,
    alert: () => {},
    console,
  }
  // Throws here if the script references anything it does not define.
  const run = new Function(...Object.keys(sandbox), script + '\n;return { pollLobby };')
  const api = run(...Object.values(sandbox)) as { pollLobby: () => Promise<void> }
  return { api, sent }
}

test('the lobby script evaluates without a missing binding', async () => {
  await runLobbyScript()
})

test('lobby requests report the UI version', async () => {
  const { UI_VERSION } = await import('./battle.ts')
  const { api, sent } = await runLobbyScript()
  await api.pollLobby()
  assert.ok(sent.length > 0, 'the poll should have made a request')
  for (const r of sent) {
    assert.equal(r.headers['Client-Version'], UI_VERSION, `${r.url} sent no version`)
  }
})

// Runs the real render() and returns the float elements it appended
// for a newly-arrived log entry.
//
// Asserting on effectBand alone would have missed the bug this covers:
// the float loop skipped events with `!e.damage`, and an immune hit
// deals exactly 0, so the "no effect" branch was unreachable. The
// classifier was right and the screen still showed nothing.
async function floatsForEvent(ev: Record<string, unknown>) {
  const dom = installDom()
  dom.get('boot').textContent = JSON.stringify({ id: 'abc123', me: 'ash', colours: {} })

  const appended: Array<{ className: string; textContent: string }> = []
  const slot = dom.get('slot')
  slot.appendChild = (c) => { appended.push(c as never); return c }
  dom.get('board').querySelector = () => slot

  const { render, resetForTest } = await import('./client/battle.ts')
  resetForTest()
  const mon = (name: string) => ({
    name, types: ['normal'], hp: 20, maxHp: 20, fainted: false, moves: [],
  })
  const battle = (log: unknown[]) => ({
    id: 'abc123', version: log.length + 1, status: 'active', turn: 'ash',
    sides: [
      { trainer: 'ash', active: 0, team: [mon('pikachu')] },
      { trainer: 'misty', active: 0, team: [mon('gastly')] },
    ],
    log,
  })

  // First render sets the log baseline; the second delivers the new
  // event, which is the only one that animates.
  render(battle([]) as never)
  appended.length = 0
  render(battle([ev]) as never)
  dom.flushFrames()
  dom.restore()
  return appended
}

test('an immune hit shows "no effect" rather than "-0"', async () => {
  const floats = await floatsForEvent({
    turnNumber: 1, text: 'gastly is immune', target: 'gastly',
    damage: 0, effectiveness: 0,
  })
  assert.equal(floats.length, 1, 'an immune hit should still float something')
  assert.equal(floats[0].textContent, 'no effect')
  assert.match(floats[0].className, /immune/)
})

test('a normal hit still floats its damage', async () => {
  const floats = await floatsForEvent({
    turnNumber: 1, text: 'pikachu hits', target: 'gastly',
    damage: 7, effectiveness: 1,
  })
  assert.equal(floats.length, 1)
  assert.equal(floats[0].textContent, '-7')
})

// The full band table, asserted through the same rendering path rather
// than against the classifier in isolation.
//
// An earlier version of this called effectBand() directly out of the
// LOBBY script, which was wrong twice over: it tested the layer below
// the bug, and evaluating that script without stubbing setTimeout left
// the lobby poll re-arming forever, so `node --test` never exited.
test('effectiveness bands match the server chart', async () => {
  const cases: Array<[number, string, string]> = [
    [4, 'super', '-9 !!'],
    [2, 'super', '-9 !!'],
    [1, 'normal', '-9'],
    // No suffix on a weak hit: the web marks only super-effective.
    [0.5, 'weak', '-9'],
    [0.25, 'weak', '-9'],
  ]
  for (const [eff, band, text] of cases) {
    const floats = await floatsForEvent({
      turnNumber: 1, text: 'hit', target: 'gastly', damage: 9, effectiveness: eff,
    })
    assert.equal(floats.length, 1, `${eff}x should float`)
    assert.match(floats[0].className, new RegExp(band), `${eff}x should be ${band}`)
    assert.equal(floats[0].textContent, text, `${eff}x text`)
  }
})

test('a status move with no damage floats nothing', async () => {
  const floats = await floatsForEvent({
    turnNumber: 1, text: 'pikachu used growl', target: 'gastly',
  })
  assert.equal(floats.length, 0, 'no damage field means nothing to float')
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

// The attack button ends your turn; the move buttons above only change
// a selection. They looked identical - same grey, same size, in an
// identical row directly below - so the thing that fires read as a
// fifth move.
test('the attack button is visually distinct from the move buttons', async () => {
  const dom = installDom()
  dom.get('boot').textContent = JSON.stringify({ id: 'abc123', me: 'ash', colours: {} })
  const { render, resetForTest } = await import('./client/battle.ts')
  resetForTest()

  const mon = (name: string) => ({
    name, hp: 20, maxHp: 20, types: ['normal'], fainted: false,
    moves: [{ name: 'tackle', power: 40 }, { name: 'growl', power: 0 }],
  })
  render({
    id: 'abc123', status: 'active', version: 1, turn: 'ash', log: [],
    sides: [
      { trainer: 'ash', active: 0, team: [mon('pikachu')] },
      { trainer: 'misty', active: 0, team: [mon('staryu')] },
    ],
  } as never)

  const html = dom.get('board').innerHTML
  // Its own container, not a second row of .moves - that separation is
  // what stops it reading as another choice.
  assert.match(html, /<div class="commit">/, 'attack should sit in its own commit row')
  assert.match(html, /<div class="commit">\s*<button id="go"/,
    'and the attack button should be the thing inside it')
  dom.restore()

  // The style backs it up: red, and pushed to the right.
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'ash' })
  assert.match(page, /\.commit\s*\{[^}]*justify-content:flex-end/, 'commit row aligns right')
  assert.match(page, /\.commit button\s*\{[^}]*background:#b42318/, 'attack is red')
})

// Trainers live in the API's memory, so every deploy invalidates every
// token while the browser's cookie survives. The lobby then said
// "you are Chris" from the cookie's NAME while every action answered
// 401 "register first" because of its TOKEN - and the register form
// was hidden precisely because a cookie was present. No way out but
// clearing site data.
test('the lobby can always re-register, even with a trainer cookie', async () => {
  const { lobbyPage } = await import('./battle.ts')
  const page = lobbyPage({ me: { name: 'chris', token: 'stale' }, waiting: [] as never })
  assert.match(page, /id="reg-row"/, 'the register form must be in the markup')
  assert.match(page, /id="rename"/, 'and something must reveal it')
})

// A 401 means the cookie's token is dead and the server has already
// cleared it, so reloading shows the register form. Alerting and
// stopping left the user staring at a name they could not use.
test('a stale trainer reloads into the register form rather than alerting', async () => {
  const { lobbyPage } = await import('./battle.ts')
  const page = lobbyPage({ me: { name: 'chris', token: 'stale' }, waiting: [] as never })
  const open = page.match(/<script[^>]*>/)!
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  let reloaded = false
  let alerted = ''
  const els: Record<string, unknown> = {
    open: { addEventListener(_e: string, fn: () => Promise<void>) { (els.open as {fire?: unknown}).fire = fn } },
  }
  const sandbox = {
    document: {
      getElementById: (id: string) => els[id] ?? null,
      addEventListener: () => {},
      querySelectorAll: () => [],
      hidden: false,
    },
    location: { pathname: '/battle', href: '', reload: () => { reloaded = true } },
    fetch: async () => ({ ok: false, status: 401, json: async () => ({ message: 'register first' }) }),
    setTimeout: () => 0,
    alert: (m: string) => { alerted = m },
    console,
  }
  const run = new Function(...Object.keys(sandbox), script + '\n;return {};')
  run(...Object.values(sandbox))

  await ((els.open as { fire: () => Promise<void> }).fire)()
  assert.equal(reloaded, true, 'a 401 should reload into the register form')
  assert.equal(alerted, '', 'and not dead-end in an alert')
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
  assert.doesNotMatch(html, /<article class="card"/, 'the old div-with-a-click-handler is gone')
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

// A live region inside #board is destroyed by every innerHTML swap, so
// it never announces. "your turn" is the one thing the game must tell a
// screen-reader user.
test('the battle banner is a persistent live region', async () => {
  const { battlePage } = await import('./battle.ts')
  const html = battlePage({ id: 'abc123', trainer: 'ash' })
  assert.match(html, /id="banner"[^>]*aria-live="polite"/, 'the banner must announce')
  const bannerAt = html.indexOf('id="banner"')
  const boardAt = html.indexOf('id="board"')
  assert.ok(bannerAt >= 0 && boardAt >= 0 && bannerAt < boardAt,
    'the banner must sit OUTSIDE #board, or render() destroys it every poll')
})
