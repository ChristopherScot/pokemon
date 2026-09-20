// Battle mode for the web UI.
//
// The page is server-rendered like the rest of this service, but a
// battle changes while you are looking at it - so this one page carries
// a small inline script that polls and re-renders. No framework and no
// build step: the container stays `node server.js`.
//
// The API decides everything. This file renders state and posts intents,
// exactly as the CLI and TUI do.

import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify'
import createClient, { exponentialRetry, noRetry } from '@christopherscot/pokedex-client'

// The wire types, from the generated client's schema. Importing them
// rather than restating them is the point of generating a client: a
// field that changes in openapi.yml breaks this file at typecheck
// rather than at runtime.
import type { components } from '@christopherscot/pokedex-client'

type Battle = components['schemas']['Battle']
type WaitingBattle = components['schemas']['WaitingBattle']
type BattleEvent = components['schemas']['BattleEvent']
type BattlePokemon = components['schemas']['BattlePokemon']

// The trainer identity kept in a cookie: a public name and the token
// that authorises this trainer's moves.
type Trainer = { name: string; token: string }

// What each route reads off a request. Fastify takes these as generics
// so request.body and request.params are typed rather than unknown -
// which is what stops a route reading a field the client never sends.
type IdParam = { id: string }
type TeamBody = { team?: string[] }
type NameBody = { name?: string }
type TurnBody = { attacker: number; move: number; target: number }

const baseUrl = process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local'
// exponentialRetry spends about 3.4s across five attempts before giving
// up, which is right in front of a rolling deployment and wrong in a
// test suite with no API behind it - four routes times five attempts is
// a minute of waiting to assert a 502. POKEDEX_NO_RETRY turns it off.
const api = createClient({
  baseUrl,
  policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
})

// Type colours, shared with the card grid so a badge means the same
// thing on both pages.
export const TYPE_COLOURS = {
  normal: '#9fa19f', fire: '#e62829', water: '#2980ef', electric: '#fac000',
  grass: '#3fa129', ice: '#3dcef3', fighting: '#ff8000', poison: '#9141cb',
  ground: '#915121', flying: '#81b9ef', psychic: '#ef4179', bug: '#91a119',
  rock: '#afa981', ghost: '#704170', dragon: '#5060e1', dark: '#50413f',
  steel: '#60a1b8', fairy: '#ef70ef',
}

const esc = (s: unknown): string =>
  String(s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] ?? c)

import { assetURL } from './assets.ts'
import { UI_VERSION } from './version.ts'
export { UI_VERSION }

// Shared by BOTH inline scripts.
//
// battlePage and lobbyPage each emit their own <script type="module">,
// which are separate module scopes in separate documents. A helper
// defined in one is simply absent in the other - and because these are
// strings, TypeScript cannot see the difference. Client-Version headers
// were added to the lobby's fetches while `V` was only ever declared in
// the battle page's script, so every lobby button threw
// "ReferenceError: V is not defined" and did nothing, silently, inside
// an async handler.
//
// Anything both pages need goes here, once.
const PRELUDE = `
const UI_VERSION = ${JSON.stringify(UI_VERSION)}

// Sent on every request this page makes, so the server knows which
// browser code is calling it.
const V = { 'Client-Version': UI_VERSION }

// Statuses that mean "stop asking" rather than "try again". 410 is the
// server refusing this client as too old; retrying cannot help, because
// the code in this tab will not change on its own.
const TERMINAL = new Set([410, 501, 505])

const esc = (t) => String(t).replace(/[&<>"']/g, (c) => (
  { '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[c]))

// The one place the web decides what a multiplier MEANS. The server
// sends a raw number; every display that wants a word or a colour asks
// here rather than re-deriving thresholds.
//
// Zero is immune, not weak. That distinction was wrong in one of the
// two places this replaces: the battle log excluded 0 with an explicit
// > 0 guard, the floating damage number did not, so the same immune hit
// was styled two different ways on the same screen.
// Why a turn cannot be played, or "" if it can.
//
// The order mirrors the server's takeTurn and the Go clients'
// battleclient.CheckTurn, deliberately: a client that reports a
// different FIRST reason than the authority teaches a rule that is not
// the rule. "that pokemon has fainted" where the server would say "not
// your turn" is worse than saying nothing, because it is confidently
// wrong.
//
// This does not replace the server's check. The board is up to a poll
// behind and the opponent moves too, so a locally-legal turn can still
// be rejected - attack() still shows that. What this removes is the
// turn that was knowably illegal before it was sent.
const checkTurn = (b, me, t) => {
  if (b.status === 'finished') return 'this battle is over'
  if (b.status !== 'active') return 'this battle has not started'

  const mineIdx = b.sides.findIndex((s) => s.trainer === me)
  if (mineIdx === -1 || b.turn !== me) return 'not your turn'
  const mine = b.sides[mineIdx], theirs = b.sides[1 - mineIdx]

  if (!mine.team[t.attacker]) return 'you have no pokemon ' + (t.attacker + 1)
  if (!theirs.team[t.target]) return 'they have no pokemon ' + (t.target + 1)

  const attacker = mine.team[t.attacker]
  if (attacker.fainted) return attacker.name + ' has fainted'
  if (!attacker.moves[t.move]) return attacker.name + ' has no move ' + (t.move + 1)
  if (theirs.team[t.target].fainted) return theirs.team[t.target].name + ' has already fainted'
  // The spec says of disabledMove: "Selecting it is a 409, so a client
  // should show it as unavailable rather than letting the turn fail."
  // This page was not reading the field at all.
  if (attacker.disabledMove === t.move) return attacker.moves[t.move].name + ' is disabled this turn'
  return ''
}

const effectBand = (e) => {
  if (e === undefined || e === null) return 'normal'
  if (e === 0) return 'immune'
  if (e >= 2) return 'super'
  if (e < 1) return 'weak'
  return 'normal'
}
`

// The battle page. Everything below the initial render is done by the
// inline script, which polls /battle/:id/state and swaps the board.
// Exported so a test can run the real browser code rather than assert
// on the page's text. The bug this guards against - a non-participant
// indexing sides[-1] - is only reachable by executing render().
export function battlePage({ id, trainer }: { id: string; trainer: string }) {
  return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Battle · Pokedex</title>
<style>
  :root { color-scheme: dark; --bg:#12141c; --card:#1c1f2b; --fg:#e8eaf2; --dim:#8b91a7;
          --good:#4ade80; --warn:#fbbf24; --danger:#f87171; }
  * { box-sizing: border-box; }
  body { margin:0; padding:24px; background:var(--bg); color:var(--fg);
         font:15px/1.5 ui-sans-serif,system-ui,-apple-system,sans-serif; }
  a { color:inherit; }
  h1 { margin:0 0 4px; font-size:22px; }
  .sub { color:var(--dim); margin:0 0 20px; font-size:13px; }
  .side { margin-bottom:22px; }
  .side h2 { font-size:13px; text-transform:uppercase; letter-spacing:.08em;
             color:var(--dim); margin:0 0 8px; font-weight:600; }
  .mon { display:grid; grid-template-columns:52px 1fr 92px; gap:12px; align-items:center;
         background:var(--card); border-radius:10px; padding:10px 12px; margin-bottom:8px;
         position:relative; transition:opacity .4s ease, filter .4s ease; }
  /* A fainted Pokemon fades rather than vanishing, so you can see what
     is gone without the layout jumping. */
  .mon.fainted { opacity:.35; filter:grayscale(1); }
  .mon img { width:52px; height:52px; image-rendering:pixelated; }
  .name { font-weight:600; text-transform:capitalize; }
  .types { display:flex; gap:4px; margin-top:3px; }
  .type { font-size:10px; text-transform:uppercase; letter-spacing:.04em;
          padding:1px 6px; border-radius:999px; color:#12141c; font-weight:700; }
  .hpwrap { height:8px; background:#2a2e3d; border-radius:999px; overflow:hidden; margin-top:6px; }
  /* The drain: width transitions, so a hit visibly drops the bar
     instead of teleporting it. */
  .hp { height:100%; border-radius:999px; transition:width .55s cubic-bezier(.22,.61,.36,1), background-color .3s; }
  .hpnum { text-align:right; font-variant-numeric:tabular-nums; color:var(--dim); font-size:13px; }
  /* Damage numbers rise off the Pokemon they hit and fade out. */
  @keyframes floatUp {
    0%   { opacity:0; transform:translateY(6px) scale(.9); }
    15%  { opacity:1; transform:translateY(0) scale(1.08); }
    100% { opacity:0; transform:translateY(-26px) scale(1); }
  }
  .float { position:absolute; right:104px; top:8px; font-weight:800; font-size:22px;
           pointer-events:none; animation:floatUp 2.4s ease-out forwards;
           text-shadow:0 2px 8px rgba(0,0,0,.6); }
  .float.super { color:var(--warn); text-shadow:0 0 12px rgba(251,191,36,.6); }
  .float.weak  { color:var(--dim); font-size:14px; }
  /* Immune reads as text, not a number, so it is sized like a word. */
  .float.immune{ color:var(--dim); font-size:13px; font-style:italic; }
  .float.normal{ color:var(--danger); }
  /* A super-effective hit shakes the card it landed on. */
  @keyframes shake {
    0%,100% { transform:translateX(0); }
    20% { transform:translateX(-5px); } 40% { transform:translateX(5px); }
    60% { transform:translateX(-3px); } 80% { transform:translateX(3px); }
  }
  .mon.hit { animation:shake .4s ease-in-out; }
  @keyframes shakeHard {
    0%,100% { transform:translateX(0); }
    15% { transform:translateX(-9px); } 30% { transform:translateX(9px); }
    45% { transform:translateX(-7px); } 60% { transform:translateX(7px); }
    75% { transform:translateX(-4px); } 90% { transform:translateX(4px); }
  }
  .mon.hit-hard { animation:shakeHard .5s ease-in-out;
                  box-shadow:0 0 22px rgba(251,191,36,.45); }
  .banner { font-size:15px; font-weight:700; padding:10px 14px; border-radius:10px;
            margin-bottom:18px; transition:background-color .4s ease; }
  .banner.mine { background:rgba(74,222,128,.14); color:var(--good); }
  .banner.theirs { background:rgba(139,145,167,.12); color:var(--dim); }
  .banner.over { background:rgba(251,191,36,.16); color:var(--warn); }
  .log { background:var(--card); border-radius:10px; padding:12px 14px; font-size:13px;
         max-height:180px; overflow-y:auto; }
  .log div { padding:2px 0; animation:fadeIn .4s ease; }
  .log div.super { color:var(--warn); font-weight:600; }
  .log div.weak { color:var(--dim); }
  .log div.immune { color:var(--dim); font-style:italic; }
  @keyframes fadeIn { from { opacity:0; transform:translateX(-6px); } to { opacity:1; transform:none; } }
  .moves { display:flex; flex-wrap:wrap; gap:6px; margin:10px 0 0; }
  button { font:inherit; color:inherit; background:#252a38; border:1px solid #333a4d;
           border-radius:8px; padding:5px 10px; cursor:pointer; transition:all .15s; }
  button:hover:not(:disabled) { background:#2f3547; border-color:#4a5268; }
  button:disabled { opacity:.35; cursor:not-allowed; }
  button.sel { background:#3b3170; border-color:#6d5ae0; }
  /* A link styled as a button. Navigating to the picker is a link, and
     a <button> inside an <a> is invalid HTML. */
  .btn { font:inherit; border-radius:8px; padding:5px 10px; text-decoration:none;
         font-weight:600; color:#12141c; background:#a78bfa; border:1px solid #a78bfa;
         text-align:center; transition:background .15s; }
  .btn:hover { background:#b9a3fb; }
  /* The commit, kept apart from the choosing.
     The move buttons above are a multiple-choice: neutral, one of many,
     and pressing one only changes a selection. This ends the turn. It
     was the same grey as the moves in an identical row directly below,
     so the thing that fires looked like a fifth move. */
  .commit { display:flex; justify-content:flex-end; margin-top:14px;
            padding-top:12px; border-top:1px solid #2c3040; }
  .commit button { background:#b42318; border-color:#d0362a; color:#fff;
                   font-weight:600; padding:9px 20px; min-height:44px; }
  .commit button:hover:not(:disabled) { background:#d0362a; border-color:#e8574a; }
  .pick { margin-top:14px; padding:14px; background:var(--card); border-radius:10px; }
  .pick.hidden { display:none; }
  /* Victory: the whole board gets a brief glow rather than a modal
     nobody asked for. */
  @keyframes celebrate { 0%,100% { box-shadow:none; } 50% { box-shadow:0 0 40px rgba(74,222,128,.35); } }
  .board.won { animation:celebrate 1.2s ease-in-out 2; border-radius:14px; }
  @media (prefers-reduced-motion: reduce) {
    *, *::before, *::after { animation:none !important; transition:none !important; }
  }
</style>
</head>
<body>
  <h1>Battle</h1>
  <p class="sub">you are <strong>${esc(trainer)}</strong> · battle <code>${esc(id)}</code> ·
     <a href="/">back to the pokedex</a></p>
  <!-- React mounts here. The banner is a component with role=status
       inside that tree: React updates its text in place rather than
       replacing the node, so the live region survives and a
       screen-reader user is actually told their turn began. The old
       code rebuilt the banner inside a full innerHTML swap, which
       announces to nobody. -->
  <div id="root"><div class="board">loading…</div></div>

<!-- What the server knows and the browser needs. A JSON island
     rather than values interpolated into the code, so client/battle.ts
     is a real module the compiler checks rather than a template
     literal it cannot see into. -->
<script type="application/json" id="boot">${JSON.stringify({
  id,
  me: trainer,
})}</script>
<script type="module" src="${assetURL('battle')}"></script>
</body></html>`
}

export function registerBattle(app: FastifyInstance) {
  // One place where an unreachable API becomes a 502.
  //
  // openapi-fetch returns `error` for an HTTP error response but THROWS
  // when the connection is refused, so every route below would
  // otherwise 500 whenever the pokedex is down - which reads as a bug
  // here rather than a dependency being unavailable. Wrapping each call
  // in its own try/catch is six chances to forget one.
  // Generic over the request, so a route's Params and Body types survive
  // the wrapper. Typed as FastifyRequest it would erase them, and every
  // request.params inside a wrapped handler goes back to unknown.
  const reachable =
    <Req extends FastifyRequest>(handler: (request: Req, reply: FastifyReply) => Promise<unknown>) =>
    async (request: Req, reply: FastifyReply) => {
    try {
      return await handler(request, reply)
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).send({ message: 'pokedex API unavailable' })
    }
  }

  // The trainer is kept in a cookie so the browser behaves like the CLI:
  // register once, then play. Not auth - it is the same token model the
  // API uses, which stops one player moving another's Pokemon.
  // A token the API does not know is the same situation as having no
  // cookie at all: the server restarted and lost its trainers, or the
  // cookie outlived them. Both need a fresh registration, so both are a
  // 401 - and the stale cookie is cleared, or the browser sends it
  // again on the next attempt and nothing improves.
  const staleTrainer = (reply: FastifyReply) => {
    reply.header('set-cookie', 'trainer=; Path=/; Max-Age=0; SameSite=Lax')
    return reply.code(401).send({ message: 'register first' })
  }

  const readTrainer = (request: FastifyRequest): Trainer | null => {
    const raw = request.headers.cookie || ''
    const m = /(?:^|;\s*)trainer=([^;]+)/.exec(raw)
    if (!m) return null
    try {
      return JSON.parse(decodeURIComponent(m[1]))
    } catch {
      return null
    }
  }

  app.get('/battle', async (request, reply) => {
    const me = readTrainer(request)
    // try/catch as well as the error field: openapi-fetch returns
    // `error` for an HTTP error response, but a refused connection
    // THROWS. Without this the lobby 500s when the API is down, which
    // reads as a bug in this service rather than a dependency being
    // unavailable - the same reason the card grid wraps its call.
    let waiting
    try {
      const { data, error } = await api.GET('/trainers/waiting')
      if (error) {
        request.log.error({ err: error }, 'lobby lookup failed')
        return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
      }
      waiting = data.waiting
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
    }
    return reply.type('text/html').send(lobbyPage({ me, waiting }))
  })

  // The waiting list as JSON, so the lobby can refresh without a full
  // page load.
  //
  // The lobby used to render once and never change, so a battle opened
  // by somebody else after your page loaded simply never appeared -
  // there was no join button because the list was frozen at load time.
  // The battle page has polled once a second all along; this is the
  // same idea for the one screen that needed it more.
  app.get('/battle/waiting', reachable(async (_request, reply) => {
    const { data, error } = await api.GET('/trainers/waiting')
    if (error) return reply.code(502).send(error)
    return { waiting: data.waiting }
  }))

  app.post<{ Body: NameBody }>('/battle/register', reachable(async (request, reply) => {
    const name = String((request.body || {}).name || '').trim()
    if (!name) return reply.code(400).send({ message: 'name is required' })
    const { data, error } = await api.POST('/trainers', { body: { name } })
    if (error) return reply.code(409).send(error)
    // httpOnly deliberately omitted: nothing here is a credential worth
    // protecting from the page's own script, and the script needs the
    // name to know whose turn it is.
    reply.header('set-cookie',
      `trainer=${encodeURIComponent(JSON.stringify(data))}; Path=/; Max-Age=604800; SameSite=Lax`)
    return { name: data.name }
  }))

  app.post<{ Body: TeamBody }>('/battle/open', reachable(async (request, reply) => {
    const me = readTrainer(request)
    if (!me) return reply.code(401).send({ message: 'register first' })
    const { data, error, response } = await api.POST('/battles', {
      body: { team: (request.body || {}).team || [] },
      params: { header: { 'X-Trainer-Token': me.token } },
    })
    if (error) {
      // 401 from the API means the token is unknown, which the client
      // can recover from by asking for a name. Flattening it to 400
      // turned that into a dead-end alert.
      if (response.status === 401) return staleTrainer(reply)
      return reply.code(400).send(error)
    }
    return { id: data.id }
  }))

  app.post<{ Params: IdParam; Body: TeamBody }>('/battle/:id/join', reachable(async (request, reply) => {
    const me = readTrainer(request)
    if (!me) return reply.code(401).send({ message: 'register first' })
    const { data, error, response } = await api.POST('/battles/{id}/join', {
      params: { path: { id: request.params.id }, header: { 'X-Trainer-Token': me.token } },
      body: { team: (request.body || {}).team || [] },
    })
    if (error) {
      if (response.status === 401) return staleTrainer(reply)
      return reply.code(409).send(error)
    }
    return { id: data.id }
  }))

  app.get<{ Params: IdParam }>('/battle/:id', async (request, reply) => {
    const me = readTrainer(request)
    if (!me) return reply.redirect('/battle')
    return reply.type('text/html').send(battlePage({ id: request.params.id, trainer: me.name }))
  })

  // The poll endpoint. Proxied rather than called from the browser so
  // the API stays on the cluster network and the page needs no CORS.
  app.get<{ Params: IdParam }>('/battle/:id/state', reachable(async (request, reply) => {
    const { data, error } = await api.GET('/battles/{id}', {
      params: { path: { id: request.params.id } },
    })
    if (error) return reply.code(404).send(error)
    return data
  }))

  app.post<{ Params: IdParam; Body: TurnBody }>('/battle/:id/turn', reachable(async (request, reply) => {
    const me = readTrainer(request)
    if (!me) return reply.code(401).send({ message: 'register first' })
    const { attacker, move, target } = request.body || {}
    const { data, error, response } = await api.POST('/battles/{id}/turn', {
      params: { path: { id: request.params.id }, header: { 'X-Trainer-Token': me.token } },
      body: { attacker, move, target },
    })
    if (error) {
      if (response.status === 401) return staleTrainer(reply)
      return reply.code(409).send(error)
    }
    return data
  }))
}

// lobbyPage lists open battles and, when there is no trainer yet, asks
// for a name first.
export function lobbyPage({ me, waiting }: { me: Trainer | null; waiting: WaitingBattle[] }) {
  const rows = waiting.length === 0
    ? '<p class="sub">nobody is waiting. open one below and share the link.</p>'
    : waiting.map((w) => `<div class="row">
         <div><strong>${esc(w.trainer)}</strong>
         <div class="sub" style="margin:0">${w.team.map(esc).join(', ')}</div></div>
         <a class="btn" href="/?join=${encodeURIComponent(w.battleId)}">join</a></div>`).join('')

  return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Battle lobby · Pokedex</title>
<style>
  :root { color-scheme: dark; --bg:#12141c; --card:#1c1f2b; --fg:#e8eaf2; --dim:#8b91a7; }
  * { box-sizing:border-box; }
  body { margin:0; padding:24px; background:var(--bg); color:var(--fg);
         font:15px/1.5 ui-sans-serif,system-ui,-apple-system,sans-serif; }
  a { color:inherit; }
  h1 { margin:0 0 4px; font-size:22px; }
  .sub { color:var(--dim); margin:0 0 20px; font-size:13px; }
  .row { display:flex; justify-content:space-between; align-items:center; gap:12px;
         background:var(--card); border-radius:10px; padding:12px 14px; margin-bottom:8px;
         animation:fadeIn .3s ease; }
  @keyframes fadeIn { from { opacity:0; transform:translateY(4px);} to { opacity:1; transform:none; } }
  input, button { font:inherit; color:inherit; background:#252a38; border:1px solid #333a4d;
                  border-radius:8px; padding:7px 12px; }
  button { cursor:pointer; transition:background .15s; }
  button:hover { background:#2f3547; }
  /* An anchor styled as a button: navigating to the picker is a link,
     and a <button> wrapped in an <a> is invalid HTML that screen
     readers announce wrongly. */
  .btn { font:inherit; background:#a78bfa; border:1px solid #a78bfa;
         border-radius:8px; padding:7px 12px; text-decoration:none;
         font-weight:600; color:#12141c; transition:background .15s; }
  .btn:hover { background:#b9a3fb; }
  .team { display:flex; gap:6px; flex-wrap:wrap; margin:10px 0; }
</style></head>
<body>
  <h1>Battle lobby</h1>
  <p class="sub">${me ? 'you are <strong>' + esc(me.name) + '</strong>' : 'pick a trainer name to start'} ·
    <a href="/">back to the pokedex</a>${me ? ' · <a href="#" id="rename">not you?</a>' : ''}</p>

  <!-- Always in the markup, hidden when there is already a trainer.
       Trainers live in server memory, so a deploy invalidates every
       token while the cookie survives - the page then says "you are
       <name>" and every action answers "register first", with the only
       form that could fix it hidden because a cookie was present. -->
  <div class="row" id="reg-row"${me ? ' hidden' : ''}><input id="name" placeholder="trainer name" maxlength="32">
    <button id="reg">register</button></div>

  ${me ? `<div class="row"><div><strong>your team</strong>
      <div class="sub" style="margin:0">pick from the pokedex, or start straight away
        and get a random team.</div></div></div>
    <div class="row"><a class="btn" href="/">pick a team &rarr;</a>
      <button id="open">open with a random team</button></div>` : ''}

  <h2 style="font-size:14px;color:var(--dim);margin:22px 0 8px">waiting</h2>
  <div id="waiting">${rows}</div>

<script type="module">
${PRELUDE}
// Reveal the register form for someone whose identity went stale, or
// who simply wants a different name.
const rename = document.getElementById('rename')
if (rename) rename.addEventListener('click', (ev) => {
  ev.preventDefault()
  const row = document.getElementById('reg-row')
  if (row) { row.hidden = false; document.getElementById('name').focus() }
})

const reg = document.getElementById('reg')
if (reg) reg.addEventListener('click', async () => {
  const name = document.getElementById('name').value.trim()
  if (!name) return
  const res = await fetch('/battle/register', {
    method:'POST', headers:{'content-type':'application/json', ...V}, body:JSON.stringify({name}) })
  if (res.ok) location.reload()
  else alert((await res.json()).message || 'that name is taken')
})

// No team in the body: this button is explicitly "open with a random
// team". Choosing one happens in the pokedex, the only screen that can
// show you what you are choosing between.
const open = document.getElementById('open')
if (open) open.addEventListener('click', async () => {
  const res = await fetch('/battle/open', {
    method:'POST', headers:{'content-type':'application/json', ...V}, body:JSON.stringify({}) })
  const body = await res.json()
  if (res.ok) { location.href = '/battle/' + body.id; return }
  // A 401 here means the token in the cookie is one the API no longer
  // knows - the server restarted and lost its trainers, which happens
  // on every deploy. The server has already cleared the cookie, so a
  // reload brings back the register form. Alerting "register first"
  // and stopping was a dead end: the page still said "you are <name>"
  // from the same cookie, and the form to register again was hidden
  // precisely because a cookie was present.
  if (res.status === 401) { location.reload(); return }
  alert(body.message || 'could not open that battle')
})

// Keep the waiting list current.
//
// It used to render once and never change, so a battle opened by
// somebody else after your page loaded never appeared - no join button,
// because the list was frozen at load. The battle page has polled all
// along; this is the same idea for the screen that needed it more.
//
// Paused while the tab is hidden. A lobby left open in a background tab
// is the shape that produced 46,000 wasted requests from one stale
// battle page, and nobody is waiting to join a battle they cannot see.
function renderWaiting(list) {
  const box = document.getElementById('waiting')
  if (!box) return
  box.innerHTML = list.length === 0
    ? '<p class="sub">nobody is waiting. open one below and share the link.</p>'
    : list.map((w) => '<div class="row">' +
        '<div><strong>' + esc(w.trainer) + '</strong>' +
        '<div class="sub" style="margin:0">' + w.team.map(esc).join(', ') + '</div></div>' +
        '<a class="btn" href="/?join=' + encodeURIComponent(w.battleId) + '">join</a></div>').join('')
}

let lobbyMisses = 0
async function pollLobby() {
  if (!document.hidden) {
    try {
      const res = await fetch('/battle/waiting', { headers: V })
      if (res.ok) {
        lobbyMisses = 0
        const b = await res.json()
        renderWaiting(b.waiting || [])
      } else if (TERMINAL.has(res.status)) {
        return // this client is too old, or the endpoint is gone
      } else if (++lobbyMisses >= 5) {
        return // the API has been unreachable for a while; stop asking
      }
    } catch { /* a dropped poll retries on the next tick */ }
  }
  setTimeout(pollLobby, 3000)
}
setTimeout(pollLobby, 3000)
</script>
</body></html>`
}
