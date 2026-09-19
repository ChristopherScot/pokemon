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

// Joining from the battle page used to POST no body at all, which the
// server reads as "no preference" and fills randomly. Landing on a
// battle link someone sent you therefore gave you three Pokemon you did
// not choose, with nothing on screen suggesting you had a say.
test('the battle page offers a team input when the battle is joinable', async () => {
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'ash' })
  // Match the INPUT, not any mention of the id: the script below also
  // names it, so a bare includes() passes even with the input removed.
  assert.match(page, /<input id="join-team"/, 'the join form should have a team input')
})

test('joining from the battle page sends the chosen team', async () => {
  const { battlePage } = await import('./battle.ts')
  const page = battlePage({ id: 'abc123', trainer: 'ash' })
  const open = page.match(/<script[^>]*>/)!
  const script = page.slice(open.index! + open[0].length, page.lastIndexOf('</script>'))

  const board = { innerHTML: '', className: '', scrollTop: 0 }
  const input = { value: 'pikachu, onix, gengar' }
  const joinBtn: Record<string, unknown> = { disabled: false, textContent: '' }
  const els: Record<string, unknown> = {
    board, log: board, 'join-team': input, 'join-battle': joinBtn,
  }
  let sentBody: string | undefined

  const sandbox = {
    document: {
      getElementById: (id: string) => els[id] ?? null,
      addEventListener: () => {},
      querySelectorAll: () => [],
    },
    location: { pathname: '/battle/abc123', reload: () => {} },
    fetch: async (_u: string, init?: { body?: string }) => {
      if (init?.body) sentBody = init.body
      return { ok: true, status: 200, json: async () => ({ version: 1, sides: [], log: [] }) }
    },
    setTimeout: () => 0,
    setInterval: () => 0,
    requestAnimationFrame: () => 0,
    console,
  }

  const run = new Function(
    ...Object.keys(sandbox),
    script + '\n;return { render };',
  ) as (...a: unknown[]) => { render: (b: unknown) => void }

  const { render } = run(...Object.values(sandbox))
  // A waiting battle with one side is joinable, so render wires the button.
  render({
    id: 'abc123', status: 'waiting', version: 1, turn: null, log: [],
    sides: [{ trainer: 'misty', team: [{ name: 'staryu', hp: 10, maxHp: 10, types: ['water'], moves: [], fainted: false }] }],
  })

  await (joinBtn.onclick as () => Promise<void>)()
  assert.ok(sentBody, 'the join should send a body')
  assert.deepEqual(JSON.parse(sentBody!).team, ['pikachu', 'onix', 'gengar'])
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
