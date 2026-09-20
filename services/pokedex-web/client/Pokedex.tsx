// The pokedex: browse, filter, and pick a team.
//
// The card grid IS the team picker. That is why every card is a real
// button - as an <article> with a delegated click handler the whole
// product was mouse-only, and picking a team is the product.
import { useMemo, useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { colour } from '../types.ts'
import { TEAM_SIZE } from './shared.ts'
import { useTeam, type Pick } from './useTeam.ts'

type Pokemon = components['schemas']['Pokemon']

export type TypeSummary = { name: string; count: number }

export function Pokedex({
  pokemon, types, active, join, resume,
}: {
  pokemon: Pokemon[]
  types: TypeSummary[]
  /** The type currently filtered on, or "" for all. */
  active: string
  /** A battle id when this page is "pick a team, then join THAT". */
  join: string
  /** A battle already in progress, so leaving is not a one-way trip. */
  resume: string
}) {
  const { team, toggle, removeAt, clear } = useTeam()
  const [query, setQuery] = useState('')

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? pokemon.filter((p) => p.name.toLowerCase().includes(q)) : pokemon
  }, [pokemon, query])

  const picked = useMemo(() => new Set(team.map((t) => t.name)), [team])

  return (
    <>
      <header className="topbar">
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
    </>
  )
}

function href(query: string, join: string): string {
  const parts = [query, join ? `join=${encodeURIComponent(join)}` : ''].filter(Boolean)
  return parts.length ? `/?${parts.join('&')}` : '/'
}

function Card({
  mon, picked, full, onToggle,
}: {
  mon: Pokemon
  picked: boolean
  full: boolean
  onToggle: (p: Pick) => void
}) {
  return (
    <button
      type="button"
      className={'card' + (picked ? ' picked' : '')}
      // The selected state has to reach the accessibility tree: the
      // "on your team" pill is CSS ::after content and announces to
      // nobody.
      aria-pressed={picked}
      // A full team still lets you deselect, or you could trap
      // yourself with three picks and no way back.
      disabled={full && !picked}
      onClick={() => onToggle({ name: mon.name, sprite: mon.sprite })}
    >
      <header>
        <span className="num">#{String(mon.id).padStart(3, '0')}</span>
        <h2>{mon.name}</h2>
      </header>
      <img src={mon.sprite} alt="" loading="lazy" width={96} height={96} />
      <div className="types">
        {mon.types.map((t) => (
          <span key={t} className="type" style={{ background: colour(t) }}>{t}</span>
        ))}
      </div>
      <dl>
        <dt>height</dt><dd>{(mon.height / 10).toFixed(1)} m</dd>
        <dt>weight</dt><dd>{(mon.weight / 10).toFixed(1)} kg</dd>
      </dl>
      <ul className="moves">
        {mon.moves.map((m) => (
          <li key={m.name}>
            <span>{m.name}</span>
            <span className="move-type" style={{ color: colour(m.type) }}>{m.type}</span>
            {/* Status moves have power 0, which reads as a bug rather
                than "deals no damage" - a dash, as the CLI prints. */}
            <b>{m.power || '—'}</b>
          </li>
        ))}
      </ul>
    </button>
  )
}

function TeamSlots({ team, onRemove }: { team: Pick[]; onRemove: (i: number) => void }) {
  return (
    <div className="team-slots" id="team">
      {Array.from({ length: TEAM_SIZE }, (_, i) => {
        const pick = team[i]
        if (!pick) {
          // A die, not a slot number: an empty slot is filled randomly
          // by the server, and saying so is what makes "just start a
          // battle" a visible option rather than a hidden one.
          return (
            <div className="slot" key={i} title="random">
              <span className="slot-empty">🎲</span>
            </div>
          )
        }
        return (
          <div className="slot filled" key={i}>
            <img src={pick.sprite} alt={pick.name} />
            <button
              type="button"
              className="remove"
              aria-label={`remove ${pick.name} from your team`}
              onClick={() => onRemove(i)}
            >×</button>
          </div>
        )
      })}
    </div>
  )
}

function Ready({
  team, join, onCommitted,
}: {
  team: Pick[]
  join: string
  onCommitted: () => void
}) {
  const [busy, setBusy] = useState(false)
  const [needsName, setNeedsName] = useState(false)
  const [error, setError] = useState('')

  /**
   * Commits the team: joins `join` if set, otherwise opens a new
   * battle. One function for both, because picking a team is the same
   * act either way - splitting them is what left joining without a
   * picker at all.
   */
  async function commit(): Promise<boolean> {
    const body = team.length ? { team: team.map((t) => t.name) } : {}
    const url = join ? `/battle/${encodeURIComponent(join)}/join` : '/battle/open'
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    })
    const out = await res.json().catch(() => ({}))
    if (res.ok) {
      const id = join || String(out.id)
      onCommitted()
      try { sessionStorage.setItem('pokedex.battle', id) } catch { /* private mode */ }
      location.href = `/battle/${id}`
      return true
    }
    // 401 is the only recoverable one: there is no trainer yet.
    if (res.status === 401) return false
    throw new Error(String(out.message ?? 'that did not work'))
  }

  return (
    <>
      <button
        id="ready"
        type="button"
        data-join={join}
        disabled={busy}
        onClick={async () => {
          setBusy(true)
          setError('')
          try {
            if (await commit()) return
            // Ask for the name in place rather than redirecting to the
            // lobby, which would throw away the team just picked.
            setNeedsName(true)
          } catch (err) {
            setError(err instanceof Error ? err.message : 'that did not work')
          } finally {
            setBusy(false)
          }
        }}
      >
        {busy ? (join ? 'joining…' : 'opening…') : join ? 'Join battle' : 'Ready to battle'}
      </button>
      {error && <p className="sub" role="alert">{error}</p>}
      {needsName && (
        <NameDialog
          onClose={() => setNeedsName(false)}
          onRegistered={async () => {
            setNeedsName(false)
            try { await commit() } catch (err) {
              setError(err instanceof Error ? err.message : 'that did not work')
            }
          }}
        />
      )}
    </>
  )
}

function NameDialog({
  onClose, onRegistered,
}: {
  onClose: () => void
  onRegistered: () => void
}) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    const res = await fetch('/battle/register', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name: trimmed }),
    })
    if (res.ok) { onRegistered(); return }
    const body = await res.json().catch(() => ({}))
    setError(String(body.message ?? 'that name is taken'))
  }

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label="Pick a trainer name">
      <form className="modal" onSubmit={submit}>
        <h2>Pick a trainer name</h2>
        <p>Other trainers see this in the lobby.</p>
        <input
          id="trainer-name"
          aria-label="Trainer name"
          maxLength={32}
          placeholder="e.g. Ash"
          autoComplete="off"
          autoFocus
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        {error && <p className="modal-error" role="alert">{error}</p>}
        <div className="modal-actions">
          <button type="button" onClick={onClose}>cancel</button>
          <button type="submit">start</button>
        </div>
      </form>
    </div>
  )
}
