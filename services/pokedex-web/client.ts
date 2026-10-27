import {
  createClient,
  exponentialRetry,
  noRetry,
} from '@christopherscot/pokedex-client'

/** A factory, not a ready-made export: a module-level client reads
 *  process.env when the import graph evaluates, fixing the retry
 *  policy before anything setting POKEDEX_NO_RETRY afterwards can
 *  take effect. server.test.ts builds its own and hands it to
 *  register, which is what the argument is for. */
export function pokedexClient() {
  return createClient({
    baseUrl: process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local',
    name: 'pokedex-web',
    policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
  })
}

export type PokedexClient = ReturnType<typeof pokedexClient>
