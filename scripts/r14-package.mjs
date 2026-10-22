// Local source delivery snapshot, including uncommitted work; no git push/tag/release.
import { readdir,readFile,writeFile,mkdir,lstat,copyFile } from 'node:fs/promises';
import { resolve,relative,extname } from 'node:path';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
const root=process.cwd();
const git=(...args)=>execFileSync('git',['-c',`safe.directory=${root.replaceAll('\\','/')}`,...args],{encoding:'utf8'}).trim();
const hash=data=>createHash('sha256').update(data).digest('hex');
const output=resolve('.cache/handover',new Date().toISOString().replaceAll(':','-'));
await mkdir(output,{recursive:true});
const stage=resolve(output,'aiq-platform-v0.1.0-candidate');await mkdir(stage);
const excluded=[];const files=[];
const ignored=new Set(['.git','.cache','node_modules','dist','test-results','playwright-report','backups']);
const allowed=new Set(['.go','.mod','.sum','.sql','.ts','.tsx','.js','.mjs','.css','.html','.json','.yaml','.yml','.md','.conf','.template','.sh','.ps1','.service','.example','.svg','.png','.pdf','.docx']);
async function collect(path){
 const info=await lstat(resolve(root,path));
 if(info.isSymbolicLink())throw Error(`Refusing symlink: ${path}`);
 const name=path.split('/').at(-1);
 if(info.isDirectory()){
  if(ignored.has(name)){excluded.push(path+'/');return;}
  for(const entry of (await readdir(resolve(root,path))).sort())await collect(`${path}/${entry}`);
  return;
 }
 if(!info.isFile())throw Error(`Refusing non-regular file: ${path}`);
 if((name.startsWith('.env') && name!=='.env.example') || /(?:^|\.)(?:key|pem|p12|pfx|pgpass|dump|log|exe)$/i.test(name) || (!allowed.has(extname(name)) && !['Dockerfile','.dockerignore','.gitignore','.gitattributes'].includes(name) && !name.endsWith('.Dockerfile'))){excluded.push(path);return;}
 const data=await readFile(resolve(root,path));
 if(/\.(?:go|ts|tsx|js|mjs|json|yaml|yml|md|conf|sh|ps1|example)$/.test(name)){
  // Known credential formats only; this is not a substitute for human secret review.
  if(/-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{50,}/.test(data.toString('utf8')))throw Error(`Credential pattern in ${path}; package refused`);
 }
 const target=resolve(stage,path);await mkdir(resolve(target,'..'),{recursive:true});await copyFile(resolve(root,path),target);
 files.push({path,bytes:data.length,sha256:hash(data)});
}
for(const dir of ['backend','frontend','docs','deploy','scripts','.github'])await collect(dir);
for(const entry of (await readdir(root)).sort())if(['README.md','.env.example','.gitignore','.gitattributes'].includes(entry)||/^compose(?:\.[a-z-]+)?\.ya?ml$/.test(entry))await collect(entry);
files.sort((a,b)=>a.path.localeCompare(b.path,'en'));
for(const required of ['README.md','.env.example','compose.yaml','backend/Dockerfile','frontend/Dockerfile','docs/openapi.yaml','docs/database-schema.md','docs/handover-r14.md','docs/acceptance-r14.md','docs/execution-report.md','docs/reports/stage8-p0-migration.json','docs/reports/stage8-container.json','docs/r13-validation-report.md','docs/admin-guide.md','docs/backup-r12.md','.github/workflows/ci.yml']){
 if(!files.some(f=>f.path===required))throw Error(`Missing handover item: ${required}`);
}
const manifest={format:1,kind:'unpublished candidate; not an accepted release',version_candidate:'v0.1.0',created_at:new Date().toISOString(),base_commit:git('rev-parse','HEAD'),branch:git('branch','--show-current'),working_tree_dirty:!!git('status','--porcelain'),remote:git('remote','get-url','origin'),files,excluded,limitations:'Base commit does not identify uncommitted contents; file hashes identify this snapshot. No credentials, database dumps, build outputs or Git history are included. Repository access and release approval are separate.'};
await writeFile(resolve(stage,'MANIFEST.json'),JSON.stringify(manifest,null,2)+'\n');
// Re-read the copy before archiving: detect copy/storage corruption.
for(const f of files)if(hash(await readFile(resolve(stage,f.path)))!==f.sha256)throw Error(`Copy checksum mismatch: ${f.path}`);
const archive=resolve(output,'aiq-platform-v0.1.0-candidate.tar');
execFileSync('tar',['-cf',archive,'-C',output,'aiq-platform-v0.1.0-candidate'],{stdio:'pipe'});
const checksum=hash(await readFile(archive));
await writeFile(archive+'.sha256',`${checksum}  aiq-platform-v0.1.0-candidate.tar\n`);
console.log(JSON.stringify({archive:relative(root,archive),sha256:checksum,files:files.length,manifest:relative(root,resolve(stage,'MANIFEST.json'))},null,2));
