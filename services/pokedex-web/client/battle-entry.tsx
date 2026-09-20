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
