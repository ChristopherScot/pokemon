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

/**
 * JSON for a `<script type="application/json">` island.
 *
 * JSON.stringify does NOT escape `<`, so a trainer named
 * `x</script><script>alert(1)</script>` closes the island early and the
 * rest of the string is parsed as markup and executed. Everything else
 * on these pages goes through esc(); the island was the one hole, and
 * it is the one carrying attacker-influenceable data.
 *
 * `<` is valid JSON and parses back to the same string, so nothing
 * downstream changes. U+2028 and U+2029 are escaped for a different
 * reason: they are valid in JSON but are line terminators in JavaScript
 * source, which breaks any consumer that eval()s this.
 *
 * Every SSR framework ships this function. "JSON.stringify is safe in
 * HTML" is a common and wrong belief.
 */
export function island(data: unknown): string {
  return JSON.stringify(data)
    .replace(/</g, '\\u003c')
    .replace(/\u2028/g, '\\u2028')
    .replace(/\u2029/g, '\\u2029')
}
