using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Threading;

namespace CodexQuota {
 // Owns one child process and one sequential writer. UI callers only change a
 // bounded in-memory queue. No pipe write, process wait or disposal runs there.
 internal sealed class WorkerTransport {
  internal const int Capacity=32,MaxCommandBytes=60000;
  sealed class Command {internal string Line;internal bool Preference;}
  readonly object gate=new object();readonly LinkedList<Command> commands=new LinkedList<Command>();
  readonly AutoResetEvent wake=new AutoResetEvent(false);
  readonly Stopwatch clock=Stopwatch.StartNew();readonly ProcessStartInfo start;
  readonly Action<WorkerTransport,string> output,error,failure;
  readonly int writeTimeoutMs,shutdownTimeoutMs;
  System.Threading.Timer watchdog;Process process;
  volatile bool alive=true,completed,stopping;bool started,abort,reported;
  long writeDeadline,stopDeadline;volatile int processId;
  internal WorkerTransport(ProcessStartInfo start,Action<WorkerTransport,string> output,Action<WorkerTransport,string> error,Action<WorkerTransport,string> failure,int writeTimeoutMs=1000,int shutdownTimeoutMs=1500){
   this.start=start;this.output=output;this.error=error;this.failure=failure;this.writeTimeoutMs=writeTimeoutMs;this.shutdownTimeoutMs=shutdownTimeoutMs;
  }
  internal bool Alive {get{return alive&&!stopping;}}
  internal bool Completed {get{return completed;}}
  internal int ProcessId {get{return processId;}}
  internal int PendingCount {get{lock(gate)return commands.Count;}}
  internal bool Writing {get{lock(gate)return writeDeadline!=0;}}
  internal void Start(){
   lock(gate){if(started||completed)throw new InvalidOperationException("Worker already started or stopped");started=true;}
   // Foreground cleanup keeps the host alive briefly after its UI closes, so
   // the child cannot be orphaned when Application.Run returns. Watchdog bounds
   // pipe writes/shutdown; the UI itself never waits for this thread.
   var writerThread=new Thread(Run);writerThread.Name="CodexQuota worker transport";writerThread.IsBackground=false;writerThread.Start();
  }
  internal bool TrySend(string line,bool preference=false){
   if(string.IsNullOrEmpty(line)||line.Length>MaxCommandBytes||Encoding.UTF8.GetByteCount(line)>MaxCommandBytes)return false;
   lock(gate){
    if(!alive||stopping||completed)return false;
    if(preference){
     // Latest preference supersedes older UNSENT preferences. Normal commands
     // remain FIFO; reserve a slot so disabling alerts cannot lose to refreshes.
     var node=commands.First;while(node!=null){var next=node.Next;if(node.Value.Preference)commands.Remove(node);node=next;}
    }else if(commands.Count>=Capacity-1)return false;
    if(commands.Count>=Capacity)return false;
    commands.AddLast(new Command{Line=line,Preference=preference});
   }
   Signal();return true;
  }
  internal void Stop(){
   bool neverStarted=false;
   lock(gate){if(completed||stopping)return;stopping=true;stopDeadline=clock.ElapsedMilliseconds+shutdownTimeoutMs;ScheduleLocked();if(!started){alive=false;completed=true;commands.Clear();neverStarted=true;}}
   if(neverStarted){wake.Dispose();return;}
   Signal();
  }
  void Signal(){try{wake.Set();}catch(ObjectDisposedException){}}
  void Report(string message){
   bool notify;lock(gate){notify=!reported&&!stopping;reported=true;alive=false;abort=true;}
   if(notify&&failure!=null)try{failure(this,message);}catch{}
   Signal();
  }
  void Tick(object state){
   Process child;bool expired;lock(gate){long now=clock.ElapsedMilliseconds;expired=(writeDeadline!=0&&now>=writeDeadline)||(stopping&&now>=stopDeadline);child=process;if(!expired)ScheduleLocked();}
   if(!expired||child==null)return;
   Report("额度读取进程没有及时响应；已停止本次连接，将自动重试。");
   Kill(child);
  }
  void ScheduleLocked(){
   if(watchdog==null)return;long deadline=writeDeadline;
   if(stopping&&(deadline==0||stopDeadline<deadline))deadline=stopDeadline;
   watchdog.Change(deadline==0?Timeout.Infinite:(int)Math.Min(int.MaxValue,Math.Max(1,deadline-clock.ElapsedMilliseconds)),Timeout.Infinite);
  }
  static void Kill(Process child){try{if(!child.HasExited)child.Kill();}catch(InvalidOperationException){}catch(System.ComponentModel.Win32Exception){} }
  Thread Read(StreamReader reader,Action<WorkerTransport,string> receive){
   var readerThread=new Thread(delegate(){try{string line;while((line=reader.ReadLine())!=null){if(Alive&&receive!=null)try{receive(this,line);}catch{}}}catch(IOException){}catch(ObjectDisposedException){} });
   readerThread.IsBackground=true;readerThread.Name="CodexQuota worker pipe reader";readerThread.Start();return readerThread;
  }
  void Run(){
   Process child=null;StreamReader stdout=null,stderr=null;Thread outputThread=null,errorThread=null;EventHandler exited=null;
   try{
    child=new Process{StartInfo=start,EnableRaisingEvents=true};exited=delegate{Signal();};child.Exited+=exited;lock(gate){process=child;}
    child.Start();processId=child.Id;
    lock(gate){watchdog=new System.Threading.Timer(Tick,null,Timeout.Infinite,Timeout.Infinite);ScheduleLocked();}
    // Explicitly own and close all three redirected streams. Process.Dispose
    // alone does not close externally referenced readers/writers on Framework.
    stdout=child.StandardOutput;stderr=child.StandardError;outputThread=Read(stdout,output);errorThread=Read(stderr,error);
    bool quitSent=false;
    while(!child.HasExited){
     string line=null;
     lock(gate){
      if(abort)break;
      if(commands.Count>0){line=commands.First.Value.Line;commands.RemoveFirst();}
      else if(stopping&&!quitSent){line="{\"action\":\"quit\"}";quitSent=true;}
      if(line!=null){writeDeadline=clock.ElapsedMilliseconds+writeTimeoutMs;ScheduleLocked();}
     }
     if(line!=null){child.StandardInput.WriteLine(line);child.StandardInput.Flush();lock(gate){writeDeadline=0;ScheduleLocked();}}
     else wake.WaitOne();
    }
    if(!stopping)Report("额度读取进程已停止，30 秒内自动恢复。");
   }catch{Report("暂时无法连接额度读取进程，将自动重试。");}
   finally{
    alive=false;lock(gate){commands.Clear();process=null;writeDeadline=0;if(watchdog!=null){watchdog.Dispose();watchdog=null;}}
    if(child!=null){
     Kill(child);try{child.WaitForExit(500);}catch{}
     try{child.StandardInput.Dispose();}catch{}
     if(stdout!=null)try{stdout.Dispose();}catch{}if(stderr!=null)try{stderr.Dispose();}catch{}
     if(outputThread!=null)outputThread.Join(200);if(errorThread!=null)errorThread.Join(200);
     if(exited!=null)child.Exited-=exited;
     child.Dispose();
    }
    lock(gate)completed=true;wake.Dispose();
   }
  }
 }
}
