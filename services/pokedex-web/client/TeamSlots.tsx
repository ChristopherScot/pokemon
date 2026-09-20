// The three slots above the grid, showing what you have picked.
import { TEAM_SIZE } from './shared.ts'
import type { Pick } from './useTeam.ts'

export function TeamSlots({ team, onRemove }: { team: Pick[]; onRemove: (i: number) => void }) {
  return (
    <div className="team-slots" id="team">
      {Array.from({ length: TEAM_SIZE }, (_, i) => {
        const pick = team[i]
        if (!pick) {
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
