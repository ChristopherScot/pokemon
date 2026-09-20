// The lobby page's entry point.
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { Lobby, type Trainer } from './Lobby.tsx'

import type { components } from '@christopherscot/pokedex-client'

const boot = JSON.parse(
  document.getElementById('boot')?.textContent || '{}',
) as { me: Trainer | null; waiting: components['schemas']['WaitingBattle'][] }

const root = document.getElementById('root')
if (root) {
  createRoot(root).render(
    <StrictMode>
      <Lobby me={boot.me} initial={boot.waiting ?? []} />
    </StrictMode>,
  )
}
