// Typed client for the pokedex API, generated from the same
// openapi.yml the Go server is generated from. Defaults match the Go
// client in api/client.go: 5s timeout, one retry (five can mean five
// times the traffic to something already struggling), and a breaker -
// both clients failing the same way is the point.
//
//   import createClient from '@christopherscot/pokedex-client'
//   const client = createClient({ baseUrl: 'https://pokedex' })
//   const { data, error } = await client.GET('/')
import createFetchClient from 'openapi-fetch'

// Read from package.json rather than hardcoded: the header cannot then
// disagree with the version a consumer installed.
import pkg from './package.json' with { type: 'json' }

export const ClientVersion = pkg.version
export const ClientVersionHeader = 'Client-Version'

// Set via createClient({ name: 'my-service' }); No X- prefix on either:
// RFC 6648 deprecated it in 2012.
export const ClientNameHeader = 'Client-Name'

// A POST may already have applied, so retrying it can create a second thing.
const IDEMPOTENT = new Set(['GET', 'HEAD', 'OPTIONS', 'PUT', 'DELETE'])

function retryable(req, res, err) {
  if (!IDEMPOTENT.has(req.method)) return false
  if (err !== undefined) return true
  return res !== undefined && (res.status === 429 || res.status >= 500)
}

// One retry, a second later. The default.
export const singleRetry = {
  backoffs: () => [1000],
  retry: retryable,
}

// Five jittered attempts, for a slow-but-not-broken dependency. Jitter
// so a fleet of callers does not retry in lockstep.
export const exponentialRetry = {
  backoffs: () => {
    const out = []
    let next = 100
    for (let i = 0; i < 5; i++) {
      out.push(next + (Math.random() * 2 - 1) * 0.25 * next)
      next *= 2
    }
    return out
  },
  retry: retryable,
}

export const noRetry = { backoffs: () => [], retry: () => false }

// Reads the server's own answer to "when should I come back". RFC 9110
// allows either a delay in seconds or an HTTP-date. Capped so an
// hour-long value cannot masquerade as a hang.
export function retryAfterMs(res, maxMs) {
  const v = res?.headers?.get('retry-after')
  if (!v) return undefined

  let ms
  const secs = Number(v.trim())
  if (Number.isInteger(secs)) {
    if (secs < 0) return undefined
    ms = secs * 1000
  } else {
    const when = Date.parse(v)
    if (Number.isNaN(when)) return undefined
    ms = Math.max(0, when - Date.now())
  }
  return ms > maxMs ? undefined : ms
}

export class CircuitOpenError extends Error {
  constructor() {
    super('circuit open')
    this.name = 'CircuitOpenError'
  }
}

export class Breaker {
  #failures = 0
  #openedAt = 0

  constructor(threshold = 5, cooldownMs = 30_000) {
    this.threshold = threshold
    this.cooldownMs = cooldownMs
  }

  allow() {
    if (this.#failures < this.threshold) return true
    if (Date.now() - this.#openedAt > this.cooldownMs) {
      this.#failures = 0 // half-open: one probe decides whether to close
      return true
    }
    return false
  }

  record(failed) {
    if (!failed) {
      this.#failures = 0
      return
    }
    this.#failures++
    if (this.#failures === this.threshold) this.#openedAt = Date.now()
  }
}

/**
 * @param {object} options
 * @param {string} options.baseUrl
 * @param {number} [options.timeoutMs=5000] bounds a single attempt.
 * @param {object} [options.policy=singleRetry]
 * @param {Breaker} [options.breaker] omit to disable circuit breaking.
 */
export default function createClient({
  baseUrl,
  // CALLER's own service name, sent as Client-Name.
  name,
  timeoutMs = 5000,
  policy = singleRetry,
  breaker,
  maxRetryAfterMs = 30_000,
}) {
  const resilientFetch = async (input, init) => {
    const req = new Request(input, init)
    req.headers.set(ClientVersionHeader, ClientVersion)
    if (name) req.headers.set(ClientNameHeader, name)

    if (breaker && !breaker.allow()) throw new CircuitOpenError()

    const backoffs = policy.backoffs()
    let res
    let err

    for (let attempt = 0; ; attempt++) {
      err = undefined
      try {
        res = await fetch(req.clone(), { signal: AbortSignal.timeout(timeoutMs) })
      } catch (e) {
        err = e
        res = undefined
      }
      if (attempt >= backoffs.length || !policy.retry(req, res, err)) break

      // The server's Retry-After wins over the policy's backoff.
      const after = retryAfterMs(res, maxRetryAfterMs)
      const wait = after ?? backoffs[attempt]
      await new Promise((r) => setTimeout(r, wait))
    }

    breaker?.record(err !== undefined || (res !== undefined && res.status >= 500))
    if (err !== undefined) throw err
    return res
  }

  return createFetchClient({ baseUrl, fetch: resilientFetch })
}
