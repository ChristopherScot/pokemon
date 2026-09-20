import { useMemo, useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { colour } from '../types.ts'
import { Card } from './Card.tsx'
import { Downloads } from './Downloads.tsx'
import { Ready } from './Ready.tsx'
import { TeamSlots } from './TeamSlots.tsx'
import { useTopbarHeight } from './useTopbarHeight.ts'
import { TEAM_SIZE } from './shared.ts'
import { useTeam } from './useTeam.ts'

type Pokemon = components['schemas']['Pokemon']

export type TypeSummary = { name: string; count: number }

export function Pokedex({
  pokemon, types, active, join, resume,
}: {
  pokemon: Pokemon[]
  types: TypeSummary[]
  active: string
  join: string
  resume: string
}) {
  const { team, toggle, removeAt, clear } = useTeam()
  const [query, setQuery] = useState('')
  const topbar = useTopbarHeight()

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? pokemon.filter((p) => p.name.toLowerCase().includes(q)) : pokemon
  }, [pokemon, query])

  const picked = useMemo(() => new Set(team.map((t) => t.name)), [team])

  return (
    <>
      <header className="topbar" ref={topbar}>
        <div className="title">
          <h1>{join ? 'Pick your team' : 'Pokedex'}</h1>
          <p className="sub">
            {join
              ? <>joining battle <code>{join}</code> · pick up to three, or none for a random team · <a className="battle-link" href="/battle">back to the lobby</a></>
              : <>{pokemon.length} pokemon · <a className="battle-link" href="/battle">Battle lobby →</a></>}
          </p>
          {/* Wandering off to the pokedex mid-battle is normal - you
              want to check what a move does. Until this, the only way
              back was the browser's back button. */}
          {resume && resume !== join && (
            <p className="sub">
              you are in battle <code>{resume}</code> ·{' '}
              <a className="battle-link" href={`/battle/${encodeURIComponent(resume)}`}>back to it →</a>
            </p>
          )}
        </div>

        <input
          id="search"
          type="search"
          aria-label="Search pokemon by name"
          placeholder="Search pokemon..."
          autoComplete="off"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />

        <TeamSlots team={team} onRemove={removeAt} />
        <Ready team={team} join={join} onCommitted={clear} />
      </header>

      {/* Type filters stay LINKS rather than becoming client state.
          The server filters by type, so each one is a real URL you can
          share or bookmark - and the join id rides along, or filtering
          to "water" mid-join would quietly turn a join into an open. */}
      <nav className="filters">
        <a href={href('', join)} className={active ? undefined : 'on'}>all</a>
        {types.map((t) => (
          <a
            key={t.name}
            href={href(`type=${encodeURIComponent(t.name)}`, join)}
            className={active === t.name ? 'on' : undefined}
            style={{ borderColor: colour(t.name) }}
          >
            {t.name} <small>{t.count}</small>
          </a>
        ))}
      </nav>

      {/* Announced, not just visible: filtering used to change the grid
          silently, so a screen-reader user had no idea how many were
          left. */}
      <p className="sr-only" role="status">
        {query ? `${shown.length} pokemon match ${query}` : ''}
      </p>

      <main className="grid">
        {shown.map((p) => (
          <Card
            key={p.id}
            mon={p}
            picked={picked.has(p.name)}
            full={team.length >= TEAM_SIZE}
            onToggle={toggle}
          />
        ))}
        {shown.length === 0 && <p className="empty">nothing matches “{query}”.</p>}
      </main>

      <Downloads />
    </>
  )
}

function href(query: string, join: string): string {
  const parts = [query, join ? `join=${encodeURIComponent(join)}` : ''].filter(Boolean)
  return parts.length ? `/?${parts.join('&')}` : '/'
}
