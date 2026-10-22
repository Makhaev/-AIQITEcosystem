import { spawn } from 'node:child_process';
import { createWriteStream } from 'node:fs';
import { mkdir,readFile,writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { createServer } from 'node:net';
import { setTimeout as sleep } from 'node:timers/promises';
if(process.env.R13_TEST_MODE!=='synthetic'||!process.env.R13_ADMIN_URL)throw Error('Set R13_TEST_MODE=synthetic and R13_ADMIN_URL');
// The native/Compose reverse proxy must already route 14173 -> 18180.
await new Promise((done,reject)=>{const socket=createServer();socket.on('error',reject);socket.listen(18180,'127.0.0.1',()=>socket.close(done));});
const dir=resolve('.cache/r13/e2e-'+Date.now());await mkdir(dir,{recursive:true});
const exe=process.platform==='win32'?'.exe':'';
const fixtureTool=resolve('.cache/r13/fixture'+exe),adminTool=resolve('.cache/r13/admin'+exe),serverTool=resolve(process.env.R13_SERVER_EXE??('.cache/r13/server-final'+exe));
const fixturePath=resolve(dir,'fixture.json');
function run(command,args,env=process.env){return new Promise((res,rej)=>{const child=spawn(command,args,{env,windowsHide:true,stdio:'inherit'});child.once('error',rej);child.once('exit',code=>code===0?res(code):rej(Error(`Command failed (${code}): ${command}`)));});}
let server;
const report={status:'failed',run_directory:dir,started_at:new Date().toISOString()};
try{
 await run(fixtureTool,['create-e2e',fixturePath]);const f=JSON.parse(await readFile(fixturePath,'utf8'));report.database=f.database;
 const env={...process.env,DATABASE_URL:f.database_url,APP_ORIGIN:'http://127.0.0.1:14173',HTTP_ADDR:'127.0.0.1:18180'};
 await run(adminTool,[],{...env,ADMIN_EMAIL:'admin@example.test',ADMIN_NAME:'R13 Admin',ADMIN_PASSWORD:f.password});
 const out=createWriteStream(resolve(dir,'backend.stdout.log')),err=createWriteStream(resolve(dir,'backend.stderr.log'));
 server=spawn(serverTool,[],{env,windowsHide:true,stdio:['ignore','pipe','pipe']});server.stdout.pipe(out);server.stderr.pipe(err);
 for(let i=0;i<100;i++){try{if((await fetch('http://127.0.0.1:18180/health/ready')).ok)break;}catch{}if(i===99)throw Error('Backend not ready');await sleep(100);}
 await run(process.execPath,['frontend/node_modules/@playwright/test/cli.js','test','--config','frontend/playwright.r13.config.ts'],{...process.env,R13_FIXTURE:fixturePath});
 report.status='passed';
}catch(e){report.failure=e.message;process.exitCode=1;}
finally{if(server){server.kill();await new Promise(done=>server.once('exit',done));}report.finished_at=new Date().toISOString();await writeFile('.cache/r13/e2e-run.json',JSON.stringify(report,null,2));}
