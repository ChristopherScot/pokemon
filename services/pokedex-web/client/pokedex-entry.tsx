// The pokedex page's entry point.
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import type { components } from '@christopherscot/pokedex-client'

import { Pokedex } from './Pokedex.tsx'

const boot = JSON.parse(
  document.getElementById('boot')?.textContent || '{}',
) as {
  pokemon: components['schemas']['Pokemon'][]
  types: { name: string; count: number }[]
  active: string
  join: string
}

let resume = ''
try { resume = sessionStorage.getItem('pokedex.battle') ?? '' } catch { /* private mode */ }

const root = document.getElementById('root')
if (root) {
  createRoot(root).render(
    <StrictMode>
      <Pokedex
        pokemon={boot.pokemon ?? []}
        types={boot.types ?? []}
        active={boot.active ?? ''}
        join={boot.join ?? ''}
        resume={resume}
      />
    </StrictMode>,
  )
}
