// The type palette, in one place.
//
// It was declared in battle.ts and again in pokedex.ts - two copies of
// one table in one service, which is how the two pages ended up
// choosing OPPOSITE foreground colours for the same badge. Both sides
// import it now, and so does the browser code.
export const TYPE_COLOURS: Record<string, string> = {
  normal: '#9fa19f', fire: '#e62829', water: '#2980ef', electric: '#fac000',
  grass: '#3fa129', ice: '#3dcef3', fighting: '#ff8000', poison: '#9141cb',
  ground: '#915121', flying: '#81b9ef', psychic: '#ef4179', bug: '#91a119',
  rock: '#afa981', ghost: '#704170', dragon: '#5060e1', dark: '#50413f',
  steel: '#60a1b8', fairy: '#ef70ef',
}

/** The badge colour for a type, or a neutral grey for an unknown one. */
export const colour = (type: string): string => TYPE_COLOURS[type] ?? '#6b7280'
