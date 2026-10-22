// Real Go + PostgreSQL + browser validation in an isolated schema.
import {spawn, execFileSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
import {mkdirSync, openSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const root=resolve(fileURLToPath(new URL('..',import.meta.url)));
const cache=resolve(root,'.cache/m2');
mkdirSync(cache,{recursive:true});
if(!process.env.TEST_DATABASE_URL) throw Error('Set TEST_DATABASE_URL to a local test PostgreSQL database.');
const env={...process.env,GOCACHE:resolve(root,'.cache/go-build'),GOMODCACHE:resolve(root,'.cache/go-mod'),M2_TEST_PASSWORD:randomBytes(24).toString('hex')};
const exe=process.platform==='win32'?'.exe':'';
const fixture=resolve(cache,`fixture${exe}`), server=resolve(cache,`server${exe}`);
execFileSync('go',['build','-buildvcs=false','-o',fixture,'./cmd/m2-fixture'],{cwd:resolve(root,'backend'),env,stdio:'inherit',windowsHide:true});
execFileSync('go',['build','-buildvcs=false','-o',server,'./cmd/server'],{cwd:resolve(root,'backend'),env,stdio:'inherit',windowsHide:true});
const seeded=JSON.parse(execFileSync(fixture,['init'],{env,encoding:'utf8',windowsHide:true}));
const children=[];
function launch(cmd,args,name,extra={}) {
 const child=spawn(cmd,args,{cwd:resolve(root,'frontend'),env:{...env,...extra},windowsHide:true,stdio:['ignore',openSync(resolve(cache,`${name}.stdout.log`),'w'),openSync(resolve(cache,`${name}.stderr.log`),'w')]});
 children.push(child); return child;
}
async function ready(url) {
 for(let i=0;i<100;i++) { try { if((await fetch(url)).ok) return; } catch {} await new Promise(r=>setTimeout(r,100)); }
 throw Error(`Server unavailable: ${url}`);
}
try {
 launch(server,[],'e2e-backend',{DATABASE_URL:seeded.database_url,HTTP_ADDR:'127.0.0.1:18080',APP_ORIGIN:'http://127.0.0.1:15173',TRUST_PROXY:'false'});
 const viteConfig=resolve(cache,'vite-preview.config.mjs');
 writeFileSync(viteConfig,`export default ${JSON.stringify({root:resolve(root,'frontend'),preview:{host:'127.0.0.1',port:15173,strictPort:true,proxy:{'/api':'http://127.0.0.1:18080','/health':'http://127.0.0.1:18080'}}})};\n`);
 launch(process.execPath,['node_modules/vite/bin/vite.js','preview','--config',viteConfig],'e2e-frontend');
 await Promise.all([ready('http://127.0.0.1:18080/health/ready'),ready('http://127.0.0.1:15173')]);
 const config=resolve(cache,'playwright-m2.config.mjs');
 writeFileSync(config,`export default ${JSON.stringify({testDir:resolve(root,'frontend/e2e'),testMatch:'m2.spec.ts',workers:1,fullyParallel:false,timeout:180000,reporter:[['list'],['json',{outputFile:resolve(cache,'e2e-report.json')}]],outputDir:resolve(cache,'browser-artifacts'),use:{baseURL:'http://127.0.0.1:15173',browserName:'chromium',trace:'off',launchOptions:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH?{executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH}:{}},projects:[{name:'desktop',use:{viewport:{width:1440,height:1000}}},{name:'mobile-360',use:{viewport:{width:360,height:800}}}]})};\n`);
 const code=await new Promise((done,reject)=>{
  const child=spawn(process.execPath,['node_modules/@playwright/test/cli.js','test','--config',config],{cwd:resolve(root,'frontend'),env:{...env,M2_TEACHER_ID:String(seeded.teacher_id)},stdio:'inherit',windowsHide:true});
  child.on('error',reject); child.on('exit',done);
 });
 if(code!==0) process.exitCode=code||1;
} finally {
 for(const child of children.reverse()) { if(child.exitCode===null) { child.kill(); await new Promise(resolve=>{child.once('exit',resolve);setTimeout(resolve,2000);}); } }
 execFileSync(fixture,['cleanup'],{env:{...env,M2_SCHEMA:seeded.schema},stdio:'inherit',windowsHide:true});
}
