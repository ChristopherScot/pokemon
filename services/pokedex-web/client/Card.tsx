// One Pokemon in the grid.
//
// A button, not an <article> with a click handler. The card IS the team
// picker, and as a div the whole product was mouse-only: you could
// browse and filter and then not pick a team.
import type { components } from '@christopherscot/pokedex-client'

import { colour } from '../types.ts'
import type { Pick } from './useTeam.ts'

type Pokemon = components['schemas']['Pokemon']

export function Card({
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
