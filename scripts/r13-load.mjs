// HTTP timing on the generator, including response download over loopback/LAN;
// no browser, no API mocks. Defaults are the acceptance 5 + 15 minute profile.
import { readFile, writeFile } from 'node:fs/promises';
import { performance } from 'node:perf_hooks';
import { setTimeout as sleep } from 'node:timers/promises';
import { createHash } from 'node:crypto';

const [configPath, output] = process.argv.slice(2);
if (process.env.R13_TEST_MODE !== 'synthetic' || !configPath || !output) throw Error('Explicit R13_TEST_MODE=synthetic, fixture config and report path required');
const fixture = JSON.parse(await readFile(configPath, 'utf8'));
if (fixture.kind !== 'load' || !/^aiq_r13_load_[a-f0-9]+$/.test(fixture.database) || fixture.users?.length !== 20) throw Error('Refusing non-load fixture');
const origin = process.env.R13_ORIGIN;
if (!origin) throw Error('R13_ORIGIN required');
const warmup = Number(process.env.R13_WARMUP_SECONDS ?? 300), measurement = Number(process.env.R13_MEASURE_SECONDS ?? 900);
if (!(warmup >= 0 && measurement > 0)) throw Error('Invalid durations');
const report = { status: 'running', started_at: new Date().toISOString(), fixture: fixture.database, users: 20,
  warmup_seconds: warmup, measurement_seconds: measurement, full_profile: warmup === 300 && measurement === 900,
  generator: { runtime: process.version, platform: process.platform, origin, timing: 'performance.now around API request and complete response, generator host; no browser' },
  distribution: { list: 50, card: 30, overview: 10, dashboard: 10 }, endpoints: {}, errors: [], samples: [] };
const records = new Map();
let sampleTimer;
function rng(seed) { let s=seed;return ()=>{s=(s*1664525+1013904223)>>>0;return s/4294967296;}; }
function client(account) {
  const cookies = new Map(); let csrf;
  async function request(path, method='GET', body) {
    const start=performance.now(); let response;
    try {
      response=await fetch(origin+'/api/v1'+path,{method,signal:AbortSignal.timeout(15000),headers:{Cookie:[...cookies].map(([k,v])=>`${k}=${v}`).join('; '),Origin:origin,'Content-Type':'application/json',...(csrf?{'X-CSRF-Token':csrf}:{})},body:body===undefined?undefined:JSON.stringify(body)});
      for(const value of response.headers.getSetCookie()){const pair=value.split(';')[0],i=pair.indexOf('=');cookies.set(pair.slice(0,i),pair.slice(i+1));}
      const text=await response.text(); const elapsed=performance.now()-start;
      let data;try{data=JSON.parse(text);}catch{}
      if(data?.csrf_token)csrf=data.csrf_token;
      return {status:response.status,data,elapsed,privateLeak: /"(?:password_hash|token_hash|csrf_hash)"\s*:/.test(text) || text.includes(fixture.password)};
    }catch{return {status:0,elapsed:performance.now()-start};}
  }
  return {account,request};
}
try {
 const clients=[];
 for(const account of fixture.users){
  const c=client(account);await c.request('/auth/csrf');
  if((await c.request('/auth/login','POST',{email:account.email,password:fixture.password})).status!==200)throw Error('Fixture login failed');
  const me=await c.request('/auth/me');if(!me.data?.roles?.includes(account.role))throw Error('Fixture role mismatch');
  c.lists=[];c.cards=[];
  for(const type of ['students','groups','lessons','programs','tasks']){
   const res=await c.request('/'+type+'?page_size=100');if(res.status!==200 || !res.data?.items?.length)throw Error(`Fixture catalog unavailable: ${type}/${account.role}`);
   c.lists.push({key:`GET /${type}`,path:`/${type}?page_size=20`});
   for(const item of res.data.items)c.cards.push({key:`GET /${type}/{id}`,path:`/${type}/${item.id}`});
   if(type==='students')c.students=res.data.items.map(x=>x.id);
  }
  c.cycle=account.role==='teacher'?'LLLLLCCCCC':account.role==='curator'?'LLLLLCCCOO':'LLLLLCCODD';
  clients.push(c);
 }
 const start=performance.now(), measuredStart=start+warmup*1000,end=measuredStart+measurement*1000;
 report.measurement_started_at=new Date(Date.now()+warmup*1000).toISOString();
 const flush=async()=>{await writeFile(output,JSON.stringify({...report,completed_requests:[...records.values()].reduce((n,r)=>n+r.times.length,0)},null,2));};
 sampleTimer=setInterval(()=>{console.log(`R13 load: ${Math.round((performance.now()-start)/1000)}s / ${warmup+measurement}s`);},30000);
 await Promise.all(clients.map(async(c,index)=>{
  const random=rng(13000+index);let iteration=index%10;
  while(performance.now()<end){
   const category=c.cycle[iteration++%10];let target;
   if(category==='L'){target=c.lists[Math.floor(random()*c.lists.length)];target={...target,path:target.path+'&page='+(1+Math.floor(random()*3))};}
   if(category==='C')target=c.cards[Math.floor(random()*c.cards.length)];
   if(category==='O')target={key:'GET /students/{id}/overview',path:`/students/${c.students[Math.floor(random()*c.students.length)]}/overview`};
   if(category==='D')target={key:'GET /dashboard',path:'/dashboard'};
   const began=performance.now(), result=await c.request(target.path);
   if(result.status!==200 || result.privateLeak){report.errors.push({endpoint:target.key,role:c.account.role,status:result.status,phase:began<measuredStart?'warmup':'measurement',secret_leak:!!result.privateLeak});}
   if(began>=measuredStart && began<end){
    const record=records.get(target.key)??{times:[],errors:0,roles:{}};record.times.push(result.elapsed);record.roles[c.account.role]=(record.roles[c.account.role]??0)+1;
    if(result.status!==200 || result.privateLeak)record.errors++;records.set(target.key,record);
   }
   await sleep(Math.min(1000+random()*2000,Math.max(0,end-performance.now())));
  }
 }));
 for(const [key,r]of records){r.times.sort((a,b)=>a-b);const q=p=>r.times[Math.max(0,Math.ceil(r.times.length*p)-1)];const limit=/overview|dashboard/.test(key)?2000:1000;report.endpoints[key]={requests:r.times.length,p50_ms:q(.5),p95_ms:q(.95),p99_ms:q(.99),errors:r.errors,roles:r.roles,limit_ms:limit,passed:r.errors===0&&q(.95)<=limit};}
 report.status=report.errors.length===0&&Object.values(report.endpoints).length===12&&Object.values(report.endpoints).every(r=>r.passed)?'passed':'failed';
 report.finished_at=new Date().toISOString();
 await flush();
}catch(error){report.status='failed';report.failure=error.message;await writeFile(output,JSON.stringify(report,null,2));process.exitCode=1;}
finally{clearInterval(sampleTimer);}
if(report.status!=='passed')process.exitCode=1;
