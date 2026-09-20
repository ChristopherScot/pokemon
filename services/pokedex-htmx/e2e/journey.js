// Two players, clicked in a browser, at the speed a person clicks.
//
// Every bug this file exists for got past the unit tests AND a pixel
// diff: a taken name that silently did nothing, and three quick clicks
// that left one pokemon picked. Both need a real browser and no
// artificial waiting between clicks to reproduce.
//
// NOT run by `make test`, and NOT run by CI. It needs a browser and a
// running stack, and Playwright is deliberately not a dependency here.
// Nothing will tell you if it breaks - run it yourself when you change
// how the page behaves. See the README for the two commands that bring
// the stack up without a database.
//
//   B=http://127.0.0.1:3001 node e2e/journey.js
import { chromium } from 'playwright'
const B = process.env.B || 'http://127.0.0.1:3001'
const errs=[]; let failed=0
const ok=(c,m)=>{ console.log((c?'  PASS ':'  FAIL ')+m); if(!c)failed++ }
const watch=(p,w)=>{ p.on('pageerror',e=>errs.push(w+': '+e.message))
  p.on('response',r=>{ if(r.status()>=500) errs.push(w+' '+r.status()+' '+r.url()) }) }

;(async()=>{
const br=await chromium.launch({ignoreHTTPSErrors:true})
const c1=await br.newContext({ignoreHTTPSErrors:true}), c2=await br.newContext({ignoreHTTPSErrors:true})
const h=await c1.newPage(), g=await c2.newPage()
watch(h,'HOST'); watch(g,'GUEST')
const hn='h'+Math.floor(Math.random()*99999), gn='g'+Math.floor(Math.random()*99999)

console.log('1. host: pick a team as a new visitor, then register when asked')
await h.goto(B+'/',{waitUntil:'networkidle'})
for(const n of ['pikachu','onix','gengar']) await h.click('#card-'+n)   // no waits
await h.waitForTimeout(2500)
ok(await h.locator('.slot.filled').count()===3, 'three slots filled after rapid clicks')
await h.click('#ready'); await h.waitForTimeout(1500)
ok(await h.locator('#name-dialog').count()>0, 'asked for a name')
await h.fill('#trainer-name',hn); await h.click('#name-dialog button[type=submit]')
await h.waitForURL(/\/battle\//,{timeout:12000})
const id=new URL(h.url()).pathname.split('/').pop()
ok(true,'landed on battle '+id)
ok(!/Pick your team/.test(await h.locator('body').innerText()), 'not asked for a team again')

console.log('2. guest: register in the lobby, then join from it')
await g.goto(B+'/battle',{waitUntil:'networkidle'})
await g.fill('#name',gn); await g.click('#reg'); await g.waitForTimeout(1800)
ok(/you are/.test(await g.locator('body').innerText()), 'registered')
await g.waitForSelector('#w-'+id,{timeout:12000})
await g.click('#w-'+id+' a.btn'); await g.waitForLoadState('networkidle')
ok(new URL(g.url()).search.includes('join='+id), 'on the pick page for that battle')
for(const n of ['charizard','blastoise','venusaur']) await g.click('#card-'+n)
await g.waitForTimeout(2500)
ok(await g.locator('.slot.filled').count()===3, 'guest got three slots')
await g.click('#ready'); await g.waitForTimeout(3000)
ok(g.url().includes('/battle/'+id), 'guest joined the battle')

console.log('3. both boards are live')
await h.waitForTimeout(2000)
const hb=(await h.locator('#banner').textContent()).trim()
ok(!/waiting for an opponent/.test(hb), 'host sees the join: '+JSON.stringify(hb))

console.log('4. take a turn')
if(await h.locator('#go').count()){ await h.click('#go'); await h.waitForTimeout(2000) }
ok(/waiting on/.test((await h.locator('#banner').textContent()).trim()), 'turn passed to the guest')

console.log('5. host wanders back to the pokedex mid-game, then returns')
await h.goto(B+'/',{waitUntil:'networkidle'})
const back=await h.locator('a.battle-link[href="/battle/'+id+'"]').count()
ok(back>0,'the pokedex offers a way back to the battle')
if(back){ await h.click('a.battle-link[href="/battle/'+id+'"]'); await h.waitForTimeout(2000) }
ok(h.url().endsWith('/battle/'+id), 'back on the battle')
ok(await h.locator('.mon').count()===6, 'the board is intact')

console.log('\nERRORS:', errs.length?errs:'none')
await br.close(); process.exit(failed||errs.length?1:0)
})().catch(e=>{console.log('THREW:',e.message); console.log(errs); process.exit(1)})
