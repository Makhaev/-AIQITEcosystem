// Reproducible access-control and migration evidence; never stores response bodies or credentials.
import { spawn,execFileSync } from 'node:child_process';
import { readdir,readFile,writeFile,mkdir } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { resolve } from 'node:path';
import { createInterface } from 'node:readline';
if(process.env.STAGE8_TEST_MODE!=='synthetic'||!process.env.TEST_DATABASE_URL)throw Error('Explicit STAGE8_TEST_MODE=synthetic and TEST_DATABASE_URL required');
let db;
try{db=new URL(process.env.TEST_DATABASE_URL);}catch{throw Error('Invalid TEST_DATABASE_URL; value omitted');}
if(!['postgres:','postgresql:'].includes(db.protocol))throw Error('PostgreSQL URL required');
if(process.env.TEST_DATABASE_URL===process.env.DATABASE_URL||(['localhost','127.0.0.1','[::1]'].includes(db.hostname)&&db.port==='55432'))throw Error('Refusing the running development database');
const tests=[
 'TestCompletionStudentProjectionByObjectAndCombinedRoles',
 'TestCompletionNestedStudentVisibilityAndFrozenRoster',
 'TestCompletionTaskObjectPermissionsAndWithdrawal',
 'TestCompletionAssignmentWaitsForRelationshipChange',
 'TestTaskAssignmentAccessContractAndAtomicity',
 'TestTaskTeacherWithdrawalAndAccountBlocking',
 'TestTaskAssignmentConcurrentRevocation',
 'TestTaskAssignmentRechecksAuthorAfterWaiting',
 'TestTaskAssignmentAuditFailureRollsBack',
 'TestArchitectureHTTPHandlersUseServices',
 'TestArchitectureMigrationPreservesParticipantsMarksAndHistory',
];
async function sources(){
 const files=[];
 async function walk(dir){for(const e of await readdir(dir,{withFileTypes:true})){const p=`${dir}/${e.name}`;if(e.isSymbolicLink())throw Error('Source symlink refused');if(e.isDirectory())await walk(p);else if(/\.(go|sql|mod|sum)$/.test(p))files.push({path:p,sha256:createHash('sha256').update(await readFile(p)).digest('hex')});}}
 await walk('backend');files.sort((a,b)=>a.path<b.path?-1:a.path>b.path?1:0);return files;
}
const sourceFiles=await sources(),fingerprint=files=>createHash('sha256').update(JSON.stringify(files)).digest('hex');
const git=(...args)=>execFileSync('git',['-c',`safe.directory=${process.cwd().replaceAll('\\','/')}`,...args],{encoding:'utf8'}).trim();
const go=process.env.GO_BIN??'go',args=['test','-count=1','-json','./internal/app','-run',`^(${tests.join('|')})$`];
const report={started_at:new Date().toISOString(),status:'failed',base_commit:git('rev-parse','HEAD'),branch:git('branch','--show-current'),working_tree_dirty:!!git('status','--porcelain'),source_sha256:fingerprint(sourceFiles),source_files:sourceFiles,command:['go',...args],go_version:execFileSync(go,['version'],{encoding:'utf8'}).trim(),test_database:{host:db.hostname,port:db.port||'5432',database:db.pathname.slice(1),isolation:'random aiq_test_* schemas; cleanup after tests'},expected_tests:tests,results:[],package_results:[]};
await mkdir('.cache/stage8',{recursive:true});await mkdir('docs/reports',{recursive:true});
let diagnostics='',invalidJSON=false;
const child=spawn(go,args,{cwd:resolve('backend'),env:process.env,windowsHide:true,stdio:['ignore','pipe','pipe']});
child.stderr.on('data',chunk=>{diagnostics+=chunk;});
const lines=createInterface({input:child.stdout});
lines.on('line',line=>{try{const e=JSON.parse(line);if(['pass','skip','fail'].includes(e.Action)){const result={name:e.Test??e.Package,result:e.Action,elapsed_seconds:e.Elapsed??null};if(e.Test){report.results.push(result);if(!e.Test.includes('/'))console.log(`${e.Action.toUpperCase()} ${e.Test}`);}else report.package_results.push(result);}}catch{invalidJSON=true;}});
const code=await new Promise(done=>{child.once('error',e=>{diagnostics+=e.code??'spawn error';done(-1);});child.once('close',done);});
report.exit_code=code;report.finished_at=new Date().toISOString();
report.source_unchanged=fingerprint(await sources())===report.source_sha256;
report.top_level_passed=report.results.filter(r=>!r.name.includes('/')&&r.result==='pass').length;
report.failed=report.results.filter(r=>r.result==='fail').length;
report.skipped=report.results.filter(r=>r.result==='skip').length;
if(code===0&&!invalidJSON&&report.source_unchanged&&tests.every(name=>report.results.some(r=>r.name===name&&r.result==='pass'))&&report.failed===0&&report.skipped===0)report.status='passed';
await writeFile('.cache/stage8/p0-diagnostics.log',diagnostics);
await writeFile('docs/reports/stage8-p0-migration.json',JSON.stringify(report,null,2)+'\n');
console.log(`Report: docs/reports/stage8-p0-migration.json; ${report.status}; ${report.top_level_passed}/${tests.length} top-level tests.`);
if(report.status!=='passed')process.exitCode=1;
