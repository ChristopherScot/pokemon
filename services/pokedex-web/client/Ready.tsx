import { useState } from 'react'

import { TEAM_SIZE, V } from './shared.ts'
import type { Pick } from './useTeam.ts'

export function Ready({
  team, join, onCommitted,
}: {
  team: Pick[]
  join: string
  onCommitted: () => void
}) {
  const [busy, setBusy] = useState(false)
  const [needsName, setNeedsName] = useState(false)
  const [error, setError] = useState('')

  async function commit(): Promise<boolean> {
    const body = team.length ? { team: team.map((t) => t.name) } : {}
    const url = join ? `/battle/${encodeURIComponent(join)}/join` : '/battle/open'
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'content-type': 'application/json', ...V },
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
      headers: { 'content-type': 'application/json', ...V },
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
