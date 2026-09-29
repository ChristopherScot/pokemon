import { useEffect, useRef } from 'react'

/**
 * Publishes the topbar's real height as --topbar-h. Uses ResizeObserver because
 * the topbar changes height on internal text reflow, which fires no resize event.
 */
export function useTopbarHeight() {
  const ref = useRef<HTMLElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const publish = () =>
      document.documentElement.style.setProperty('--topbar-h', `${el.getBoundingClientRect().height}px`)
    publish()
    const ro = new ResizeObserver(publish)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  return ref
}
