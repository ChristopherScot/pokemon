import { useEffect, useState } from 'react'

import { detectPlatform, type Platform } from './platform.ts'

const RELEASES =
  'https://api.github.com/repos/ChristopherScot/pokemon/releases?per_page=30'

type Tool = { prefix: string; name: string; blurb: string; icon: React.ReactNode }

const TOOLS: Tool[] = [
  {
    prefix: 'pokedex-tui',
    name: 'Pokedex TUI',
    blurb: 'Browse and battle in a full-screen terminal app',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"
           strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="2" y="4" width="20" height="16" rx="2" />
        <path d="M6 9l3 3-3 3M12 15h5" />
      </svg>
    ),
  },
  {
    prefix: 'pokedex-cli',
    name: 'Pokedex CLI',
    blurb: 'One-shot lookups and scripting',
    // A chevron prompt.
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"
           strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M4 6l5 6-5 6M12 18h8" />
      </svg>
    ),
  },
]

type Release = {
  draft?: boolean
  prerelease?: boolean
  tag_name: string
  assets?: Array<{ name: string; browser_download_url: string }>
}

type Link = { tool: Tool; url: string; tag: string }

export function pickLinks(releases: Release[], plat: Platform): Link[] {
  const want = `_${plat.os}_${plat.arch}.tar.gz`
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

export function Downloads() {
  const [state, setState] = useState<{ plat: Platform; links: Link[] } | null>(null)

  useEffect(() => {
    let live = true
    void (async () => {
      const plat = await detectPlatform()
      if (!plat || !live) return
      try {
        const res = await fetch(RELEASES, { headers: { Accept: 'application/vnd.github+json' } })
        if (!res.ok || !live) return
        const releases = (await res.json()) as unknown
        if (!Array.isArray(releases) || !live) return
        const links = pickLinks(releases as Release[], plat)
        if (links.length) setState({ plat, links })
      } catch {
        // Offline, rate limited, or blocked. Stay hidden.
      }
    })()
    return () => { live = false }
  }, [])

  if (!state) return null

  return (
    <footer id="downloads">
      <h2>Play from a terminal</h2>
      {/* Each tool carries its OWN version, because they are not
          released together - one line naming a single version would be
          wrong for whichever tool is not on it. */}
      <p className="sub">for {state.plat.label}</p>
      <div className="dl-row" id="dl-row">
        {state.links.map(({ tool, url, tag }) => (
          <a className="dl" href={url} download key={tool.prefix}>
            {tool.icon}
            <span>
              <span className="dl-name">{tool.name}</span><br />
              <span className="dl-meta">{tool.blurb} · {tag}</span>
            </span>
          </a>
        ))}
      </div>
      {/* The escape hatch. This footer only appears for a platform we
          build for, so anyone else - Windows, or a machine we guessed
          wrong about - needs a way to the full list. */}
      <p className="dl-other">
        <a href="https://github.com/ChristopherScot/pokemon/releases/latest"
           target="_blank" rel="noopener">All downloads &amp; other platforms</a>
      </p>
    </footer>
  )
}
