import { esc } from './types.ts'

const RELEASES =
  'https://api.github.com/repos/ChristopherScot/pokemon/releases?per_page=30'

type Tool = { prefix: string; name: string; blurb: string; icon: string }

const ICON = (path: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"` +
  ` stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${path}</svg>`

const TOOLS: Tool[] = [
  {
    prefix: 'pokedex-tui',
    name: 'Pokedex TUI',
    blurb: 'Browse and battle in a full-screen terminal app',
    icon: ICON('<rect x="2" y="4" width="20" height="16" rx="2" /><path d="M6 9l3 3-3 3M12 15h5" />'),
  },
  {
    prefix: 'pokedex-cli',
    name: 'Pokedex CLI',
    blurb: 'One-shot lookups and scripting',
    // A chevron prompt.
    icon: ICON('<path d="M4 6l5 6-5 6M12 18h8" />'),
  },
]

type Release = {
  draft?: boolean
  prerelease?: boolean
  tag_name: string
  assets?: Array<{ name: string; browser_download_url: string }>
}

export type Link = { tool: Tool; url: string; tag: string }

export function pickLinks(releases: Release[], os: string, arch: string): Link[] {
  const want = `_${os}_${arch}.tar.gz`
  const links: Link[] = []
  for (const tool of TOOLS) {
    for (const release of releases) {
      if (release.draft || release.prerelease) continue
      const asset = (release.assets ?? []).find((a) => a.name === tool.prefix + want)
      if (asset) {
        links.push({ tool, url: asset.browser_download_url, tag: release.tag_name })
        break
      }
    }
  }
  return links
}

export const label = (os: string, arch: string): string =>
  `${os === 'darwin' ? 'macOS' : 'Linux'} · ${
    arch === 'arm64' ? (os === 'darwin' ? 'Apple Silicon' : 'ARM64') : 'Intel / AMD'}`

// The release list is fetched HERE, by the pod, and rendered to HTML.
// pokedex-web fetched GitHub's JSON from the browser; only the platform
// probe has to stay client-side, because telling Apple Silicon from
// Intel needs a WebGL renderer string that exists nowhere else.
export async function downloadsFooter(
  os: string, arch: string, fetchImpl: typeof fetch = fetch,
): Promise<string> {
  if (os !== 'darwin' && os !== 'linux') return ''
  if (arch !== 'arm64' && arch !== 'amd64') return ''

  let releases: unknown
  try {
    const res = await fetchImpl(RELEASES, {
      headers: { Accept: 'application/vnd.github+json', 'User-Agent': 'pokedex-htmx' },
    })
    if (!res.ok) return ''
    releases = await res.json()
  } catch {
    // Offline, rate limited, or blocked. Stay hidden, as before.
    return ''
  }
  if (!Array.isArray(releases)) return ''

  const links = pickLinks(releases as Release[], os, arch)
  if (!links.length) return ''

  const row = links.map(({ tool, url, tag }) =>
    `<a class="dl" href="${esc(url)}" download>${tool.icon}<span>` +
    `<span class="dl-name">${esc(tool.name)}</span><br>` +
    // Each tool carries its OWN version, because they are not released
    // together - one line naming a single version would be wrong for
    // whichever tool is not on it.
    `<span class="dl-meta">${esc(tool.blurb)} · ${esc(tag)}</span></span></a>`).join('')

  return `<footer id="downloads">` +
    `<h2>Play from a terminal</h2>` +
    `<p class="sub">for ${esc(label(os, arch))}</p>` +
    `<div class="dl-row" id="dl-row">${row}</div>` +
    // The escape hatch. This footer only appears for a platform we build
    // for, so anyone else needs a way to the full list.
    `<p class="dl-other"><a href="https://github.com/ChristopherScot/pokemon/releases/latest"` +
    ` target="_blank" rel="noopener">All downloads &amp; other platforms</a></p>` +
    `</footer>`
}
