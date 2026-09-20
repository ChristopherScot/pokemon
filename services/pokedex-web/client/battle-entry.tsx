// The battle page's entry point.
//
// Reads what the server knew from a JSON island rather than having
// values interpolated into the code, which is what lets every file
// here be ordinary TypeScript the compiler checks.
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { BattlePage } from './BattlePage.tsx'

const boot = JSON.parse(
  document.getElementById('boot')?.textContent || '{}',
) as { id: string; me: string }

const root = document.getElementById('root')
if (root) {
  createRoot(root).render(
    <StrictMode>
      <BattlePage id={boot.id} me={boot.me} />
    </StrictMode>,
  )
}
