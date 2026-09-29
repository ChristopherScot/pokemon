(function () {
  'use strict'

  // Search is a pure view filter: the whole pokedex is already in the document.
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
    // Announced, or filtering the grid changes silently for a screen reader.
    count.textContent = q ? shown + ' pokemon match ' + search.value : ''
  }

  if (search) {
    search.addEventListener('input', filter)
    // Load-bearing: /team swaps a new grid out of band, so the filter has to
    // be re-applied after each swap or a search-in-progress is lost.
    // afterSettle, not afterSwap: three fragments interleave and afterSwap
    // fires before the grid is in place.
    document.body.addEventListener('htmx:afterSettle', filter)
  }

  // Publish topbar height as --topbar-h so filters pin under it correctly.
  // ResizeObserver catches slot-fills on htmx swaps that fire no resize event.
  var bar = document.getElementById('topbar')
  if (bar && typeof ResizeObserver !== 'undefined') {
    var publish = function () {
      document.documentElement.style.setProperty(
        '--topbar-h', bar.getBoundingClientRect().height + 'px')
    }
    publish()
    new ResizeObserver(publish).observe(bar)
  }

  // Client-only: telling Apple Silicon from Intel needs a WebGL renderer
  // probe and userAgentData. The browser probes, then asks the server for
  // the footer, so GitHub's JSON is still fetched server-side.
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

  // Fires the footer's hx-trigger once the probe has an answer.
  var footer = document.getElementById('downloads')
  if (footer) {
    detect().then(function (plat) {
      if (!plat) return
      window.__plat = plat
      window.htmx.trigger(footer, 'probed')
    })
  }
})()
