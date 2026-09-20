import { useEffect } from 'react'

import { BattleBoard } from './Battle.tsx'
import { useBattle } from './useBattle.ts'

export function BattlePage({ id, me }: { id: string; me: string }) {
  const state = useBattle(id)

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
