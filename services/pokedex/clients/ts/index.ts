// The package entry: generated operations plus the transport that
// carries the retries, timeout and circuit breaker.
//
//   import { createClient, listThings } from '@christopherscot/pokedex-client'
//   const client = createClient({ baseUrl: 'https://pokedex' })
//   const { data, error } = await listThings({ client })
//
// Hand-written, unlike everything it re-exports: the generator owns
// generated/ and clears it on every run, and this file decides what
// the package's surface is.
export * from './generated/sdk.gen.ts'
export * from './generated/types.gen.ts'
export {
  Breaker,
  CircuitOpenError,
  ClientNameHeader,
  ClientVersion,
  ClientVersionHeader,
  createClient,
  exponentialRetry,
  noRetry,
  retryAfterMs,
  singleRetry,
} from './resilience.ts'
export type { ClientOptions, RetryPolicy } from './resilience.ts'
