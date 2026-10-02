import type { Client } from 'openapi-fetch'
import type { paths } from './schema.js'

// Re-exported so consumers can name the types the API is made of.
export type { paths, components, operations } from './schema.js'

/** The installed package version, which tracks the spec. */
export declare const ClientVersion: string
export declare const ClientVersionHeader: string
export declare const ClientNameHeader: string

export interface RetryPolicy {
  backoffs(): number[]
  retry(req: Request, res: Response | undefined, err: unknown): boolean
}

export declare const singleRetry: RetryPolicy
export declare const exponentialRetry: RetryPolicy
export declare const noRetry: RetryPolicy

export declare class CircuitOpenError extends Error {}

export declare class Breaker {
  constructor(threshold?: number, cooldownMs?: number)
  readonly threshold: number
  readonly cooldownMs: number
  allow(): boolean
  record(failed: boolean): void
}

/** Reads Retry-After in milliseconds; undefined when absent, unparseable
 *  or further away than maxMs. */
export declare function retryAfterMs(res: Response | undefined, maxMs: number): number | undefined

export interface ClientOptions {
  baseUrl: string
  /** Bounds a single attempt. Default 5000; a retry gets its own. */
  timeoutMs?: number
  /** Default singleRetry. */
  policy?: RetryPolicy
  /** Omit to disable circuit breaking. */
  breaker?: Breaker
  /** Bounds Retry-After. Default 30000. */
  maxRetryAfterMs?: number
}

/** Typed client; calling an endpoint the spec does not describe is a
 *  compile error. */
export default function createClient(options: ClientOptions): Client<paths>
