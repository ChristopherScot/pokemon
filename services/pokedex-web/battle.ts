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
  @keyframes fadeIn { from { opacity:0; transform:translateX(-6px); } to { opacity:1; transform:none; } }
  .moves { display:flex; flex-wrap:wrap; gap:6px; margin:10px 0 0; }
  button { font:inherit; color:inherit; background:#252a38; border:1px solid #333a4d;
           border-radius:8px; padding:5px 10px; cursor:pointer; transition:all .15s; }
  button:hover:not(:disabled) { background:#2f3547; border-color:#4a5268; }
  button:disabled { opacity:.35; cursor:not-allowed; }
  button.sel { background:#3b3170; border-color:#6d5ae0; }
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
  <div id="board" class="board">loading…</div>

<script type="module">
const ID = ${JSON.stringify(id)}
const ME = ${JSON.stringify(trainer)}
const COLOURS = ${JSON.stringify(TYPE_COLOURS)}

let seen = -1
let picked = { attacker: 0, move: 0, target: 0 }
// -1 means "not rendered yet"; the first render adopts the log's
// length rather than replaying it.
let lastLogLen = -1

// The hp percentage each bar is currently DRAWING, so a re-render can
// start the transition from where the bar was rather than from the
// value it is moving to.
const shownHp = new Map()

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => (
  { '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[c]))

function hpColour(hp, max) {
  const f = max > 0 ? hp / max : 0
  return f <= 0.2 ? 'var(--danger)' : f <= 0.5 ? 'var(--warn)' : 'var(--good)'
}

function monEl(p, side, i, selectable, selected) {
  const pct = p.maxHp > 0 ? Math.max(0, p.hp / p.maxHp * 100) : 0
  const types = p.types.map((t) =>
    '<span class="type" style="background:' + (COLOURS[t] || '#666') + '">' + esc(t) + '</span>').join('')
  return '<div class="mon' + (p.fainted ? ' fainted' : '') + '"' +
    ' data-slot="' + side + '-' + i + '"' +
    (selectable ? ' data-pick="' + side + '" data-index="' + i + '"' : '') +
    (selected ? ' style="outline:2px solid #6d5ae0"' : '') + '>' +
    '<img src="' + esc(p.sprite) + '" alt="" loading="lazy">' +
    '<div><div class="name">' + esc(p.name) + '</div>' +
    '<div class="types">' + types + '</div>' +
    '<div class="hpwrap"><div class="hp" data-pct="' + pct + '" style="width:' + pct + '%;background:' + hpColour(p.hp, p.maxHp) + '"></div></div></div>' +
    '<div class="hpnum">' + p.hp + '/' + p.maxHp + '</div></div>'
}

// renderSpectator draws a battle this trainer is not in.
//
// A waiting battle gets a join button, because that is the whole point
// of sharing the link. One already underway is read-only - the server
// would reject a third side anyway, and a button that always errors is
// worse than no button.
function renderSpectator(b) {
  const joinable = b.status === 'waiting' && b.sides.length < 2
  let html = '<div class="banner theirs">' +
    (joinable ? 'this battle is waiting for an opponent'
              : 'watching ' + esc(b.sides.map((s) => s.trainer).join(' vs '))) +
    '</div>'

  for (const side of b.sides) {
    html += '<div class="side"><h2>' + esc(side.trainer) + '</h2>' +
      side.team.map((p, i) => monEl(p, 'them', i, false, false)).join('') +
      '</div>'
  }

  if (joinable) {
    html += '<div class="pick"><div class="moves">' +
      '<button id="join-battle">join as ' + esc(ME) + '</button>' +
      '</div></div>'
  }

  html += '<div class="log" id="log">' + b.log.slice(-8).map(
    (e) => '<div>' + esc(e.text) + '</div>').join('') + '</div>'

  const board = document.getElementById('board')
  board.innerHTML = html
  board.className = 'board'

  const join = document.getElementById('join-battle')
  if (join) {
    join.onclick = async () => {
      join.disabled = true
      const res = await fetch(location.pathname + '/join', { method: 'POST' })
      if (res.ok) { location.reload(); return }
      // 401 means this trainer no longer exists - the server has already
      // cleared the cookie, so reloading lands on the name prompt rather
      // than leaving a dead button. Reachable whenever the server has
      // restarted since the cookie was set, which is every deploy.
      if (res.status === 401) { location.reload(); return }
      join.disabled = false
      join.textContent = 'could not join - try the lobby'
    }
  }
}

function render(b) {
  const mineIdx = b.sides.findIndex((s) => s.trainer === ME)

  // Not a participant: show the battle and a way in, rather than
  // indexing our way into undefined.
  //
  // findIndex returns -1, which made theirsIdx 2, which made BOTH sides
  // undefined, and render threw on the first .team - leaving the board
  // on its initial "loading…" forever with no error anywhere the player
  // could see. Reachable without doing anything strange: open a battle
  // link someone sent you, or keep a trainer cookie across a deploy
  // that cleared the server's in-memory battles.
  if (mineIdx === -1) {
    renderSpectator(b)
    return
  }

  const theirsIdx = 1 - mineIdx
  const mine = b.sides[mineIdx], theirs = b.sides[theirsIdx]
  const myTurn = b.status === 'active' && b.turn === ME

  let banner, cls
  if (b.status === 'finished') {
    banner = b.winner === ME ? '★ you win!' : (b.winner ? b.winner + ' wins' : 'a draw')
    cls = 'over'
  } else if (b.status === 'waiting') {
    banner = 'waiting for an opponent — share the battle id'
    cls = 'theirs'
  } else {
    banner = myTurn ? 'your turn' : 'waiting on ' + esc(b.turn) + '…'
    cls = myTurn ? 'mine' : 'theirs'
  }

  let html = '<div class="banner ' + cls + '">' + banner + '</div>'

  if (theirs) {
    html += '<div class="side"><h2>' + esc(theirs.trainer) + '</h2>' +
      theirs.team.map((p, i) => monEl(p, 'them', i, myTurn && !p.fainted, myTurn && picked.target === i)).join('') +
      '</div>'
  }
  html += '<div class="side"><h2>you</h2>' +
    mine.team.map((p, i) => monEl(p, 'me', i, myTurn && !p.fainted, myTurn && picked.attacker === i)).join('') +
    '</div>'

  if (myTurn) {
    const att = mine.team[picked.attacker]
    html += '<div class="pick"><strong>' + esc(att.name) + '</strong> uses…' +
      '<div class="moves">' +
      att.moves.map((m, i) =>
        '<button data-move="' + i + '"' + (picked.move === i ? ' class="sel"' : '') + '>' +
        esc(m.name) + (m.power ? ' <span style="opacity:.6">' + m.power + '</span>' : ' <span style="opacity:.6">—</span>') +
        '</button>').join('') +
      '</div><div class="moves" style="margin-top:10px">' +
      '<button id="go">attack ' + esc(theirs.team[picked.target].name) + '</button>' +
      '</div></div>'
  }

  html += '<div class="log" id="log">' + b.log.slice(-8).map((e) => {
    const k = e.effectiveness >= 2 ? ' class="super"' : (e.effectiveness > 0 && e.effectiveness < 1 ? ' class="weak"' : '')
    return '<div' + k + '>' + esc(e.text) + '</div>'
  }).join('') + '</div>'

  const board = document.getElementById('board')
  board.innerHTML = html
  board.className = 'board' + (b.status === 'finished' && b.winner === ME ? ' won' : '')
  document.getElementById('log').scrollTop = 9e9

  // The bars start at the PREVIOUS hp and are moved to the real one on
  // the next frame, so the CSS width transition has something to
  // animate from. Rendering straight to the new value paints the bar
  // already drained - the transition has no start state, and the drop
  // you are meant to watch has already happened.
  for (const [key, was] of shownHp) {
    const bar = board.querySelector('[data-slot="' + key + '"] .hp')
    if (bar && bar.dataset.pct !== undefined && was !== bar.dataset.pct) {
      bar.style.width = was + '%'
    }
  }
  requestAnimationFrame(() => {
    for (const bar of board.querySelectorAll('.hp')) {
      bar.style.width = bar.dataset.pct + '%'
    }
    for (const [, s] of b.sides.entries()) void s
    shownHp.clear()
    for (const [si, side] of b.sides.entries()) {
      for (const [i, p] of side.team.entries()) {
        const key = (si === mineIdx ? 'me' : 'them') + '-' + i
        shownHp.set(key, p.maxHp > 0 ? Math.max(0, (p.hp / p.maxHp) * 100) : 0)
      }
    }
  })

  // Damage floats come from the NEW log entries, so a poll that brings
  // three turns at once animates all three rather than only the last.
  //
  // lastLogLen starts at the log's length on the FIRST render, not at
  // zero: a battle joined mid-way would otherwise replay every hit that
  // already happened as floats, and then go quiet for the turns that
  // actually arrive while you are watching.
  if (lastLogLen < 0) lastLogLen = b.log.length
  else if (b.log.length > lastLogLen) {
    for (const e of b.log.slice(lastLogLen)) {
      if (!e.damage || !e.target) continue
      for (const [si, s] of b.sides.entries()) {
        const i = s.team.findIndex((p) => p.name === e.target)
        if (i < 0) continue
        const key = (si === mineIdx ? 'me' : 'them') + '-' + i
        const el = board.querySelector('[data-slot="' + key + '"]')
        if (!el) continue
        const f = document.createElement('div')
        f.className = 'float ' + (e.effectiveness >= 2 ? 'super' : e.effectiveness < 1 ? 'weak' : 'normal')
        f.textContent = '-' + e.damage + (e.effectiveness >= 2 ? ' !!' : '')
        el.appendChild(f)
        // Every hit shakes; a super-effective one shakes harder. Only
        // animating 2x meant most turns had no feedback at all beyond a
        // bar moving.
        el.classList.add(e.effectiveness >= 2 ? 'hit-hard' : 'hit')
        setTimeout(() => el.classList.remove('hit', 'hit-hard'), 500)
        setTimeout(() => f.remove(), 2400)
      }
    }
    lastLogLen = b.log.length
  }

  board.querySelectorAll('[data-pick]').forEach((el) => {
    el.addEventListener('click', () => {
      const i = Number(el.dataset.index)
      if (el.dataset.pick === 'me') { picked.attacker = i; picked.move = 0 } else picked.target = i
      render(b)
    })
  })
  board.querySelectorAll('[data-move]').forEach((el) => {
    el.addEventListener('click', () => { picked.move = Number(el.dataset.move); render(b) })
  })
  const go = document.getElementById('go')
  if (go) go.addEventListener('click', attack)
}

async function attack() {
  const go = document.getElementById('go')
  if (go) { go.disabled = true; go.textContent = 'attacking…' }
  const res = await fetch('/battle/' + ID + '/turn', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(picked),
  })
  if (!res.ok) {
    // A 409 means the state moved on. Show it rather than silently
    // leaving the button dead.
    const body = await res.json().catch(() => ({ message: 'that move was rejected' }))
    const log = document.getElementById('log')
    if (log) {
      const d = document.createElement('div')
      d.style.color = 'var(--danger)'
      d.textContent = body.message || 'that move was rejected'
      log.appendChild(d)
    }
  }
  poll()
}

async function poll() {
  try {
    const res = await fetch('/battle/' + ID + '/state')
    if (res.ok) {
      const b = await res.json()
      if (b.version !== seen) { seen = b.version; render(b) }
    }
  } catch { /* a dropped poll is not worth showing; the next one retries */ }
  setTimeout(poll, 1000)
}
poll()
</script>
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
function lobbyPage({ me, waiting }: { me: Trainer | null; waiting: WaitingBattle[] }) {
  const rows = waiting.length === 0
    ? '<p class="sub">nobody is waiting. open one below and share the link.</p>'
    : waiting.map((w) => `<div class="row">
         <div><strong>${esc(w.trainer)}</strong>
         <div class="sub" style="margin:0">${w.team.map(esc).join(', ')}</div></div>
         <button data-join="${esc(w.battleId)}">join</button></div>`).join('')

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
  .team { display:flex; gap:6px; flex-wrap:wrap; margin:10px 0; }
</style></head>
<body>
  <h1>Battle lobby</h1>
  <p class="sub">${me ? 'you are <strong>' + esc(me.name) + '</strong>' : 'pick a trainer name to start'} ·
    <a href="/">back to the pokedex</a></p>

  ${me ? '' : `<div class="row"><input id="name" placeholder="trainer name" maxlength="32">
    <button id="reg">register</button></div>`}

  ${me ? `<div class="row"><div><strong>open a battle</strong>
      <div class="sub" style="margin:0">three pokemon, comma separated</div></div></div>
    <div class="row"><input id="team" style="flex:1" placeholder="charizard, blastoise, venusaur">
      <button id="open">open</button></div>` : ''}

  <h2 style="font-size:14px;color:var(--dim);margin:22px 0 8px">waiting</h2>
  ${rows}

<script type="module">
const reg = document.getElementById('reg')
if (reg) reg.addEventListener('click', async () => {
  const name = document.getElementById('name').value.trim()
  if (!name) return
  const res = await fetch('/battle/register', {
    method:'POST', headers:{'content-type':'application/json'}, body:JSON.stringify({name}) })
  if (res.ok) location.reload()
  else alert((await res.json()).message || 'that name is taken')
})

const team = () => document.getElementById('team').value.split(',').map(s => s.trim()).filter(Boolean)

const open = document.getElementById('open')
if (open) open.addEventListener('click', async () => {
  const res = await fetch('/battle/open', {
    method:'POST', headers:{'content-type':'application/json'}, body:JSON.stringify({team: team()}) })
  const body = await res.json()
  if (res.ok) location.href = '/battle/' + body.id
  else alert(body.message || 'could not open that battle')
})

document.querySelectorAll('[data-join]').forEach((el) => el.addEventListener('click', async () => {
  const t = document.getElementById('team')
  const res = await fetch('/battle/' + el.dataset.join + '/join', {
    method:'POST', headers:{'content-type':'application/json'},
    body: JSON.stringify({team: t ? team() : []}) })
  const body = await res.json()
  if (res.ok) location.href = '/battle/' + el.dataset.join
  else alert(body.message || 'could not join')
}))
</script>
</body></html>`
}
