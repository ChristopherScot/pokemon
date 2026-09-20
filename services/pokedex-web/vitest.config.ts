// Tests run through Vite, which is what makes .tsx work.
//
// They used to run on `node --test`, because Node strips types and the
// service had no build step. Node does not compile JSX, so React ended
// that: a .tsx file is ERR_UNKNOWN_FILE_EXTENSION to it. Vitest is the
// standard answer and shares this repo's Vite config, so the code under
// test is transformed the same way the shipped bundle is.
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  test: {
    // Server tests want Node; component tests want a DOM. The
    // environment is chosen per file by a docblock comment, so neither
    // pays for the other.
    environment: 'node',
    // The API is deliberately unreachable in tests - POKEDEX_URL points
    // at a dead port - and the retrying client would spend 30s per call
    // discovering that.
    env: { POKEDEX_NO_RETRY: '1' },
    // The "API unreachable" tests wait out a real connection failure,
    // which is the point of them - they prove the 502 path rather than
    // mocking it. That takes longer than the 5s default.
    testTimeout: 30_000,
    setupFiles: ['./vitest.setup.ts'],
    include: ['**/*.test.ts', '**/*.test.tsx'],
    exclude: ['node_modules/**', 'dist/**'],
  },
})
