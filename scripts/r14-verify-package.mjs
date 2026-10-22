import { readFile,lstat } from 'node:fs/promises';
import { resolve,relative,isAbsolute } from 'node:path';
import { createHash } from 'node:crypto';
const input=process.argv[2];
if(!input)throw Error('usage: node scripts/r14-verify-package.mjs EXTRACTED_CANDIDATE_DIRECTORY');
const root=resolve(input), manifest=JSON.parse(await readFile(resolve(root,'MANIFEST.json'),'utf8'));
if(manifest.format!==1||!Array.isArray(manifest.files)||!manifest.files.length)throw Error('Invalid manifest');
const seen=new Set();
for(const file of manifest.files){
 if(typeof file.path!=='string'||file.path.includes('\\')||file.path.includes(':')||isAbsolute(file.path)||file.path.split('/').some(p=>!p||p==='.'||p==='..')||seen.has(file.path))throw Error('Invalid or duplicate manifest path');
 seen.add(file.path);
 const target=resolve(root,file.path), rel=relative(root,target);
 if(rel.startsWith('..')||isAbsolute(rel))throw Error('Path outside package');
 let current=root;
 for(const part of file.path.split('/')){current=resolve(current,part);if((await lstat(current)).isSymbolicLink())throw Error('Package contains symlink');}
 const data=await readFile(target);
 if(data.length!==file.bytes||createHash('sha256').update(data).digest('hex')!==file.sha256)throw Error(`Checksum mismatch: ${file.path}`);
}
console.log(`Verified ${manifest.files.length} files; base ${manifest.base_commit}; dirty snapshot: ${manifest.working_tree_dirty}. This verifies integrity, not release approval.`);
