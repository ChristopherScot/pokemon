import {
  createClient,
  exponentialRetry,
  noRetry,
} from '@christopherscot/pokedex-client'

/** The pokedex client this service calls with.
 *
 *  Built once and passed to whoever needs it rather than exported
 *  ready-made: the client holds a connection pool and, when one is
 *  configured, a circuit breaker whose failure count only means
 *  anything if every caller shares the instance. A module-level
 *  client would also leak that state between tests. */
export function pokedexClient() {
  return createClient({
    baseUrl: process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local',
    name: 'pokedex-web',
    policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
  })
}

export type PokedexClient = ReturnType<typeof pokedexClient>
