param(
  [Parameter(Mandatory=$true)][string]$RunDir,
  [Parameter(Mandatory=$true)][string]$PgBin,
  [Parameter(Mandatory=$true)][string]$BackendExe,
  [int]$DatabasePort=55436,
  [int]$ApiPort=18183
)
$ErrorActionPreference='Stop'
$runPath=(Resolve-Path -LiteralPath $RunDir).Path
$pgPath=(Resolve-Path -LiteralPath $PgBin).Path
$apiPath=(Resolve-Path -LiteralPath $BackendExe).Path
if (!(Test-Path -LiteralPath (Join-Path $runPath 'pg-data/PG_VERSION'))) { throw 'Initialize the isolated pg-data first' }
if (Test-Path -LiteralPath (Join-Path $runPath 'host.json')) { throw 'Choose a new run directory; refusing an existing host' }
Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Runtime.InteropServices;
using System.Diagnostics;
public static class R13Job {
 [StructLayout(LayoutKind.Sequential)] struct Basic {public long ProcessTime,JobTime;public uint Flags;public UIntPtr MinWS,MaxWS;public uint Active;public UIntPtr Affinity;public uint Priority,Scheduling;}
 [StructLayout(LayoutKind.Sequential)] struct IO {public ulong ROp,WOp,OOp,RBytes,WBytes,OBytes;}
 [StructLayout(LayoutKind.Sequential)] struct Limits {public Basic Basic;public IO IO;public UIntPtr ProcessMemory,JobMemory,PeakProcess,PeakJob;}
 [StructLayout(LayoutKind.Sequential)] struct SA {public int Length;public IntPtr Descriptor;public int Inherit;}
 [StructLayout(LayoutKind.Sequential,CharSet=CharSet.Unicode)] struct Startup {public int Size;public string Reserved,Desktop,Title;public uint X,Y,XS,YS,XC,YC,Fill,Flags;public short Show,Reserved2;public IntPtr ReservedPtr,In,Out,Err;}
 [StructLayout(LayoutKind.Sequential)] struct PI {public IntPtr Process,Thread;public uint PID,TID;}
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern IntPtr CreateJobObject(IntPtr sa,string name);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool SetInformationJobObject(IntPtr job,int info,ref Limits limits,uint length);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool QueryInformationJobObject(IntPtr job,int info,IntPtr data,uint length,IntPtr returned);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool AssignProcessToJobObject(IntPtr job,IntPtr process);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern bool CreateProcess(string app,StringBuilder command,IntPtr pa,IntPtr ta,bool inherit,uint flags,IntPtr env,string cwd,ref Startup start,out PI pi);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern IntPtr CreateFile(string path,uint access,uint share,ref SA sa,uint mode,uint attr,IntPtr template);
 [DllImport("kernel32.dll")] static extern uint ResumeThread(IntPtr thread);
 [DllImport("kernel32.dll")] public static extern bool CloseHandle(IntPtr handle);
 [DllImport("kernel32.dll")] static extern bool TerminateProcess(IntPtr process,uint code);
 static Exception Error(){return new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error());}
 public static IntPtr Create(){
  var j=CreateJobObject(IntPtr.Zero,null);if(j==IntPtr.Zero)throw Error();
  var l=new Limits();l.Basic.Flags=0x2000|0x200|0x10;l.Basic.Affinity=new UIntPtr(3);l.JobMemory=new UIntPtr(4UL*1024*1024*1024);
  if(!SetInformationJobObject(j,9,ref l,(uint)Marshal.SizeOf(l)))throw Error();return j;
 }
 public static uint Start(IntPtr job,string exe,string args,string cwd,string output,string error){
  var sa=new SA{Length=Marshal.SizeOf(typeof(SA)),Inherit=1};
  var o=CreateFile(output,0x40000000,3,ref sa,2,0x80,IntPtr.Zero);var e=CreateFile(error,0x40000000,3,ref sa,2,0x80,IntPtr.Zero);
  if(o.ToInt64()==-1||e.ToInt64()==-1)throw Error();
  var s=new Startup{Size=Marshal.SizeOf(typeof(Startup)),Flags=0x100,Out=o,Err=e,In=IntPtr.Zero};PI p;
  try{
   if(!CreateProcess(exe,new StringBuilder("\""+exe+"\" "+args),IntPtr.Zero,IntPtr.Zero,true,0x08000004,IntPtr.Zero,cwd,ref s,out p))throw Error();
   if(!AssignProcessToJobObject(job,p.Process)){TerminateProcess(p.Process,1);CloseHandle(p.Thread);CloseHandle(p.Process);throw Error();}
   ResumeThread(p.Thread);CloseHandle(p.Thread);CloseHandle(p.Process);return p.PID;
  }finally{CloseHandle(o);CloseHandle(e);}
 }
 public static long[] Usage(IntPtr job){
  var data=Marshal.AllocHGlobal(65536);
  try{
   if(!QueryInformationJobObject(job,1,data,48,IntPtr.Zero))throw Error();
   long cpu=Marshal.ReadInt64(data,0)+Marshal.ReadInt64(data,8);
   if(!QueryInformationJobObject(job,3,data,65536,IntPtr.Zero))throw Error();
   int count=Marshal.ReadInt32(data,4);long ws=0,priv=0;
   for(int i=0;i<count;i++){try{using(var p=Process.GetProcessById((int)Marshal.ReadIntPtr(data,8+i*IntPtr.Size))){ws+=p.WorkingSet64;priv+=p.PrivateMemorySize64;}}catch(ArgumentException){}catch(InvalidOperationException){}}
   return new long[]{cpu,ws,priv,count};
  }finally{Marshal.FreeHGlobal(data);}
 }
}
'@
$job=[R13Job]::Create()
$encoding=[Text.UTF8Encoding]::new($false)
$samples=[IO.StreamWriter]::new((Join-Path $runPath 'resources.jsonl'),$false,$encoding)
try {
  $databasePID=[R13Job]::Start($job,(Join-Path $pgPath 'postgres.exe'),('-D "'+(Join-Path $runPath 'pg-data')+'" -h 127.0.0.1 -p '+$DatabasePort),$runPath,(Join-Path $runPath 'postgres.stdout.log'),(Join-Path $runPath 'postgres.stderr.log'))
  $info=@{kind='Windows Job Object';affinity_mask=3;logical_processors=2;job_commit_limit_bytes=4294967296;database_pid=$databasePID;database_port=$DatabasePort;api_port=$ApiPort;generator_outside_job=$true;host=[Environment]::MachineName;os=[Environment]::OSVersion.VersionString}
  $info|ConvertTo-Json|Set-Content -Encoding UTF8 (Join-Path $runPath 'host.json')
  $apiStarted=$false
  while (!(Test-Path -LiteralPath (Join-Path $runPath 'stop'))) {
    $fixturePath=Join-Path $runPath 'fixture.json'
    if (!$apiStarted -and (Test-Path -LiteralPath (Join-Path $runPath 'start-api'))) {
      $fixture=Get-Content -Raw -LiteralPath $fixturePath|ConvertFrom-Json
      if ($fixture.kind -ne 'load' -or $fixture.database -notmatch '^aiq_r13_load_[a-f0-9]+$') {throw 'Not a synthetic load fixture'}
      $env:DATABASE_URL=$fixture.database_url;$env:HTTP_ADDR='127.0.0.1:'+$ApiPort;$env:APP_ORIGIN='http://127.0.0.1:'+$ApiPort
      $info.backend_pid=[R13Job]::Start($job,$apiPath,'',$runPath,(Join-Path $runPath 'backend.stdout.log'),(Join-Path $runPath 'backend.stderr.log'))
      $info|ConvertTo-Json|Set-Content -Encoding UTF8 (Join-Path $runPath 'host.json');$apiStarted=$true
    }
    $usage=[R13Job]::Usage($job)
    $samples.WriteLine((@{at=[DateTime]::UtcNow.ToString('o');cpu_ticks=$usage[0];working_set_bytes=$usage[1];private_bytes=$usage[2];processes=$usage[3]}|ConvertTo-Json -Compress));$samples.Flush()
    Start-Sleep -Seconds 1
  }
}finally{$samples.Dispose();[void][R13Job]::CloseHandle($job)}
