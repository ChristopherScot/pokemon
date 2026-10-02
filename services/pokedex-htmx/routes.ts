import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify'
import type { components } from '@christopherscot/pokedex-client'

import { api } from './api.ts'
import { byURL } from './assets.ts'
import { board, battlePage, goneBoard, sideFor, type Turn } from './battle.ts'
import { downloadsFooter } from './downloads.ts'
import { lobbyPage, registerRow, waitingRows, type Trainer } from './lobby.ts'
import { filters, grid, page, teamSlots, url, type Ctx } from './pokedex.ts'
import { esc, TEAM_SIZE } from './types.ts'

type Pokemon = components['schemas']['Pokemon']

// Shape-check: JSON.parse returns truthy junk (5, "hi") that would sail past
// `if (!me)` and send `X-Trainer-Token: undefined` to the API.
export const readTrainer = (request: FastifyRequest): Trainer | null => {
  const m = /(?:^|;\s*)trainer=([^;]+)/.exec(request.headers.cookie || '')
  if (!m) return null
  try {
    const t: unknown = JSON.parse(decodeURIComponent(m[1]))
    if (t && typeof t === 'object'
      && typeof (t as Trainer).name === 'string'
      && typeof (t as Trainer).token === 'string') return t as Trainer
    return null
  } catch {
    return null
  }
}

// decodeURIComponent throws on a bad escape; a cookie ships every request, so
// an unguarded decode would be a permanent 500 until the user clears cookies.
const readResume = (request: FastifyRequest): string => {
  const m = /(?:^|;\s*)battle=([^;]+)/.exec(request.headers.cookie || '')
  if (!m) return ''
  try {
    return decodeURIComponent(m[1])
  } catch {
    return ''
  }
}

// Repeated `team` fields, deduped before the cap so a,a,b,c keeps c. The API
// accepts three-of-a-kind, and float placement matches on the first slot with
// that name, so dedupe is enforced here.
export const readTeam = (v: unknown): string[] => {
  const all = v === undefined ? [] : Array.isArray(v) ? v : [v]
  return [...new Set(all.map(String).filter(Boolean))].slice(0, TEAM_SIZE)
}

const one = (v: unknown): string => (typeof v === 'string' ? v : '')

// HttpOnly because the trainer cookie holds an API token; both cookies are
// server-read only. Secure is dropped for local HTTP via INSECURE_COOKIES.
const COOKIE_FLAGS = 'Path=/; HttpOnly; SameSite=Lax'
  + (process.env.INSECURE_COOKIES ? '' : '; Secure')

// Unfiltered lookup: a pick made before filtering has no entry in ctx.pokemon
// to read a sprite from.
async function spritesFor(names: string[]): Promise<Record<string, string>> {
  if (names.length === 0) return {}
  const out: Record<string, string> = {}
  const found = await Promise.all(names.map((name) =>
    api.GET('/pokemon/{name}', { params: { path: { name } } }).catch(() => null)))
  for (const [i, hit] of found.entries()) {
    if (hit && !hit.error && hit.data) out[names[i]] = hit.data.sprite
  }
  return out
}

export function register(app: FastifyInstance) {
  // Asset URLs one per line, so CI can fetch each and prove browser scripts
  // are bundled in rather than read from a disk the image does not have.
  app.get('/assets', async (_request, reply) =>
    reply.type('text/plain').send([...byURL.keys()].join('\n') + '\n'))

  app.get<{ Params: { '*': string } }>('/assets/*', async (request, reply) => {
    const hit = byURL.get(request.url.split('?')[0])
    if (!hit) return reply.code(404).send()
    return reply
      .type('text/javascript')
      .header('cache-control', 'public, max-age=31536000, immutable')
      .send(hit.body)
  })

  // Everything the page needs is in the URL, so a link and a reload restore
  // the same state.
  app.get('/', async (request, reply) => {
    const q = request.query as Record<string, unknown>
    const active = one(q.type)
    const team = readTeam(q.team)
    const ctx: Omit<Ctx, 'pokemon' | 'types' | 'sprites'> = {
      active,
      join: one(q.join),
      team,
      resume: readResume(request),
    }

    let list, typeList
    try {
      ;[list, typeList] = await Promise.all([
        api.GET('/pokemon', { params: { query: active ? { type: active } : {} } }),
        api.GET('/types', {}),
      ])
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
    }
    if (list.error || typeList.error) {
      request.log.error({ err: list.error || typeList.error }, 'pokedex lookup failed')
      return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
    }

    return reply.type('text/html').send(page({
      ...ctx,
      pokemon: list.data.pokemon,
      types: typeList.data.types,
      sprites: await spritesFor(team),
    }, one(q.error)))
  })

  // Stateless: the current team comes in on the form, the new team goes back
  // out as hidden inputs. A team only becomes server state at open/join.
  app.post('/team', async (request, reply) => {
    const q = request.query as Record<string, unknown>
    const body = (request.body ?? {}) as Record<string, unknown>

    const team = readTeam(body.team)
    const toggle = one(q.toggle)
    const drop = one(q.drop)

    let next = team
    if (drop) next = team.filter((t) => t !== drop)
    else if (toggle) {
      next = team.includes(toggle)
        ? team.filter((t) => t !== toggle)
        : team.length >= TEAM_SIZE ? team : [...team, toggle]
    }

    const active = one(body.active)
    const join = one(body.join)

    // Types are fetched too because the filter nav is re-rendered, and its
    // links carry the team.
    let list, typeList
    try {
      ;[list, typeList] = await Promise.all([
        api.GET('/pokemon', { params: { query: active ? { type: active } : {} } }),
        api.GET('/types', {}),
      ])
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).send('pokedex API unavailable')
    }
    if (list.error || typeList.error) return reply.code(502).send('pokedex API unavailable')

    const ctx: Ctx = {
      pokemon: list.data.pokemon, types: typeList.data.types, active, join, team: next,
      sprites: await spritesFor(next),
      resume: readResume(request),
    }

    // Three fragments (slots, grid, filters) because a pick changes all three:
    // a third pick disables every non-picked card, and every filter link
    // carries the team. Client/page.js re-applies its search filter on the
    // new grid via htmx:afterSettle. hx-push-url keeps the team in the URL.
    return reply
      .type('text/html')
      .header('HX-Push-Url', url({ active, join, team: next }))
      .send(teamSlots(ctx) + grid(ctx, true) + filters(ctx, true))
  })

  app.get('/battle', async (request, reply) => {
    const me = readTrainer(request)
    try {
      const { data, error } = await api.GET('/trainers/waiting')
      if (error) return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
      return reply.type('text/html').send(lobbyPage({ me, waiting: data.waiting }))
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
    }
  })

  app.get('/battle/waiting', async (request, reply) => {
    try {
      const { data, error } = await api.GET('/trainers/waiting')
      if (error) return reply.code(502).send('')
      return reply.type('text/html').send(waitingRows(data.waiting))
    } catch {
      return reply.code(502).send('')
    }
  })

  app.get('/battle/rename', async (_request, reply) =>
    reply.type('text/html').send(registerRow()))

  // Registers and, if the pending team/join rode along, commits in one shot.
  // A rejected name comes back 200 with the form and reason; htmx discards
  // 4xx bodies, so a 409 would leave the page silent.
  app.post('/battle/register', async (request, reply) => {
    const body = (request.body ?? {}) as Record<string, unknown>
    const name = one(body.name).trim()
    // Same POST serves two forms (lobby row, pokedex dialog); each has to get
    // its own shape back or the swap would replace the wrong element.
    const fromDialog = one(body.commit) !== '' || one(body.join) !== '' ||
      readTeam(body.team).length > 0
    const again = (why: string) =>
      reply.type('text/html').send(fromDialog
        ? nameDialog(readTeam(body.team), one(body.join), one(body.active), why)
        : registerRow() + `<p class="sub" role="alert">${esc(why)}</p>`)

    if (!name) return again('a name is required')

    let created
    try {
      created = await api.POST('/trainers', { body: { name } })
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return again('the pokedex is unreachable — try again in a moment')
    }
    if (created.error) {
      return again('that name is taken')
    }

    reply.header('set-cookie',
      `trainer=${encodeURIComponent(JSON.stringify(created.data))}; ${COOKIE_FLAGS}; Max-Age=604800`)

    const team = readTeam(body.team)
    const join = one(body.join)
    if (team.length || join || one(body.commit)) {
      return commit(request, reply, created.data as Trainer, team, join, one(body.active))
    }
    return reply.header('HX-Refresh', 'true').code(204).send()
  })

  const commit = async (
    request: FastifyRequest, reply: FastifyReply,
    me: Trainer, team: string[], join: string, active: string,
  ) => {
    try {
      if (join) {
        const { data, error, response } = await api.POST('/battles/{id}/join', {
          params: { path: { id: join }, header: { 'X-Trainer-Token': me.token } },
          body: { team },
        })
        if (error) return rejected(reply, response.status, team, join, active)
        return toBattle(reply, data.id)
      }
      const { data, error, response } = await api.POST('/battles', {
        body: { team },
        params: { header: { 'X-Trainer-Token': me.token } },
      })
      if (error) return rejected(reply, response.status, team, join, active)
      return toBattle(reply, data.id)
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return rejected(reply, 502, team, join, active)
    }
  }

  // Battle id in a cookie so the pokedex can offer a way back to it.
  const toBattle = (reply: FastifyReply, id: string) =>
    reply
      .header('set-cookie', `battle=${encodeURIComponent(id)}; ${COOKIE_FLAGS}; Max-Age=86400`)
      .header('HX-Redirect', `/battle/${encodeURIComponent(id)}`)
      .code(204)
      .send()

  // 401 means no trainer yet: return the name dialog carrying the pending
  // team, join and active filter so registering finishes the original intent.
  const rejected = (
    reply: FastifyReply, status: number, team: string[], join: string, active: string,
  ) => {
    if (status === 401) {
      return reply.type('text/html').send(nameDialog(team, join, active))
    }
    const back = url({ active, join, team })
    return reply
      .header('HX-Redirect',
        `${back}${back.includes('?') ? '&' : '?'}error=${encodeURIComponent('that did not work')}`)
      .code(204).send()
  }

  const openOrJoin = (join: string) => async (request: FastifyRequest, reply: FastifyReply) => {
    const me = readTrainer(request)
    const body = (request.body ?? {}) as Record<string, unknown>
    const team = readTeam(body.team)
    const active = one(body.active)
    if (!me) return reply.type('text/html').send(nameDialog(team, join, active))
    return commit(request, reply, me, team, join, active)
  }

  app.post('/battle/open', async (request, reply) => openOrJoin('')(request, reply))

  app.post<{ Params: { id: string } }>('/battle/:id/join', async (request, reply) =>
    openOrJoin(request.params.id)(request, reply))

  app.get('/downloads', async (request, reply) => {
    const q = request.query as Record<string, unknown>
    return reply.type('text/html').send(await downloadsFooter(one(q.os), one(q.arch)))
  })

  // First board is rendered into the page so it does not flash empty before
  // the first poll.
  app.get<{ Params: { id: string } }>('/battle/:id', async (request, reply) => {
    const me = readTrainer(request)
    if (!me) return reply.redirect('/battle')

    const first = await boardFor(request, request.params.id, me.name, ZERO, '')
    return reply
      .header('set-cookie',
        `battle=${encodeURIComponent(request.params.id)}; ${COOKIE_FLAGS}; Max-Age=86400`)
      .type('text/html')
      .send(battlePage({ id: request.params.id, trainer: me.name, first }))
  })

  // No cookie means spectator, not an error: an empty name never matches a
  // side, so board() renders the watching view.
  app.get<{ Params: { id: string } }>('/battle/:id/board', async (request, reply) => {
    const me = readTrainer(request)
    const q = request.query as Record<string, unknown>
    return reply.type('text/html').send(
      await boardFor(request, request.params.id, me?.name ?? '', readTurn(q), ''))
  })

  app.post<{ Params: { id: string } }>('/battle/:id/turn', async (request, reply) => {
    const me = readTrainer(request)
    if (!me) return reply.type('text/html').send(goneBoard('register first'))
    const sel = readTurn((request.body ?? {}) as Record<string, unknown>)

    let why = ''
    try {
      const { error, response } = await api.POST('/battles/{id}/turn', {
        params: { path: { id: request.params.id }, header: { 'X-Trainer-Token': me.token } },
        body: sel,
      })
      if (error) {
        why = String((error as { message?: string }).message ?? 'that move was rejected')
        if (response.status === 401) why = 'register first'
      }
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      why = 'the server is unreachable'
    }
    return reply.type('text/html').send(
      await boardFor(request, request.params.id, me.name, sel, why))
  })

  async function boardFor(
    request: FastifyRequest, id: string, me: string, sel: Turn, rejected: string,
  ): Promise<string> {
    try {
      const { data, error } = await api.GET('/battles/{id}', { params: { path: { id } } })
      if (error || !data) {
        return goneBoard('this battle is over — the server restarted, or it expired')
      }
      return board({ b: data, me, sel, rejected })
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return goneBoard('the server is unreachable — reload to try again')
    }
  }
}

const ZERO: Turn = { attacker: 0, move: 0, target: 0 }

// 0 is the only index the API is guaranteed to accept, so garbage clamps to a
// legal move rather than an API error.
export const readTurn = (v: Record<string, unknown>): Turn => {
  const n = (x: unknown, fallback = 0) => {
    const i = Number(x)
    return Number.isInteger(i) && i >= 0 && i < 6 ? i : fallback
  }
  return { attacker: n(v.attacker), move: n(v.move), target: n(v.target) }
}

// Carries the pending team and join so registering completes the original intent.
function nameDialog(team: string[], join: string, active = '', why = ''): string {
  const carried = team.map((t) => `<input type="hidden" name="team" value="${esc(t)}">`).join('')
  return `<div class="modal-backdrop" id="name-dialog" role="dialog" aria-modal="true"
       aria-label="Pick a trainer name">
  <form class="modal" hx-post="/battle/register" hx-target="#name-dialog" hx-swap="outerHTML">
    ${carried}<input type="hidden" name="join" value="${esc(join)}">
    <input type="hidden" name="active" value="${esc(active)}">
    <input type="hidden" name="commit" value="1">
    <h2>Pick a trainer name</h2>
    <p>Other trainers see this in the lobby.</p>
    <input id="trainer-name" name="name" aria-label="Trainer name" maxlength="32"
           placeholder="e.g. Ash" autocomplete="off" autofocus required>
    ${why ? `<p class="modal-error" role="alert">${esc(why)}</p>` : ''}
    <div class="modal-actions">
      <button type="button" class="ghost"
              onclick="this.closest('#name-dialog').remove()">cancel</button>
      <button type="submit">start</button>
    </div>
  </form>
</div>`
}
