
export type Platform = { os: 'darwin' | 'linux'; arch: 'arm64' | 'amd64'; label: string }

type UAData = {
  platform?: string
  getHighEntropyValues?: (hints: string[]) => Promise<{ architecture?: string }>
}

export async function detectPlatform(): Promise<Platform | null> {
  const ua = navigator.userAgent
  const data = (navigator as Navigator & { userAgentData?: UAData }).userAgentData
  const hint = data?.platform ?? ''

  const os: Platform['os'] | '' =
    /Mac|Darwin/i.test(hint + ua) ? 'darwin'
    : /Linux|X11/i.test(hint + ua) && !/Android/i.test(ua) ? 'linux'
    : ''
  if (!os) return null

  let arch: Platform['arch'] | '' = ''
  try {
    const high = await data?.getHighEntropyValues?.(['architecture'])
    if (high?.architecture) arch = high.architecture === 'arm' ? 'arm64' : 'amd64'
  } catch {
    // Not Chromium, or the call was refused. Fall through.
  }
  if (!arch) {
    try {
      const gl = document.createElement('canvas').getContext('webgl')
      const dbg = gl?.getExtension('WEBGL_debug_renderer_info')
      const renderer = dbg && gl ? String(gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL)) : ''
      if (/Apple [GM]/.test(renderer)) arch = 'arm64'
    } catch {
      // Canvas blocked. Fall through.
    }
  }
  if (!arch) arch = /aarch64|arm64/i.test(ua) ? 'arm64' : 'amd64'

  return {
    os,
    arch,
    label: `${os === 'darwin' ? 'macOS' : 'Linux'} · ${
      arch === 'arm64' ? (os === 'darwin' ? 'Apple Silicon' : 'ARM64') : 'Intel / AMD'
    }`,
  }
}
