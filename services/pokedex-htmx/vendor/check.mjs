// Fails when vendor/sources.ts no longer matches client/page.js.
//
// sources.ts is generated, and nothing at runtime reads client/page.js -
// the server serves the embedded copy. So an edit to page.js changes
// nothing until `npm run vendor` runs, and the symptom is a browser that
// quietly behaves like the old code.
//
// page.js only: the vendored libraries beside it change on a deliberate
// version bump, where regenerating is the point rather than a step to
// forget.
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const page = readFileSync(join(here, '..', 'client', 'page.js'), 'utf8')
const sources = readFileSync(join(here, 'sources.ts'), 'utf8')

// The generator writes page.js as a JSON string literal, so compare the
// same encoding rather than trying to unescape what is in sources.ts.
if (!sources.includes(JSON.stringify(page))) {
  console.error('vendor/sources.ts does not match client/page.js - run `npm run vendor`')
  process.exit(1)
}
