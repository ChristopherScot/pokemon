import { readFile } from 'node:fs/promises'
import { join } from 'node:path'

import Fastify, { LogController } from 'fastify'

import { ASSET_DIR } from './assets.ts'
import { register as registerPokedex } from './pokedex.ts'
import { registerBattle } from './battle.ts'
import { collectDefaultMetrics, Counter, Histogram, register } from 'prom-client'

const port: number = Number(process.env.PORT || '3000')

const app = Fastify({
  logger: {
    level: process.env.LOG_LEVEL || 'info',
    base: {
      service: 'pokedex-web',
      team: 'me-myself-and-i',
      version: process.env.VERSION || 'dev',
    },
    timestamp: () => `,"time":"${new Date().toISOString()}"`,
    formatters: {
      level: (label: string) => ({ level: label.toUpperCase() }),
    },
    hooks: {
      logMethod(this: unknown, args: unknown[], method: (...a: unknown[]) => void) {
        if (args[0] === '') return
        return method.apply(this, args)
      },
    },
  },
  logController: new LogController({ disableRequestLogging: true }),
})

collectDefaultMetrics()

const requests = new Counter({
  name: 'http_requests_total',
  help: 'Requests by route, method and status.',
  labelNames: ['route', 'method', 'status'],
})

const latency = new Histogram({
  name: 'http_request_duration_seconds',
  help: 'Request latency by route and method.',
  labelNames: ['route', 'method'],
})

app.addHook('onResponse', (request, reply, done) => {
  const route = request.routeOptions?.url ?? 'other'

  if (route !== '/metrics' && route !== '/healthz') {
    const seconds = reply.elapsedTime / 1000
    requests.inc({ route, method: request.method, status: reply.statusCode })
    latency.observe({ route, method: request.method }, seconds)
    request.log.info({
      method: request.method,
      route,
      status: reply.statusCode,
      duration_ms: reply.elapsedTime,
    }, 'request')
  }
  done()
})

app.get<{ Params: { '*': string } }>('/assets/*', async (request, reply) => {
  const rel = request.params['*']
  // No traversal: the only legal shape here is one generated filename.
  if (!/^[A-Za-z0-9._-]+$/.test(rel)) return reply.code(404).send()

  const type = rel.endsWith('.js') ? 'text/javascript'
    : rel.endsWith('.css') ? 'text/css'
    : 'application/octet-stream'
  try {
    const body = await readFile(join(ASSET_DIR, rel))
    return reply
      .type(type)
      .header('cache-control', 'public, max-age=31536000, immutable')
      .send(body)
  } catch {
    return reply.code(404).send()
  }
})

app.get('/healthz', async (_request, reply) => reply.type('text/plain').send('ok\n'))

app.get('/metrics', async (_request, reply) =>
  reply.type(register.contentType).send(await register.metrics()))

// The card UI lives in pokedex.ts; this file stays the template's.
registerPokedex(app)

registerBattle(app)

for (const sig of ['SIGTERM', 'SIGINT'] as const) {
  process.on(sig, async () => {
    app.log.info('shutting down')
    await app.close()
    process.exit(0)
  })
}

export { app }

async function start(): Promise<void> {
  try {
    await app.listen({ port, host: '0.0.0.0', listenTextResolver: () => '' })
    app.log.info({ addr: ':' + port }, 'started')
  } catch (err) {
    app.log.error({ err }, 'server stopped')
    process.exit(1)
  }
}

const entry = process.argv[1] ?? ''
if (entry.endsWith('server.ts') || entry.endsWith('server.js')) {
  await start()
}
