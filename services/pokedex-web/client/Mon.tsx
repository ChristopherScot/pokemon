// One Pokemon on the board.
//
// A button when it can be chosen, a plain div when it cannot - which is
// how a keyboard reaches it and how a screen reader is told it is
// pressable. The old markup was a div with a delegated click handler,
// so the board was mouse-only.
import type { components } from '@christopherscot/pokedex-client'

import { colour } from '../types.ts'

type Mon = components['schemas']['BattlePokemon']

function hpColour(hp: number, max: number): string {
  const f = max > 0 ? hp / max : 0
  return f <= 0.2 ? 'var(--danger)' : f <= 0.5 ? 'var(--warn)' : 'var(--good)'
}

export function MonView({
  mon, side, index, selectable, selected, onPick,
}: {
  mon: Mon
  side: 'me' | 'them'
  index: number
  selectable: boolean
  selected: boolean
  onPick: (i: number) => void
}) {
  const pct = mon.maxHp > 0 ? Math.max(0, (mon.hp / mon.maxHp) * 100) : 0

  const inner = (
    <>
      <img src={mon.sprite} alt="" loading="lazy" />
      <div>
        <div className="name">{mon.name}</div>
        <div className="types">
          {mon.types.map((t) => (
            <span key={t} className="type" style={{ background: colour(t) }}>{t}</span>
          ))}
        </div>
        <div className="hpwrap">
          {/* The width transition animates from the previous value, so
              the drain is visible rather than already-drained. */}
          <div
            className="hp"
            style={{ width: `${pct}%`, background: hpColour(mon.hp, mon.maxHp) }}
          />
        </div>
      </div>
      <div className="hpnum">{mon.hp}/{mon.maxHp}</div>
    </>
  )

  const className = 'mon' + (mon.fainted ? ' fainted' : '')
  const slot = `${side}-${index}`

  if (!selectable) {
    return <div className={className} data-slot={slot}>{inner}</div>
  }
  return (
    <button
      type="button"
      className={className}
      data-slot={slot}
      aria-pressed={selected}
      onClick={() => onPick(index)}
      style={selected ? { outline: '2px solid #6d5ae0' } : undefined}
    >
      {inner}
    </button>
  )
}
