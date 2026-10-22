import assert from 'node:assert/strict';
import { readFile,writeFile } from 'node:fs/promises';
const origin=process.env.R13_BROWSER_ORIGIN??'http://127.0.0.1:14173';
const canary='r13-private-canary@example.test';
const checks=[];
const ready=await fetch(origin+'/health/ready');assert.equal(ready.status,503,'run after the isolated E2E backend has stopped');
for(const [bytes,status,code]of [[40*1024,503,'unavailable'],[1024*1024+1,413,'payload_too_large']]){
 const res=await fetch(origin+'/api/v1/users?email='+canary,{method:'POST',headers:{Cookie:'session='+canary,'X-Contact':canary},body:'x'.repeat(bytes)});
 const json=await res.json();assert.equal(res.status,status);assert.equal(json.code,code);assert.deepEqual(Object.keys(json).sort(),['code','field_errors','message','request_id']);assert.match(json.request_id,/^[a-f0-9]{32}$/);assert.equal(res.headers.get('X-Request-ID'),json.request_id);checks.push({bytes,status,code});
}
const page=await fetch(origin+'/');assert.equal(page.status,200);assert.ok((await page.text()).includes('<div id="root">'));for(const name of ['content-security-policy','x-frame-options','x-content-type-options','referrer-policy'])assert.ok(page.headers.get(name));
const logs=await readFile('.cache/r13/proxy/access.log','utf8');assert.ok(!logs.includes(canary));for(const line of logs.trim().split(/\r?\n/)){const row=JSON.parse(line);for(const name of ['url','path','query','cookie','headers','body','email','password','token'])assert.ok(!(name in row));}
await writeFile('.cache/r13/proxy-report.json',JSON.stringify({passed:true,origin,checks,static_frontend_and_security_headers:true,logs_without_canary_or_private_fields:true},null,2));
console.log('Proxy contract, 1 MiB limit, request IDs, static assets, headers and private-log exclusion passed.');
