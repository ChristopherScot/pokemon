import {
  createClient,
  exponentialRetry,
  noRetry,
} from '@christopherscot/pokedex-client'

/** The pokedex client this service calls with.
 *
 *  A factory rather than a ready-made export: when a breaker is
 *  configured, its consecutive-failure count only means anything if
 *  every caller shares one instance. Built at the entry point and
 *  passed down, so a test can hand in its own.
 *
 *  Nothing passes a different one yet: ClientOptions has no fetch
 *  field, so a fake cannot serve canned data (oapi-clientgen#11).
 *  The seam is here for when it can. */
export function pokedexClient() {
  return createClient({
    baseUrl: process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local',
    name: 'pokedex-htmx',
    policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
  })
}

export type PokedexClient = ReturnType<typeof pokedexClient>
