// The pokedex page's script, in full. Three jobs, none of which is a
// hypermedia exchange, and none of which touches server data.
(function () {
  'use strict'

  // 1. Search: a pure VIEW filter over a list the server already sent.
  //
  // Deliberately not `hx-get="/?q=" hx-trigger="keyup changed delay:200ms"`.
  // The whole pokedex is already in this document, so filtering is
  // instant; a debounced round trip per keystroke would be SLOWER than
  // the React version it is replacing. The essays allow client state
  // that "does not affect server data" - this is that case exactly.
  var search = document.getElementById('search')
  var count = document.getElementById('count')

  function filter() {
    var q = search.value.trim().toLowerCase()
    var cards = document.querySelectorAll('#grid .card')
    var shown = 0
    for (var i = 0; i < cards.length; i++) {
      var name = cards[i].id.slice('card-'.length)
      var hit = !q || name.indexOf(q) !== -1
      cards[i].classList.toggle('hidden', !hit)
      if (hit) shown++
    }
    var empty = document.getElementById('no-match')
    if (empty) {
      empty.hidden = !(q && shown === 0)
      document.getElementById('no-match-q').textContent = search.value
    }
    // Announced, not just visible: filtering used to change the grid
    // silently, so a screen-reader user had no idea how many were left.
    count.textContent = q ? shown + ' pokemon match ' + search.value : ''
  }

  if (search) {
    search.addEventListener('input', filter)
    // A swap replaces the grid; re-apply the filter to the new cards.
    document.body.addEventListener('htmx:afterSwap', filter)
  }

  // 2. Publish the topbar's real height as --topbar-h.
  //
  // The filters pin underneath it and the offset was a hardcoded 86px,
  // roughly right on a desktop and wrong everywhere else - the topbar
  // wraps, so on a phone it is two or three rows tall and the filters
  // slid behind it. ResizeObserver rather than a resize listener: it
  // also changes height when a slot fills, which fires no resize event,
  // and here that happens on an htmx swap.
  var bar = document.getElementById('topbar')
  if (bar && typeof ResizeObserver !== 'undefined') {
    var publish = function () {
      document.documentElement.style.setProperty(
        '--topbar-h', bar.getBoundingClientRect().height + 'px')
    }
    publish()
    new ResizeObserver(publish).observe(bar)
  }

  // 3. Platform detection for the downloads footer.
  //
  // This cannot move to the server: telling Apple Silicon from Intel
  // needs a WebGL renderer probe and userAgentData, both of which only
  // exist in the browser. The browser detects, then asks the SERVER for
  // the footer - so the release list is still fetched server-side and
  // the browser never sees GitHub's JSON.
  window.platform = function () { return window.__plat || {} }

  function detect() {
    var ua = navigator.userAgent
    var data = navigator.userAgentData
    var hint = (data && data.platform) || ''
    var os = /Mac|Darwin/i.test(hint + ua) ? 'darwin'
      : (/Linux|X11/i.test(hint + ua) && !/Android/i.test(ua)) ? 'linux' : ''
    if (!os) return Promise.resolve(null)

    var viaHints = data && data.getHighEntropyValues
      ? data.getHighEntropyValues(['architecture']).catch(function () { return null })
      : Promise.resolve(null)

    return viaHints.then(function (high) {
      var arch = high && high.architecture
        ? (high.architecture === 'arm' ? 'arm64' : 'amd64') : ''
      if (!arch) {
        try {
          var gl = document.createElement('canvas').getContext('webgl')
          var dbg = gl && gl.getExtension('WEBGL_debug_renderer_info')
          var r = dbg && gl ? String(gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL)) : ''
          if (/Apple [GM]/.test(r)) arch = 'arm64'
        } catch (e) { /* canvas blocked */ }
      }
      if (!arch) arch = /aarch64|arm64/i.test(ua) ? 'arm64' : 'amd64'
      return { os: os, arch: arch }
    })
  }

  // The footer waits for this: its hx-trigger is fired here, once the
  // probe has an answer to send.
  var footer = document.getElementById('downloads')
  if (footer) {
    detect().then(function (plat) {
      if (!plat) return
      window.__plat = plat
      window.htmx.trigger(footer, 'probed')
    })
  }
})()
