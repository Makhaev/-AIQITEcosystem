import { readFile,access,readdir } from 'node:fs/promises';
import { resolve,dirname } from 'node:path';
import { createHash } from 'node:crypto';
const path='docs/execution-report.md',source=await readFile(path,'utf8');
const allowed=new Set(['не начато','в работе','реализовано — проверка не выполнена','проверено','принято']);
const rows=source.split(/\r?\n/).filter(line=>/^\| R\d{2} /.test(line));
if(rows.length!==14)throw Error('Expected exactly 14 requirement rows');
const seen=new Set();
for(const row of rows){
 const cells=row.split('|').slice(1,-1).map(s=>s.trim());
 if(cells.length!==7||cells.some(c=>!c))throw Error('Seven nonempty columns required');
 const id=cells[0].slice(0,3);
 if(seen.has(id)||!/^R(?:0[1-9]|1[0-4])$/.test(id))throw Error('Invalid or duplicate requirement ID');
 seen.add(id);
 if(!allowed.has(cells[6]))throw Error(`Invalid status: ${id}`);
 if(!/\[[^\]]+\]\([^)]+\)/.test(cells[4]))throw Error(`Evidence link required: ${id}`);
 if(cells[6]==='принято'){
  const acceptance=JSON.parse(await readFile('docs/reports/customer-acceptance.json','utf8'));
  if(!acceptance.customer||!acceptance.date||!acceptance.commit_sha?.match(/^[a-f0-9]{40}$/)||!acceptance.requirements?.includes(id)||!acceptance.decision_reference)throw Error(`Customer acceptance evidence required for ${id}`);
 }
}
let links=0;
for(const m of source.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)){
 if(/^(?:https?:|#)/.test(m[1]))continue;
 await access(resolve(dirname(path),decodeURIComponent(m[1].split('#')[0])));links++;
}
const p0=JSON.parse(await readFile('docs/reports/stage8-p0-migration.json','utf8'));
if(p0.status!=='passed'||!p0.source_unchanged||p0.skipped!==0||p0.failed!==0||p0.top_level_passed!==p0.expected_tests.length)throw Error('P0/migration evidence incomplete');
const files=[];
async function snapshot(dir){for(const e of await readdir(dir,{withFileTypes:true})){const p=`${dir}/${e.name}`;if(e.isSymbolicLink())throw Error('Source symlink refused');if(e.isDirectory())await snapshot(p);else if(/\.(go|sql|mod|sum)$/.test(p))files.push({path:p,sha256:createHash('sha256').update(await readFile(p)).digest('hex')});}}
await snapshot('backend');files.sort((a,b)=>a.path<b.path?-1:a.path>b.path?1:0);
if(createHash('sha256').update(JSON.stringify(files)).digest('hex')!==p0.source_sha256)throw Error('Backend sources changed since P0/migration evidence; rerun verify-p0.mjs');
console.log(`14 requirements, 7 columns, allowed statuses, ${links} local links and P0/migration report verified. This is not customer acceptance or CI approval.`);
