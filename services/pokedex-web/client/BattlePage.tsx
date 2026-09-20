// The battle page: poll, then draw whatever came back.
//
// The three states are the poll's, not the battle's - loading, a
// battle, or stopped-with-a-reason. Stopped is its own state because a
// player left on a spinner with no explanation was a real complaint:
// the loop had given up and nothing said so.
import { useEffect } from 'react'

import { BattleBoard } from './Battle.tsx'
import { useBattle } from './useBattle.ts'

export function BattlePage({ id, me }: { id: string; me: string }) {
  const state = useBattle(id)

  // Remember the battle we are in, so the pokedex can offer a way back.
  // Set on arrival rather than only when joining, because landing here
  // from a link someone sent is the same situation.
  useEffect(() => {
    try { sessionStorage.setItem('pokedex.battle', id) } catch { /* private mode */ }
  }, [id])

  if (state.kind === 'loading') return <div className="board">loading…</div>
  if (state.kind === 'stopped') {
    return (
      <div className="board">
        <div className="banner over" role="status">{state.message}</div>
        <p className="sub"><a href="/battle">back to the lobby</a></p>
      </div>
    )
  }

  const won = state.battle.status === 'finished' && state.battle.winner === me
  return (
    <div className={'board' + (won ? ' won' : '')}>
      <BattleBoard battle={state.battle} me={me} />
    </div>
  )
}
