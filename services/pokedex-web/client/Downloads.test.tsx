// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { Downloads, pickLinks } from './Downloads.tsx'

afterEach(() => { vi.unstubAllGlobals() })

const mac = { os: 'darwin', arch: 'arm64', label: 'macOS · Apple Silicon' } as const

const release = (tag: string, assets: string[], over = {}) => ({
  tag_name: tag,
  assets: assets.map((name) => ({ name, browser_download_url: `https://x/${name}` })),
  ...over,
})

// The tools are released independently - the TUI can be on v0.1.1 and
// the CLI on v0.1.2 - so /latest would return whichever released most
// recently and hide the other. Walking newest-first finds the current
// release of each.
test('each tool gets its own newest release', () => {
  const links = pickLinks([
    release('v0.1.2', ['pokedex-cli_darwin_arm64.tar.gz']),
    release('v0.1.1', ['pokedex-tui_darwin_arm64.tar.gz', 'pokedex-cli_darwin_arm64.tar.gz']),
  ], mac)

  const byName = Object.fromEntries(links.map((l) => [l.tool.prefix, l.tag]))
  expect(byName['pokedex-cli']).toBe('v0.1.2')
  expect(byName['pokedex-tui']).toBe('v0.1.1')
})

test('drafts and prereleases are skipped', () => {
  const links = pickLinks([
    release('v0.2.0', ['pokedex-cli_darwin_arm64.tar.gz'], { prerelease: true }),
    release('v0.1.9', ['pokedex-cli_darwin_arm64.tar.gz'], { draft: true }),
    release('v0.1.8', ['pokedex-cli_darwin_arm64.tar.gz']),
  ], mac)
  expect(links.find((l) => l.tool.prefix === 'pokedex-cli')?.tag).toBe('v0.1.8')
})

test('a build for another machine is not offered', () => {
  const links = pickLinks([release('v1', ['pokedex-cli_linux_amd64.tar.gz'])], mac)
  expect(links).toHaveLength(0)
})

// Offline, rate-limited, blocked, or a platform with no build: all of
// them render nothing. A footer saying "could not load downloads" is
// worse than no footer, because there is nothing the reader can do.
test('a failed fetch shows nothing at all', async () => {
  vi.stubGlobal('fetch', async () => { throw new Error('offline') })
  const { container } = render(<Downloads />)
  await waitFor(() => expect(container.querySelector('#downloads')).toBeNull())
})

test('an unrecognised platform shows nothing', async () => {
  vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Windows NT 10.0)' })
  vi.stubGlobal('fetch', async () => ({ ok: true, json: async () => [] }))
  const { container } = render(<Downloads />)
  await waitFor(() => expect(container.querySelector('#downloads')).toBeNull())
})

// And the happy path actually renders a link you can click.
test('a matching build is offered for this machine', async () => {
  vi.stubGlobal('navigator', {
    userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36',
  })
  vi.stubGlobal('fetch', async () => ({
    ok: true,
    json: async () => [release('v0.1.2', [
      'pokedex-cli_darwin_amd64.tar.gz',
      'pokedex-tui_darwin_amd64.tar.gz',
    ])],
  }))

  render(<Downloads />)
  const link = await screen.findByRole('link', { name: /Pokedex CLI/ })
  expect(link).toHaveAttribute('href', 'https://x/pokedex-cli_darwin_amd64.tar.gz')
  expect(screen.getByText(/for macOS/)).toBeTruthy()
  // The escape hatch for everyone else.
  expect(screen.getByRole('link', { name: /other platforms/i })).toBeTruthy()
})
