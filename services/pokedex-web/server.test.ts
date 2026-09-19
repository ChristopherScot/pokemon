import { after, test } from 'node:test'
import assert from 'node:assert/strict'
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

// A battle that no longer exists must stop the polling loop.
//
// A 404 makes res.ok false, so the old loop skipped the body and called
// setTimeout anyway - forever, with no counter and no ceiling. Battles
// live in the server's memory, so a deploy ends every one of them, and
// a tab left open on a finished battle polled once a second for five
// hours. Loki recorded ~1,760 requests per 30 minutes, unbroken, and
// that traffic is what pushed this service past its memory limit and
// got it OOMKilled.
//
// Runs the real browser script so the counting is what actually ships.
test('polling stops once the battle is gone', async () => {
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'someone' })

  const open = page.match(/<script[^>]*>/)
  assert.ok(open, 'the battle page should ship a script')
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  const board = { innerHTML: 'loading…', className: '', scrollTop: 0 }
  let fetches = 0
  const pending: Array<() => void> = []

  const sandbox = {
    document: {
      getElementById: (id: string) => (id === 'board' || id === 'log' ? board : null),
      addEventListener: () => {},
    },
    location: { pathname: '/battle/abc123', reload: () => {} },
    // Always gone.
    fetch: async () => { fetches++; return { ok: false, status: 404, json: async () => ({}) } },
    // Captured rather than timed, so the test drives the loop.
    setTimeout: (fn: () => void) => { pending.push(fn); return 0 },
    setInterval: () => 0,
    requestAnimationFrame: () => 0,
    console,
  }

  const run = new Function(...Object.keys(sandbox), script + '\n;return {};') as
    (...a: unknown[]) => unknown
  run(...Object.values(sandbox))

  // Drive far more turns than the miss ceiling; it must stop on its own.
  //
  // A tick is yielded before each one because poll() is async: the
  // setTimeout it schedules is queued a microtask after its fetch
  // resolves, so draining immediately finds the queue still empty and
  // the loop looks like it stopped when it has not started.
  for (let i = 0; i < 50; i++) {
    await new Promise((r) => setImmediate(r))
    const next = pending.shift()
    if (!next) break
    next()
  }
  await new Promise((r) => setImmediate(r))

  assert.ok(fetches < 20,
    `polled ${fetches} times against a battle that is gone; the loop never stops`)
  assert.match(board.innerHTML, /this battle is over/,
    'the player is left on a spinner with no explanation')
})

// Drives the REAL poll() out of the page, so these assert on the code a
// browser runs rather than on a copy of it.
async function pollHarness(responses: Array<{ ok: boolean; status: number }>) {
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'ash' })
  const open = page.match(/<script[^>]*>/)!
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  const board = { innerHTML: 'loading…', className: '', scrollTop: 0 }
  const els: Record<string, unknown> = { board, log: board }
  const sent: Array<Record<string, string>> = []
  let reschedules = 0
  let i = 0

  const sandbox = {
    document: {
      getElementById: (id: string) => els[id] ?? null,
      addEventListener: () => {},
    },
    location: { pathname: '/battle/abc123', reload: () => {} },
    fetch: async (_url: string, init?: { headers?: Record<string, string> }) => {
      sent.push(init?.headers ?? {})
      const r = responses[Math.min(i++, responses.length - 1)]
      return { ...r, json: async () => ({ version: 1, sides: [], log: [] }) }
    },
    setTimeout: (fn: () => void) => { reschedules++; return 0 },
    setInterval: () => 0,
    requestAnimationFrame: () => 0,
    console,
  }

  const run = new Function(
    ...Object.keys(sandbox),
    script + '\n;return { poll };',
  ) as (...a: unknown[]) => { poll: () => Promise<void> }

  const { poll } = run(...Object.values(sandbox))
  // The script calls poll() itself on load, so let that settle and
  // count only what the explicit call below does.
  await new Promise((r) => setImmediate(r))
  sent.length = 0
  reschedules = 0
  i = 0

  await poll()
  return { sent, reschedules, board }
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

// Joining used to give you no say in your team at all: the request
// carried no body, so the server filled it randomly and nothing on
// screen suggested otherwise. The first fix put a text box here, which
// was better but still meant typing three names from memory - the card
// grid that shows you what you are choosing between was wired only to
// "open a battle", never to joining one.
//
// So the requirement these pin is: a joinable battle must offer a route
// to the real picker, and it must carry the battle id so the picker
// knows what it is joining.
test('a joinable battle links to the team picker', async () => {
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'ash' })
  const open = page.match(/<script[^>]*>/)!
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  // The join panel is built by render(), not sent in the HTML, so the
  // page string never contains it. Run the real thing.
  const board = { innerHTML: '', className: '', scrollTop: 0 }
  const stub = {
    innerHTML: '', textContent: '', className: '', scrollTop: 0, scrollHeight: 0,
    addEventListener: () => {}, querySelectorAll: () => [],
    classList: { add: () => {}, remove: () => {} },
  }
  const sandbox = {
    document: {
      getElementById: (id: string) => (id === 'board' ? board : stub),
      addEventListener: () => {},
      querySelectorAll: () => [],
    },
    location: { pathname: '/battle/abc123', reload: () => {} },
    fetch: async () => ({ ok: true, status: 200, json: async () => ({ version: 1, sides: [], log: [] }) }),
    setTimeout: () => 0,
    setInterval: () => 0,
    requestAnimationFrame: () => 0,
    console,
  }
  const run = new Function(...Object.keys(sandbox), script + '\n;return { render };')
  const { render } = run(...Object.values(sandbox)) as { render: (b: unknown) => void }

  // One side and still waiting: joinable.
  render({
    id: 'abc123', status: 'waiting', version: 1, turn: null, log: [],
    sides: [{ trainer: 'misty', team: [{ name: 'staryu', hp: 10, maxHp: 10, types: ['water'], moves: [], fainted: false }] }],
  })

  assert.match(
    board.innerHTML,
    /href="\/\?join=abc123"/,
    'the join panel should link to the pokedex picker for THIS battle',
  )
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

// Runs the battle page's real render() and returns the float elements
// it appended for a newly-arrived log entry.
//
// Asserting on effectBand alone would have missed the bug this covers:
// the float loop skipped events with `!e.damage`, and an immune hit
// deals exactly 0, so the "no effect" branch was unreachable. The
// classifier was right and the screen still showed nothing.
async function floatsForEvent(ev: Record<string, unknown>) {
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'ash' })
  const open = page.match(/<script[^>]*>/)!
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  const appended: Array<{ className: string; textContent: string }> = []
  const slot = {
    appendChild: (c: { className: string; textContent: string }) => appended.push(c),
    classList: { add: () => {}, remove: () => {} },
    querySelector: () => null,
  }
  const board = {
    innerHTML: '',
    querySelector: () => slot,
    querySelectorAll: () => [],
  }
  const mkEl = () => ({ className: '', textContent: '', style: {}, appendChild: () => {}, remove: () => {} })

  const mon = (name: string) => ({ name, types: ['normal'], hp: 20, maxHp: 20, moves: [] })
  const battle = (log: unknown[]) => ({
    version: log.length + 1,
    status: 'active',
    turn: 'ash',
    sides: [
      { trainer: 'ash', active: 0, team: [mon('pikachu')] },
      { trainer: 'misty', active: 0, team: [mon('gastly')] },
    ],
    log,
  })

  let frame: (() => void) | null = null
  const sandbox = {
    document: {
      // render() also writes to #log and #banner; a null here surfaces
      // as "Cannot set properties of null" rather than a useful failure.
      getElementById: (id: string) =>
        id === 'board' ? board : {
          innerHTML: '', textContent: '', className: '',
          scrollTop: 0, scrollHeight: 0,
          addEventListener: () => {}, querySelectorAll: () => [],
          classList: { add: () => {}, remove: () => {} },
        },
      addEventListener: () => {},
      createElement: mkEl,
      hidden: false,
    },
    location: { pathname: '/battle/abc123', href: '', reload: () => {} },
    fetch: async () => ({ ok: true, status: 200, json: async () => battle([]) }),
    setTimeout: () => 0,
    setInterval: () => 0,
    requestAnimationFrame: (fn: () => void) => { frame = fn; return 0 },
    alert: () => {},
    console,
  }

  const run = new Function(...Object.keys(sandbox), script + '\n;return { render };')
  const { render } = run(...Object.values(sandbox)) as { render: (b: unknown) => void }

  // First render sets the log baseline; the second delivers the new
  // event, which is the only one that animates.
  render(battle([]))
  appended.length = 0
  render(battle([ev]))
  if (frame) (frame as () => void)()
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
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'ash' })
  const open = page.match(/<script[^>]*>/)!
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  const board = {
    innerHTML: '', className: '', scrollTop: 0,
    querySelectorAll: () => [], querySelector: () => null,
  }
  const stub = {
    innerHTML: '', textContent: '', className: '', scrollTop: 0, scrollHeight: 0,
    addEventListener: () => {}, querySelectorAll: () => [], querySelector: () => null,
    classList: { add: () => {}, remove: () => {} },
  }
  const sandbox = {
    document: {
      getElementById: (id: string) => (id === 'board' ? board : stub),
      addEventListener: () => {},
      querySelectorAll: () => [],
    },
    location: { pathname: '/battle/abc123', reload: () => {} },
    fetch: async () => ({ ok: true, status: 200, json: async () => ({ version: 1, sides: [], log: [] }) }),
    setTimeout: () => 0,
    setInterval: () => 0,
    requestAnimationFrame: () => 0,
    console,
  }
  const run = new Function(...Object.keys(sandbox), script + '\n;return { render };')
  const { render } = run(...Object.values(sandbox)) as { render: (b: unknown) => void }

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
  })

  // Its own container, not a second row of .moves - that separation is
  // what stops it reading as another choice.
  assert.match(board.innerHTML, /<div class="commit">/, 'attack should sit in its own commit row')
  assert.match(
    board.innerHTML,
    /<div class="commit">\s*<button id="go"/,
    'and the attack button should be the thing inside it',
  )
  // The style backs it up: red, and pushed to the right.
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
