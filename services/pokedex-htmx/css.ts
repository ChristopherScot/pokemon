// The stylesheets, carried over byte-for-byte from pokedex-web so the
// two render identically. They were extracted from that service's
// template literals rather than retyped, which is the only way to be
// sure "near identical" is actually true.
//
// Additions for this port are at the bottom of each block, marked.

export const POKEDEX_CSS = `
  :root { color-scheme: dark; --bg:#12141c; --card:#1c1f2b; --fg:#e8eaf2; --dim:#8b91a7; }
  * { box-sizing: border-box; }
  body { margin:0; padding:24px; background:var(--bg); color:var(--fg);
         font:15px/1.5 ui-sans-serif,system-ui,-apple-system,sans-serif; }
  h1 { margin:0 0 4px; font-size:22px; letter-spacing:.02em; }
  .sub { color:var(--dim); margin:0 0 20px; font-size:13px; }
  .battle-link { color:#a78bfa; text-decoration:none; font-weight:600; }
  .battle-link:hover { text-decoration:underline; }

  .topbar { position:sticky; top:0; z-index:10; background:var(--bg);
            padding:16px 0 12px; margin-bottom:6px;
            border-bottom:1px solid #262a38;
            display:flex; flex-wrap:wrap; gap:16px; align-items:center; }
  .topbar h1 { margin:0; }
  .topbar .sub { margin:2px 0 0; }
  .topbar .title { margin-right:auto; }
  #search { flex:0 1 280px; font:inherit; color:var(--fg); background:var(--card);
            border:1px solid #333a4d; border-radius:999px; padding:8px 14px;
            outline:none; transition:border-color .15s; }
  #search:focus { border-color:#6d5ae0; }
  #search::placeholder { color:#5b6272; }
  #downloads { margin:48px auto 8px; max-width:640px; padding:24px;
               border:1px solid #2a3040; border-radius:14px; background:var(--card); }
  #downloads h2 { margin:0 0 4px; font-size:16px; }
  #downloads .sub { margin:0 0 16px; }
  .dl-row { display:flex; flex-wrap:wrap; gap:12px; }
  .dl { display:flex; align-items:center; gap:10px; flex:1 1 200px;
        padding:12px 14px; border:1px solid #333a4d; border-radius:10px;
        background:#161926; color:var(--fg); text-decoration:none; }
  .dl:hover { border-color:#4b5573; background:#1a1e2d; }
  .dl svg { flex:0 0 auto; width:22px; height:22px; }
  .dl-name { font-weight:600; }
  .dl-meta { color:var(--dim); font-size:12px; }
  .dl-other { margin:16px 0 0; font-size:13px; }
  .dl-other a { color:var(--dim); }

  .card.hidden { display:none; }

  .sr-only { position:absolute; width:1px; height:1px; padding:0; margin:-1px;
             overflow:hidden; clip:rect(0 0 0 0); white-space:nowrap; border:0; }

  .modal-backdrop { position:fixed; inset:0; display:grid; place-items:center;
                    background:rgba(9,10,14,.7); backdrop-filter:blur(2px); z-index:50; }
  .modal { border:1px solid #333a4d; border-radius:14px; background:var(--card);
           color:var(--fg); padding:22px 24px; max-width:380px; width:calc(100% - 32px); }
  .modal h2 { margin:0 0 6px; font-size:18px; }
  .modal p { margin:0 0 14px; color:var(--dim); font-size:13px; }
  .modal input { width:100%; font:inherit; color:var(--fg); background:var(--bg);
                 border:1px solid #333a4d; border-radius:8px; padding:9px 12px;
                 outline:none; margin-bottom:14px; }
  .modal input:focus { border-color:#6d5ae0; }
  .modal-error { color:#f87171; margin:-8px 0 12px; }
  .modal-actions { display:flex; gap:8px; justify-content:flex-end; }
  .modal-actions button { font:inherit; font-weight:600; border:none; border-radius:8px;
                          padding:8px 14px; cursor:pointer; color:#12141c; background:#a78bfa; }
  .modal-actions .ghost { background:transparent; color:var(--dim); border:1px solid #333a4d; }
  .team-slots { display:flex; gap:8px; }
  .slot { width:60px; height:60px; border-radius:10px; background:var(--card);
          border:2px dashed #363c4e; display:flex; align-items:center;
          justify-content:center; position:relative; transition:border-color .2s, transform .2s; }
  .slot.filled { border-style:solid; border-color:#6d5ae0; }
  .slot img { width:52px; height:52px; image-rendering:pixelated; }
  .slot-empty { color:#4b5163; font-weight:700; }
  .slot .remove { position:absolute; top:-8px; right:-8px; width:24px; height:24px;
                  border-radius:50%; background:#f87171; color:#12141c; border:none;
                  font-size:12px; line-height:1; cursor:pointer; display:none; padding:0; }
  .slot.filled .remove { display:block; }
  #ready { font:inherit; font-weight:600; color:#12141c; background:#a78bfa;
           border:none; border-radius:8px; padding:8px 16px; cursor:pointer;
           transition:opacity .2s, transform .1s; }
  #ready:disabled { opacity:.35; cursor:not-allowed; }
  #ready:not(:disabled):hover { transform:translateY(-1px); }

  .card { cursor:pointer; transition:outline-color .15s, transform .1s; outline:2px solid transparent;
          font:inherit; color:inherit; text-align:left; border:0; width:100%; display:block; }
  .card:focus-visible, #search:focus-visible, .modal input:focus-visible,
  .filters a:focus-visible, button:focus-visible {
    outline:2px solid #a78bfa; outline-offset:2px;
  }
  .card:hover { transform:translateY(-2px); }
  .card.picked { outline-color:#6d5ae0; }
  .card.picked::after { content:'on your team'; position:absolute; top:8px; right:8px;
                        background:#6d5ae0; color:#fff; font-size:10px; font-weight:700;
                        padding:2px 6px; border-radius:999px; }
  .card { position:relative; }
  .filters { position:sticky; top:var(--topbar-h, 86px); z-index:9; background:var(--bg);
             display:flex; flex-wrap:wrap; gap:8px;
             padding:10px 0 12px; margin-bottom:12px;
             border-bottom:1px solid #262a38; }
  .filters a { padding:11px 14px; min-height:44px; display:inline-flex; align-items:center; border:1px solid #333a4d; border-radius:999px;
               color:var(--fg); text-decoration:none; font-size:13px; }
  .filters a.on { background:#2a2f40; border-color:#5b6c8f; }
  .filters small { color:var(--dim); }
  .grid { display:grid; gap:14px;
          grid-template-columns:repeat(auto-fill,minmax(210px,1fr)); }
  .card { background:var(--card); border:1px solid #262b3a; border-radius:12px;
          padding:14px; text-align:center; }
  .card header { display:flex; align-items:baseline; justify-content:center; gap:7px; }
  .num { color:var(--dim); font-size:12px; font-variant-numeric:tabular-nums; }
  .card h2 { margin:0; font-size:16px; text-transform:capitalize; }
  .card img { image-rendering:pixelated; display:block; margin:2px auto;
              background:radial-gradient(circle at 50% 45%, #f2f4f8 58%, transparent 62%);
              border-radius:50%; }
  .types { display:flex; gap:5px; justify-content:center; margin-bottom:10px; }
  .type { padding:2px 9px; border-radius:999px; font-size:11px;
          text-transform:uppercase; letter-spacing:.04em; color:#fff; }
  dl { display:grid; grid-template-columns:auto auto; gap:1px 10px;
       justify-content:center; margin:0 0 10px; font-size:12px; }
  dt { color:var(--dim); } dd { margin:0; font-variant-numeric:tabular-nums; }
  .moves { list-style:none; margin:0; padding:10px 0 0; border-top:1px solid #262b3a;
           font-size:11px; }
  .moves li { display:grid; grid-template-columns:1fr auto auto; gap:8px;
              text-align:left; padding:1px 0; }
  .moves b { font-variant-numeric:tabular-nums; color:var(--dim); min-width:22px;
             text-align:right; }
  .empty { color:var(--dim); padding:32px 0; }

  /* The form wrapping the slots and the button is display:contents, so
     its children are the topbar's own flex items - exactly the four
     pokedex-web had (title, search, slots, button). Without this the
     form is a single item and the button wraps onto a second row. */
  #team-form { display:contents; }

  /* htmx: the request-in-flight cue. pokedex-web showed "opening…" by
     swapping React state; here the indicator class does the same job
     with no state to track. */
  .htmx-indicator { opacity:0; transition:opacity .15s; }
  .htmx-request .htmx-indicator { opacity:1; }
  .htmx-request.card { opacity:.6; }
`

export const BATTLE_CSS = `
  :root { color-scheme: dark; --bg:#12141c; --card:#1c1f2b; --fg:#e8eaf2; --dim:#8b91a7;
          --good:#4ade80; --warn:#fbbf24; --danger:#f87171; }
  * { box-sizing: border-box; }
  body { margin:0; padding:24px; background:var(--bg); color:var(--fg);
         font:15px/1.5 ui-sans-serif,system-ui,-apple-system,sans-serif; }
  a { color:inherit; }
  h1 { margin:0 0 4px; font-size:22px; }
  .sub { color:var(--dim); margin:0 0 20px; font-size:13px; }
  .side { margin-bottom:22px; }
  .side h2 { font-size:13px; text-transform:uppercase; letter-spacing:.08em;
             color:var(--dim); margin:0 0 8px; font-weight:600; }
  .mon { display:grid; grid-template-columns:52px 1fr 92px; gap:12px; align-items:center;
         background:var(--card); border-radius:10px; padding:10px 12px; margin-bottom:8px;
         position:relative; transition:opacity .4s ease, filter .4s ease; }
  .mon.fainted { opacity:.35; filter:grayscale(1); }
  .mon img { width:52px; height:52px; image-rendering:pixelated; }
  .name { font-weight:600; text-transform:capitalize; }
  .types { display:flex; gap:4px; margin-top:3px; }
  .type { font-size:10px; text-transform:uppercase; letter-spacing:.04em;
          padding:1px 6px; border-radius:999px; color:#12141c; font-weight:700; }
  .hpwrap { height:8px; background:#2a2e3d; border-radius:999px; overflow:hidden; margin-top:6px; }
  .hp { height:100%; border-radius:999px; transition:width .55s cubic-bezier(.22,.61,.36,1), background-color .3s; }
  .hpnum { text-align:right; font-variant-numeric:tabular-nums; color:var(--dim); font-size:13px; }
  @keyframes floatUp {
    0%   { opacity:0; transform:translateY(6px) scale(.9); }
    15%  { opacity:1; transform:translateY(0) scale(1.08); }
    100% { opacity:0; transform:translateY(-26px) scale(1); }
  }
  .float { position:absolute; right:104px; top:8px; font-weight:800; font-size:22px;
           pointer-events:none; animation:floatUp 2.4s ease-out forwards;
           text-shadow:0 2px 8px rgba(0,0,0,.6); }
  .float.super { color:var(--warn); text-shadow:0 0 12px rgba(251,191,36,.6); }
  .float.weak  { color:var(--dim); font-size:14px; }
  .float.immune{ color:var(--dim); font-size:13px; font-style:italic; }
  .float.normal{ color:var(--danger); }
  @keyframes shake {
    0%,100% { transform:translateX(0); }
    20% { transform:translateX(-5px); } 40% { transform:translateX(5px); }
    60% { transform:translateX(-3px); } 80% { transform:translateX(3px); }
  }
  .mon.hit { animation:shake .4s ease-in-out; }
  @keyframes shakeHard {
    0%,100% { transform:translateX(0); }
    15% { transform:translateX(-9px); } 30% { transform:translateX(9px); }
    45% { transform:translateX(-7px); } 60% { transform:translateX(7px); }
    75% { transform:translateX(-4px); } 90% { transform:translateX(4px); }
  }
  .mon.hit-hard { animation:shakeHard .5s ease-in-out;
                  box-shadow:0 0 22px rgba(251,191,36,.45); }
  .banner { font-size:15px; font-weight:700; padding:10px 14px; border-radius:10px;
            margin-bottom:18px; transition:background-color .4s ease; }
  .banner.mine { background:rgba(74,222,128,.14); color:var(--good); }
  .banner.theirs { background:rgba(139,145,167,.12); color:var(--dim); }
  .banner.over { background:rgba(251,191,36,.16); color:var(--warn); }
  .log { background:var(--card); border-radius:10px; padding:12px 14px; font-size:13px;
         max-height:180px; overflow-y:auto; }
  .log div { padding:2px 0; animation:fadeIn .4s ease; }
  .log div.super { color:var(--warn); font-weight:600; }
  .log div.weak { color:var(--dim); }
  .log div.immune { color:var(--dim); font-style:italic; }
  @keyframes fadeIn { from { opacity:0; transform:translateX(-6px); } to { opacity:1; transform:none; } }
  .moves { display:flex; flex-wrap:wrap; gap:6px; margin:10px 0 0; }
  button { font:inherit; color:inherit; background:#252a38; border:1px solid #333a4d;
           border-radius:8px; padding:5px 10px; cursor:pointer; transition:all .15s; }
  button:hover:not(:disabled) { background:#2f3547; border-color:#4a5268; }
  button:disabled { opacity:.35; cursor:not-allowed; }
  button.sel { background:#3b3170; border-color:#6d5ae0; }
  .btn { font:inherit; border-radius:8px; padding:5px 10px; text-decoration:none;
         font-weight:600; color:#12141c; background:#a78bfa; border:1px solid #a78bfa;
         text-align:center; transition:background .15s; }
  .btn:hover { background:#b9a3fb; }
  .commit { display:flex; justify-content:flex-end; margin-top:14px;
            padding-top:12px; border-top:1px solid #2c3040; }
  .commit button { background:#b42318; border-color:#d0362a; color:#fff;
                   font-weight:600; padding:9px 20px; min-height:44px; }
  .commit button:hover:not(:disabled) { background:#d0362a; border-color:#e8574a; }
  .pick { margin-top:14px; padding:14px; background:var(--card); border-radius:10px; }
  .pick.hidden { display:none; }
  @keyframes celebrate { 0%,100% { box-shadow:none; } 50% { box-shadow:0 0 40px rgba(74,222,128,.35); } }
  .board.won { animation:celebrate 1.2s ease-in-out 2; border-radius:14px; }
  /* Added for this port. pokedex-web made each mon and each move a
     <button> and tracked the choice in React state; here they are
     <label>s wrapping a radio, so the CHOICE IS THE FORM and the server
     renders which one is checked. These rules make a label look exactly
     like the button it replaced. */
  .sr-only { position:absolute; width:1px; height:1px; padding:0; margin:-1px;
             overflow:hidden; clip:rect(0 0 0 0); white-space:nowrap; border:0; }
  /* pokedex-web's selectable .mon was a <button>, which as a grid
     container shrinks to fit rather than filling the row. A <label> is
     block-level and would stretch, so it is made to size itself the same
     way - otherwise selectable rows are full width and unselectable ones
     are not, which is visible the moment it stops being your turn. */
  /* The 1px transparent border is not decoration: pokedex-web's .mon
     was a <button>, so it picked up BOTH the box and the #333a4d edge
     from the generic button rule above. A bare <label> gets neither,
     which left every selectable row 2px smaller and visibly unedged. */
  label.mon { cursor:pointer; width:fit-content; text-align:center;
              border:1px solid #333a4d; }
  label.mon:has(input:focus-visible) { outline:2px solid #a78bfa; outline-offset:2px; }
  .movebtn { display:inline-block; font:inherit; color:inherit; background:#252a38;
             border:1px solid #333a4d; border-radius:8px; padding:5px 10px;
             cursor:pointer; transition:all .15s; }
  .movebtn:hover { background:#2f3547; border-color:#4a5268; }
  .movebtn.sel { background:#3b3170; border-color:#6d5ae0; }
  .movebtn:has(input:focus-visible) { outline:2px solid #a78bfa; outline-offset:2px; }
  /* A move you may not use this turn is not a greyed control: the
     server omits the control and states the reason. */
  .move-off { display:inline-block; padding:5px 10px; border-radius:8px;
              border:1px dashed #333a4d; color:var(--dim); font-size:13px; }
  .move-off small { opacity:.8; }

  @media (prefers-reduced-motion: reduce) {
    *, *::before, *::after { animation:none !important; transition:none !important; }
  }
`

export const LOBBY_CSS = `
  :root { color-scheme: dark; --bg:#12141c; --card:#1c1f2b; --fg:#e8eaf2; --dim:#8b91a7; }
  * { box-sizing:border-box; }
  body { margin:0; padding:24px; background:var(--bg); color:var(--fg);
         font:15px/1.5 ui-sans-serif,system-ui,-apple-system,sans-serif; }
  a { color:inherit; }
  h1 { margin:0 0 4px; font-size:22px; }
  .sub { color:var(--dim); margin:0 0 20px; font-size:13px; }
  .row { display:flex; justify-content:space-between; align-items:center; gap:12px;
         background:var(--card); border-radius:10px; padding:12px 14px; margin-bottom:8px;
         animation:fadeIn .3s ease; }
  @keyframes fadeIn { from { opacity:0; transform:translateY(4px);} to { opacity:1; transform:none; } }
  input, button { font:inherit; color:inherit; background:#252a38; border:1px solid #333a4d;
                  border-radius:8px; padding:7px 12px; }
  button { cursor:pointer; transition:background .15s; }
  button:hover { background:#2f3547; }
  .linkish { background:none; border:0; padding:0; color:inherit;
             text-decoration:underline; cursor:pointer; font:inherit; }
  .btn { font:inherit; background:#a78bfa; border:1px solid #a78bfa;
         border-radius:8px; padding:7px 12px; text-decoration:none;
         font-weight:600; color:#12141c; transition:background .15s; }
  .btn:hover { background:#b9a3fb; }
  .team { display:flex; gap:6px; flex-wrap:wrap; margin:10px 0; }
`

