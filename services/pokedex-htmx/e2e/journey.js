// Not run by `make test` or CI: needs a browser and running stack, run manually.
// See the README for stack-up commands.
//   B=http://127.0.0.1:3001 node e2e/journey.js
import { chromium } from 'playwright'
const B = process.env.B || 'http://127.0.0.1:3001'
const errs=[]; let failed=0
const ok=(c,m)=>{ console.log((c?'  PASS ':'  FAIL ')+m); if(!c)failed++ }
const watch=(p,w)=>{ p.on('pageerror',e=>errs.push(w+': '+e.message))
  p.on('response',r=>{ if(r.status()>=500) errs.push(w+' '+r.status()+' '+r.url()) })
  // An out-of-band swap that finds no target is reported here and nowhere
  // else: htmx discards the fragment and the page just does not update.
  p.on('console',m=>{ if(/oobError/i.test(m.text())) errs.push(w+' '+m.text()) }) }

// page.js clears data-busy when no htmx request is outstanding. Waiting on
// it rather than on a duration: a fixed sleep is both slower than the page
// and, on a loaded machine, shorter than it.
const settled=(p,ms=15000)=>p.waitForFunction(
  ()=>!document.body.hasAttribute('data-busy'),null,{timeout:ms})

;(async()=>{
const br=await chromium.launch({ignoreHTTPSErrors:true})
const c1=await br.newContext({ignoreHTTPSErrors:true}), c2=await br.newContext({ignoreHTTPSErrors:true})
const h=await c1.newPage(), g=await c2.newPage()
watch(h,'HOST'); watch(g,'GUEST')
const hn='h'+Math.floor(Math.random()*99999), gn='g'+Math.floor(Math.random()*99999)

console.log('1. host: pick a team as a new visitor, then register when asked')
await h.goto(B+'/',{waitUntil:'networkidle'})
for(const n of ['pikachu','onix','gengar']) await h.click('#card-'+n)
await settled(h)
ok(await h.locator('.slot.filled').count()===3, 'three slots filled after rapid clicks')
// The grid arrives as an out-of-band swap, and a broken one fails silently:
// the slots fill from the main swap either way, so only the cards show it.
ok(await h.locator('.card.picked').count()===3, 'and the three cards show as picked')
await h.click('#ready'); await settled(h)
ok(await h.locator('#name-dialog').count()>0, 'asked for a name')
await h.fill('#trainer-name',hn); await h.click('#name-dialog button[type=submit]')
await h.waitForURL(/\/battle\//,{timeout:12000})
const id=new URL(h.url()).pathname.split('/').pop()
ok(true,'landed on battle '+id)
ok(!/Pick your team/.test(await h.locator('body').innerText()), 'not asked for a team again')

console.log('2. guest: register in the lobby, then join from it')
await g.goto(B+'/battle',{waitUntil:'networkidle'})
await g.fill('#name',gn); await g.click('#reg'); await settled(g)
ok(/you are/.test(await g.locator('body').innerText()), 'registered')
await g.waitForSelector('#w-'+id,{timeout:12000})
await g.click('#w-'+id+' a.btn'); await g.waitForLoadState('networkidle')
ok(new URL(g.url()).search.includes('join='+id), 'on the pick page for that battle')
for(const n of ['charizard','blastoise','venusaur']) await g.click('#card-'+n)
await settled(g)
ok(await g.locator('.slot.filled').count()===3, 'guest got three slots')
ok(await g.locator('.card.picked').count()===3, 'and the guest cards show as picked')
await g.click('#ready'); await settled(g)
ok(g.url().includes('/battle/'+id), 'guest joined the battle')

console.log('3. both boards are live')
// The banner is polled every 1s, so settled() says nothing about it - there
// is no request in flight between polls. Wait for the text itself.
await h.waitForFunction(
  ()=>!/waiting for an opponent/.test(document.querySelector('#banner').textContent),
  null,{timeout:15000}).catch(()=>{})
const hb=(await h.locator('#banner').textContent()).trim()
ok(!/waiting for an opponent/.test(hb), 'host sees the join: '+JSON.stringify(hb))

console.log('4. take a turn')
if(await h.locator('#go').count()){
  await h.click('#go'); await settled(h)
  await h.waitForFunction(
    ()=>/waiting on/.test(document.querySelector('#banner').textContent),
    null,{timeout:15000}).catch(()=>{})
}
ok(/waiting on/.test((await h.locator('#banner').textContent()).trim()), 'turn passed to the guest')

console.log('5. host wanders back to the pokedex mid-game, then returns')
await h.goto(B+'/',{waitUntil:'networkidle'})
const back=await h.locator('a.battle-link[href="/battle/'+id+'"]').count()
ok(back>0,'the pokedex offers a way back to the battle')
if(back){ await h.click('a.battle-link[href="/battle/'+id+'"]'); await settled(h) }
ok(h.url().endsWith('/battle/'+id), 'back on the battle')
ok(await h.locator('.mon').count()===6, 'the board is intact')

console.log('\nERRORS:', errs.length?errs:'none')
await br.close(); process.exit(failed||errs.length?1:0)
})().catch(e=>{console.log('THREW:',e.message); console.log(errs); process.exit(1)})
