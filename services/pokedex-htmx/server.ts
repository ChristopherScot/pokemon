import formbody from '@fastify/formbody'
import Fastify, { LogController } from 'fastify'
import { collectDefaultMetrics, Counter, Histogram, register } from 'prom-client'

import { register as registerRoutes } from './routes.ts'

// Repeated fields become arrays: `team=a&team=b` is the team.
const qs = (body: string): Record<string, string | string[]> => {
  const out: Record<string, string | string[]> = {}
  for (const [k, v] of new URLSearchParams(body)) {
    const seen = out[k]
    if (seen === undefined) out[k] = v
    else if (Array.isArray(seen)) seen.push(v)
    else out[k] = [seen, v]
  }
  return out
}

const port: number = Number(process.env.PORT || '3000')

// Matches go-service's slog shape (ISO time, uppercase level, msg) so one Loki
// query works across both runtimes. VERSION is CI-stamped; "dev" locally.
const app = Fastify({
  logger: {
    level: process.env.LOG_LEVEL || 'info',
    base: {
      service: 'pokedex-htmx',
      team: 'me-myself-and-i',
      version: process.env.VERSION || 'dev',
    },
    timestamp: () => `,"time":"${new Date().toISOString()}"`,
    formatters: {
      level: (label: string) => ({ level: label.toUpperCase() }),
    },
    // Drop the empty lines listenTextResolver silences below.
    hooks: {
      logMethod(this: unknown, args: unknown[], method: (...a: unknown[]) => void) {
        if (args[0] === '') return
        return method.apply(this, args)
      },
    },
  },
  // Fastify's per-request lines duplicate the onResponse hook and log the raw
  // url. Top-level disableRequestLogging is deprecated in Fastify 5.
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

// Read at startup from MIN_VERSION so raising the floor is a config edit and
// a restart, not a rebuild. Empty means no floor (the normal state).
export const floor = { minVersion: process.env.MIN_VERSION ?? '' }

// Clients send bare semver; config.yaml may write a leading v. Strip it so
// one spelling reaches the comparison.
function below(got: string, floor: string): boolean {
  const parts = (v: string) => v.replace(/^v/, '').split('.').map(Number)
  const a = parts(got)
  const b = parts(floor)
  if (a.length !== 3 || b.length !== 3 || [...a, ...b].some(Number.isNaN)) {
    return false // unparseable: not this check's place to judge
  }
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] < b[i]
  }
  return false
}

// 410 Gone stops generated clients dead; 429 or 5xx would be retried and
// escalate one stuck client into a storm. Fails open on missing/unparseable
// versions (kubelet, Alloy, curl) and skips probes/metrics outright.
app.addHook('onRequest', (request, reply, done) => {
  const route = request.routeOptions?.url ?? 'other'
  if (!floor.minVersion || route === '/metrics' || route === '/healthz') {
    return done()
  }
  const got = request.headers['client-version']
  if (typeof got !== 'string' || !got || !below(got, floor.minVersion)) {
    return done()
  }
  request.log.warn({
    route,
    client: request.headers['client-name'] ?? 'unknown',
    client_version: got,
    min_version: floor.minVersion,
  }, 'refusing a client below the floor')
  reply.code(410).send({ message: 'this client is too old; reload or upgrade' })
})

// Route label is the matched pattern, never the raw url: paths carry tokens
// and ids, so raw urls would leak into logs and blow up Prometheus cardinality.
// Unrouted requests report "other" for the same reason.
app.addHook('onResponse', (request, reply, done) => {
  const route = request.routeOptions?.url ?? 'other'

  // /metrics and /healthz are self-observation; logging them buries traffic.
  if (route !== '/metrics' && route !== '/healthz') {
    const seconds = reply.elapsedTime / 1000
    requests.inc({ route, method: request.method, status: reply.statusCode })
    latency.observe({ route, method: request.method }, seconds)
    request.log.info({
      method: request.method,
      route,
      status: reply.statusCode,
      // Not rounded: sub-1ms handlers would round to 0 on every line.
      duration_ms: reply.elapsedTime,
    }, 'request')
  }
  done()
})

app.get('/healthz', async (_request, reply) => reply.type('text/plain').send('ok\n'))

app.get('/metrics', async (_request, reply) =>
  reply.type(register.contentType).send(await register.metrics()))

// Fastify does not parse urlencoded by default; qs turns repeated `team`
// fields into an array, which is what makes the team a form value.
app.register(formbody, { parser: (str) => qs(str) })

registerRoutes(app)

// Drain in-flight requests within Kubernetes' terminationGracePeriod so a
// rollout is invisible to callers.
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
    // Empty listenTextResolver silences Fastify's per-address listen lines
    // (0.0.0.0 expands to every interface); logger drops empty messages.
    await app.listen({ port, host: '0.0.0.0', listenTextResolver: () => '' })
    app.log.info({ addr: ':' + port }, 'started')
  } catch (err) {
    app.log.error({ err }, 'server stopped')
    process.exit(1)
  }
}

// Skip start() when imported from a test. Both extensions matched: source
// runs as .ts locally, Vite bundles to .js in the image.
const entry = process.argv[1] ?? ''
if (entry.endsWith('server.ts') || entry.endsWith('server.js')) {
  await start()
}
