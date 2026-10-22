// Read-only: never changes a remote, repository visibility, permissions or releases.
import { execFileSync } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
const git = (...args) => execFileSync('git', ['-c', `safe.directory=${process.cwd().replaceAll('\\','/')}`, ...args], {encoding:'utf8'}).trim();
const remote = git('remote','get-url','origin');
const match = remote.match(/^(?:https:\/\/github\.com\/|git@github\.com:)([^/]+)\/([^/]+?)(?:\.git)?$/);
if (!match) throw Error('Expected a GitHub origin without embedded credentials');
const head = git('rev-parse','HEAD');
const report = {checked_at:new Date().toISOString(), remote, head, branch:git('branch','--show-current'), working_tree_dirty:!!git('status','--porcelain'), expected_name:'aiq-platform', expected_private:true, expected_owner:process.env.R14_EXPECTED_OWNER??null, authentication:process.env.GH_TOKEN?'token supplied':'anonymous', checks:{}};
async function get(path) {
 const headers = {Accept:'application/vnd.github+json','User-Agent':'aiq-r14-read-only-audit','X-GitHub-Api-Version':'2022-11-28'};
 if(process.env.GH_TOKEN)headers.Authorization=`Bearer ${process.env.GH_TOKEN}`;
 const r = await fetch('https://api.github.com'+path,{headers,signal:AbortSignal.timeout(15000)});
 if(!r.ok)throw Error(`GitHub HTTP ${r.status}`);
 return r.json();
}
try {
 const path=`/repos/${match[1]}/${match[2]}`, repo=await get(path);
 report.repository={full_name:repo.full_name,name:repo.name,private:repo.private,owner:repo.owner.login,default_branch:repo.default_branch,archived:repo.archived};
 report.checks.name=repo.name==='aiq-platform'?'passed':'failed';
 report.checks.privacy=repo.private===true?'passed':'failed';
 report.checks.owner_access='not_verified';
 if(process.env.GH_TOKEN && report.expected_owner){
  const user=await get('/user');
  report.authenticated_login=user.login;
  report.checks.owner_access=user.login.toLowerCase()===report.expected_owner.toLowerCase() && repo.permissions?.admin===true?'passed':'failed';
 }
 try {
  const runs=await get(`${path}/actions/runs?head_sha=${head}&per_page=100`);
  report.base_head_workflow_runs=runs.workflow_runs.map(r=>({name:r.name,head_sha:r.head_sha,status:r.status,conclusion:r.conclusion,url:r.html_url}));
 }catch(e){report.workflow_lookup_error=e.message;}
 // A dirty workspace is not a remotely checked commit, even if its base has green runs.
 report.checks.current_candidate_remote_ci=report.working_tree_dirty?'not_run_for_candidate':'requires_review_of_exact_sha_runs';
}catch(e){report.error=e.message;report.checks.metadata='not_verified';}
await mkdir('docs/reports',{recursive:true});
await writeFile('docs/reports/r14-github-audit.json',JSON.stringify(report,null,2)+'\n');
console.log(JSON.stringify({report:'docs/reports/r14-github-audit.json',checks:report.checks}));
if(Object.values(report.checks).some(v=>v!=='passed'))process.exitCode=2;
