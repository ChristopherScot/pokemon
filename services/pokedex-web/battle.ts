
import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify'
import createClient, { exponentialRetry, noRetry } from '@christopherscot/pokedex-client'

import type { components } from '@christopherscot/pokedex-client'

type Battle = components['schemas']['Battle']
type WaitingBattle = components['schemas']['WaitingBattle']
type BattleEvent = components['schemas']['BattleEvent']
type BattlePokemon = components['schemas']['BattlePokemon']

type Trainer = { name: string; token: string }

type IdParam = { id: string }
type TeamBody = { team?: string[] }
type NameBody = { name?: string }
type TurnBody = { attacker: number; move: number; target: number }

const baseUrl = process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local'
const api = createClient({
  baseUrl,
  policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
})

const esc = (s: unknown): string =>
  String(s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] ?? c)

import { assetURL, preloadURLs } from './assets.ts'
import { island } from './types.ts'
import { UI_VERSION } from './version.ts'
export { UI_VERSION }

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
  .mon.fainted { opacity:.35; filter:grayscale(1); }
  .mon img { width:52px; height:52px; image-rendering:pixelated; }
  .name { font-weight:600; text-transform:capitalize; }
  .types { display:flex; gap:4px; margin-top:3px; }
  .type { font-size:10px; text-transform:uppercase; letter-spacing:.04em;
          padding:1px 6px; border-radius:999px; color:#12141c; font-weight:700; }
  .hpwrap { height:8px; background:#2a2e3d; border-radius:999px; overflow:hidden; margin-top:6px; }
  .hp { height:100%; border-radius:999px; transition:width .55s cubic-bezier(.22,.61,.36,1), background-color .3s; }
  .hpnum { text-align:right; font-variant-numeric:tabular-nums; color:var(--dim); font-size:13px; }
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
  .float.immune{ color:var(--dim); font-size:13px; font-style:italic; }
  .float.normal{ color:var(--danger); }
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
  .btn { font:inherit; border-radius:8px; padding:5px 10px; text-decoration:none;
         font-weight:600; color:#12141c; background:#a78bfa; border:1px solid #a78bfa;
         text-align:center; transition:background .15s; }
  .btn:hover { background:#b9a3fb; }
  .commit { display:flex; justify-content:flex-end; margin-top:14px;
            padding-top:12px; border-top:1px solid #2c3040; }
  .commit button { background:#b42318; border-color:#d0362a; color:#fff;
                   font-weight:600; padding:9px 20px; min-height:44px; }
  .commit button:hover:not(:disabled) { background:#d0362a; border-color:#e8574a; }
  .pick { margin-top:14px; padding:14px; background:var(--card); border-radius:10px; }
  .pick.hidden { display:none; }
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
<script type="application/json" id="boot">${island({
  id,
  me: trainer,
})}</script>
${preloadURLs('battle').map((u) => `<link rel="modulepreload" href="${u}">`).join('')}
<script type="module" src="${assetURL('battle')}"></script>
</body></html>`
}

export function registerBattle(app: FastifyInstance) {
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

export function lobbyPage({ me, waiting }: { me: Trainer | null; waiting: WaitingBattle[] }) {
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
  .linkish { background:none; border:0; padding:0; color:inherit;
             text-decoration:underline; cursor:pointer; font:inherit; }
  .btn { font:inherit; background:#a78bfa; border:1px solid #a78bfa;
         border-radius:8px; padding:7px 12px; text-decoration:none;
         font-weight:600; color:#12141c; transition:background .15s; }
  .btn:hover { background:#b9a3fb; }
  .team { display:flex; gap:6px; flex-wrap:wrap; margin:10px 0; }
</style></head>
<body>
  <!-- React mounts here. The server still renders the page shell and
       ships the first waiting list in the boot island, so the lobby is
       useful before any JavaScript runs and does not flash empty. -->
  <div id="root"></div>

<script type="application/json" id="boot">${island({ me: me && { name: me.name }, waiting })}</script>
${preloadURLs('lobby').map((u) => `<link rel="modulepreload" href="${u}">`).join('')}
<script type="module" src="${assetURL('lobby')}"></script>

</body></html>`
}
