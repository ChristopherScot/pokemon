import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify'
import type { components } from '@christopherscot/pokedex-client'

import { api } from './api.ts'
import { byURL } from './assets.ts'
import { board, battlePage, goneBoard, sideFor, type Turn } from './battle.ts'
import { downloadsFooter } from './downloads.ts'
import { lobbyPage, registerRow, waitingList, type Trainer } from './lobby.ts'
import { grid, page, teamSlots, url, type Ctx } from './pokedex.ts'
import { esc, TEAM_SIZE } from './types.ts'

type Pokemon = components['schemas']['Pokemon']

const readTrainer = (request: FastifyRequest): Trainer | null => {
  const m = /(?:^|;\s*)trainer=([^;]+)/.exec(request.headers.cookie || '')
  if (!m) return null
  try {
    return JSON.parse(decodeURIComponent(m[1]))
  } catch {
    return null
  }
}

const readResume = (request: FastifyRequest): string => {
  const m = /(?:^|;\s*)battle=([^;]+)/.exec(request.headers.cookie || '')
  return m ? decodeURIComponent(m[1]) : ''
}

// The team arrives as repeated `team` fields - from the querystring on a
// full page load, or from the form's hidden inputs on a toggle.
const readTeam = (v: unknown): string[] => {
  const all = v === undefined ? [] : Array.isArray(v) ? v : [v]
  return all.map(String).filter(Boolean).slice(0, TEAM_SIZE)
}

const one = (v: unknown): string => (typeof v === 'string' ? v : '')

export function register(app: FastifyInstance) {
  // The asset URLs, one per line. It exists for CI: the bundle is run
  // from an empty directory and each URL fetched, which is what proves
  // the browser scripts are compiled INTO the bundle rather than read
  // from a disk the image does not have.
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

  // The pokedex. Everything the page needs is in the URL, so a filter
  // link, a bookmark and a reload all restore the same state.
  app.get('/', async (request, reply) => {
    const q = request.query as Record<string, unknown>
    const active = one(q.type)
    const ctx: Omit<Ctx, 'pokemon' | 'types'> = {
      active,
      join: one(q.join),
      team: readTeam(q.team),
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
    }, one(q.error)))
  })

  // Toggling a pick. STATELESS: the current team comes in on the form,
  // the new team goes back out as the same hidden inputs. Nothing is
  // stored - a team only becomes server state at open or join.
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

    let list
    try {
      list = await api.GET('/pokemon', { params: { query: active ? { type: active } : {} } })
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).send('pokedex API unavailable')
    }
    if (list.error) return reply.code(502).send('pokedex API unavailable')

    const ctx: Ctx = {
      pokemon: list.data.pokemon, types: [], active, join, team: next,
      resume: readResume(request),
    }

    // The slots, plus the grid out of band: a third pick disables every
    // card that is not picked, so the whole grid changes, not one card.
    // hx-push-url keeps the team in the address bar, which is what makes
    // a reload restore it.
    return reply
      .type('text/html')
      .header('HX-Push-Url', url({ active, join, team: next }))
      .send(teamSlots(ctx) + grid(ctx).replace('<main ', '<main hx-swap-oob="true" '))
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
      // Only the rows: the poll targets this div's contents.
      return reply.type('text/html').send(
        waitingList(data.waiting).replace(/^<div id="waiting"[^>]*>/, '').replace(/<\/div>$/, ''))
    } catch {
      return reply.code(502).send('')
    }
  })

  app.get('/battle/rename', async (_request, reply) =>
    reply.type('text/html').send(registerRow()))

  // Registering, and then doing what the trainer was trying to do.
  //
  // pokedex-web posted the name, then RE-POSTED the original request
  // from the browser after a 401. Here the pending team and join id ride
  // along with the name, so one exchange registers and commits.
  app.post('/battle/register', async (request, reply) => {
    const body = (request.body ?? {}) as Record<string, unknown>
    const name = one(body.name).trim()
    if (!name) return reply.code(400).type('text/html').send(registerRow())

    let created
    try {
      created = await api.POST('/trainers', { body: { name } })
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).type('text/html').send(registerRow())
    }
    if (created.error) {
      return reply.code(409).type('text/html').send(
        registerRow() + `<p class="sub" role="alert">that name is taken</p>`)
    }

    reply.header('set-cookie',
      `trainer=${encodeURIComponent(JSON.stringify(created.data))}; Path=/; Max-Age=604800; SameSite=Lax`)

    const team = readTeam(body.team)
    const join = one(body.join)
    if (team.length || join || one(body.commit)) {
      return commit(request, reply, created.data as Trainer, team, join)
    }
    return reply.header('HX-Refresh', 'true').code(204).send()
  })

  const commit = async (
    request: FastifyRequest, reply: FastifyReply,
    me: Trainer, team: string[], join: string,
  ) => {
    try {
      if (join) {
        const { data, error, response } = await api.POST('/battles/{id}/join', {
          params: { path: { id: join }, header: { 'X-Trainer-Token': me.token } },
          body: { team },
        })
        if (error) return rejected(reply, response.status, team, join)
        return toBattle(reply, data.id)
      }
      const { data, error, response } = await api.POST('/battles', {
        body: { team },
        params: { header: { 'X-Trainer-Token': me.token } },
      })
      if (error) return rejected(reply, response.status, team, join)
      return toBattle(reply, data.id)
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return rejected(reply, 502, team, join)
    }
  }

  // A committed team becomes a battle, and the battle id goes in a
  // cookie so the pokedex can offer a way back to it.
  const toBattle = (reply: FastifyReply, id: string) =>
    reply
      .header('set-cookie', `battle=${encodeURIComponent(id)}; Path=/; Max-Age=86400; SameSite=Lax`)
      .header('HX-Redirect', `/battle/${encodeURIComponent(id)}`)
      .code(204)
      .send()

  // A 401 means there is no trainer yet: send back the name form,
  // carrying the team so registering can finish the job.
  const rejected = (reply: FastifyReply, status: number, team: string[], join: string) => {
    if (status === 401) {
      return reply.type('text/html').send(nameDialog(team, join))
    }
    return reply
      .header('HX-Redirect', url({ join, team, }) + (url({ join, team }).includes('?') ? '&' : '?') +
        'error=' + encodeURIComponent('that did not work'))
      .code(204).send()
  }

  const openOrJoin = (join: string) => async (request: FastifyRequest, reply: FastifyReply) => {
    const me = readTrainer(request)
    const body = (request.body ?? {}) as Record<string, unknown>
    const team = readTeam(body.team)
    if (!me) return reply.type('text/html').send(nameDialog(team, join))
    return commit(request, reply, me, team, join)
  }

  app.post('/battle/open', async (request, reply) => openOrJoin('')(request, reply))

  app.post<{ Params: { id: string } }>('/battle/:id/join', async (request, reply) =>
    openOrJoin(request.params.id)(request, reply))

  app.get('/downloads', async (request, reply) => {
    const q = request.query as Record<string, unknown>
    return reply.type('text/html').send(await downloadsFooter(one(q.os), one(q.arch)))
  })

  // The battle page. The first board is rendered INTO the page, so it
  // does not flash empty before the first poll - the same reason
  // pokedex-web shipped a boot island.
  app.get<{ Params: { id: string } }>('/battle/:id', async (request, reply) => {
    const me = readTrainer(request)
    if (!me) return reply.redirect('/battle')

    const first = await boardFor(request, request.params.id, me.name, ZERO, '')
    return reply
      .header('set-cookie',
        `battle=${encodeURIComponent(request.params.id)}; Path=/; Max-Age=86400; SameSite=Lax`)
      .type('text/html')
      .send(battlePage({ id: request.params.id, trainer: me.name, first }))
  })

  // A reader with no cookie is a SPECTATOR, not an error: board()
  // renders the watching view for anyone who is not a participant, so
  // an empty name simply never matches a side.
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

  // One place that turns a battle id into a board, so the page, the poll
  // and a submitted turn all render the same thing.
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

const readTurn = (v: Record<string, unknown>): Turn => {
  const n = (x: unknown, fallback = 0) => {
    const i = Number(x)
    return Number.isInteger(i) && i >= 0 && i < 6 ? i : fallback
  }
  return { attacker: n(v.attacker), move: n(v.move), target: n(v.target) }
}

// The name dialog, returned in place of the action that needed a name.
// It carries the pending team so registering completes the original
// intent rather than dropping it.
function nameDialog(team: string[], join: string): string {
  const carried = team.map((t) => `<input type="hidden" name="team" value="${esc(t)}">`).join('')
  return `<div class="modal-backdrop" id="name-dialog" role="dialog" aria-modal="true"
       aria-label="Pick a trainer name">
  <form class="modal" hx-post="/battle/register" hx-target="#name-dialog" hx-swap="outerHTML">
    ${carried}<input type="hidden" name="join" value="${esc(join)}">
    <input type="hidden" name="commit" value="1">
    <h2>Pick a trainer name</h2>
    <p>Other trainers see this in the lobby.</p>
    <input id="trainer-name" name="name" aria-label="Trainer name" maxlength="32"
           placeholder="e.g. Ash" autocomplete="off" autofocus required>
    <div class="modal-actions">
      <!-- Dismissing a dialog changes no server data, so it is not a
           hypermedia exchange: removing the node is the whole action. -->
      <button type="button" class="ghost"
              onclick="this.closest('#name-dialog').remove()">cancel</button>
      <button type="submit">start</button>
    </div>
  </form>
</div>`
}
