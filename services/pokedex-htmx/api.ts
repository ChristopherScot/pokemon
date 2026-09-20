import createClient, { exponentialRetry, noRetry } from '@christopherscot/pokedex-client'

const baseUrl = process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local'

export const api = createClient({
  baseUrl,
  policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
})
