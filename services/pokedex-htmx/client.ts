import {
  createClient,
  exponentialRetry,
  noRetry,
} from '@christopherscot/pokedex-client'

/** A factory, not a ready-made export: a module-level client reads
 *  process.env when the import graph evaluates, fixing the retry
 *  policy before anything setting POKEDEX_NO_RETRY afterwards can
 *  take effect. A test builds its own and passes it to the route
 *  registration rather than reaching for this one. */
export function pokedexClient() {
  return createClient({
    baseUrl: process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local',
    name: 'pokedex-htmx',
    policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
  })
}

export type PokedexClient = ReturnType<typeof pokedexClient>
