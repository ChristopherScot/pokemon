export const TYPE_COLOURS: Record<string, string> = {
  normal: '#9fa19f', fire: '#e62829', water: '#2980ef', electric: '#fac000',
  grass: '#3fa129', ice: '#3dcef3', fighting: '#ff8000', poison: '#9141cb',
  ground: '#915121', flying: '#81b9ef', psychic: '#ef4179', bug: '#91a119',
  rock: '#afa981', ghost: '#704170', dragon: '#5060e1', dark: '#50413f',
  steel: '#60a1b8', fairy: '#ef70ef',
}

export const colour = (type: string): string => TYPE_COLOURS[type] ?? '#6b7280'

const ENTITIES: Record<string, string> = {
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
}

// Every interpolation into markup goes through this. A trainer name is
// user-supplied and reaches the page on every route.
export const esc = (s: unknown): string =>
  String(s).replace(/[&<>"']/g, (c) => ENTITIES[c] ?? c)

export const TEAM_SIZE = 3
