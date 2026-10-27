import {
  createClient,
  exponentialRetry,
  noRetry,
} from '@christopherscot/pokedex-client'

/** The pokedex client this service calls with.
 *
 *  A factory rather than a ready-made export: the client holds a
 *  connection pool and, when one is configured, a circuit breaker
 *  whose failure count only means anything if every caller shares the
 *  instance. Built once at the entry point and passed down, so a test
 *  can hand in its own rather than mocking this module's path. */
export function pokedexClient() {
  return createClient({
    baseUrl: process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local',
    name: 'pokedex-htmx',
    policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
  })
}

export type PokedexClient = ReturnType<typeof pokedexClient>
