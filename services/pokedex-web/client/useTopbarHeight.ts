import { useEffect, useRef } from 'react'

export /**
 * Publishes the topbar's real height as --topbar-h.
 *
 * The filters pin underneath it, and the offset was a hardcoded 86px -
 * roughly right on a desktop window and wrong everywhere else. The
 * topbar WRAPS (title, search, three slots, a button), so on a phone it
 * is two or three rows tall and the filters slid behind the header and
 * out of reach.
 *
 * ResizeObserver rather than a resize listener: the topbar also changes
 * height when a team slot fills and the text inside it reflows, which
 * fires no resize event. The port dropped this and the CSS fell back to
 * the hardcoded value the comment calls wrong.
 */
function useTopbarHeight() {
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
