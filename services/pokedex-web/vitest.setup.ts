// jest-dom's matchers: toBeDisabled, toHaveTextContent and friends.
// They read better than poking at attributes, and they are what a React
// codebase normally uses.
import '@testing-library/jest-dom/vitest'

// Unmount between tests. Without this every render stacks up in the
// same document, so a getByRole that should find one banner finds
// several and fails - passing in isolation and failing in the file,
// which is the most confusing way for a test to be wrong.
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(cleanup)
