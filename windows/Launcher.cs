using System;
using System.Diagnostics;
using System.IO;
using System.Threading;

namespace CodexQuota {
 internal sealed class LaunchGate {
  string observed; int absent;
  internal bool ShouldLaunch(string desktopSession){
   if(desktopSession==null){if(++absent>=2)observed=null;return false;}
   absent=0;if(observed==desktopSession)return false;observed=desktopSession;return true;
  }
  internal void Retry(){observed=null;}
 }
 internal static class Launcher {
  static bool MonitorRunning(string target){var all=Process.GetProcessesByName("CodexQuota");try{foreach(var p in all){try{if(string.Equals(p.MainModule.FileName,target,StringComparison.OrdinalIgnoreCase))return true;}catch{}}return false;}finally{foreach(var p in all)p.Dispose();}}
  static void Assert(bool value){if(!value)throw new Exception("Launcher self-test failed");}
  static int SelfTest(){
   string root=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles),"WindowsApps");
   Assert(DesktopDetector.IsCodexDesktop(Path.Combine(root,@"OpenAI.Codex_26.1013.4894.0_x64__2p2nqsd0c76g0\app\ChatGPT.exe")));
   Assert(DesktopDetector.IsCodexDesktop(Path.Combine(root,@"OpenAI.Codex_99.1.0.0_arm64__2p2nqsd0c76g0\app\Codex.exe")));
   Assert(!DesktopDetector.IsCodexDesktop(Path.Combine(root,@"OpenAI.ChatGPT_1_x64__2p2nqsd0c76g0\app\ChatGPT.exe")));
   Assert(!DesktopDetector.IsCodexDesktop(@"C:\npm\node_modules\@openai\codex\codex.exe"));
   Assert(!DesktopDetector.IsCodexDesktop(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),@"OpenAI\Codex\bin\version\codex.exe")));
   var gate=new LaunchGate();Assert(!gate.ShouldLaunch(null));Assert(gate.ShouldLaunch("1"));Assert(!gate.ShouldLaunch("1"));
   Assert(!gate.ShouldLaunch(null));Assert(!gate.ShouldLaunch("1"));Assert(gate.ShouldLaunch("2"));
   Assert(!gate.ShouldLaunch(null));Assert(!gate.ShouldLaunch(null));Assert(gate.ShouldLaunch("2"));
   gate.Retry();Assert(gate.ShouldLaunch("2"));return 0;
  }
  [STAThread] static int Main(string[] args){
   try{
    if(args.Length==1&&args[0]=="--self-test")return SelfTest();
    if(args.Length==1&&args[0]=="--probe")return DesktopDetector.Session()!=null?0:2;
    if(args.Length==1&&args[0]=="--enable"){StartupIntegration.SetEnabled(true);return 0;}
    if(args.Length==1&&args[0]=="--disable"){StartupIntegration.SetEnabled(false);return 0;}
    if(args.Length!=0)return 2;
    bool first;using(var mutex=new Mutex(true,@"Local\CodexQuotaMonitor.Launcher.v1",out first)){
     if(!first)return 0;
     using(var stop=new EventWaitHandle(false,EventResetMode.ManualReset,StartupIntegration.StopEvent)){
      stop.Reset();var gate=new LaunchGate();string target=Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"CodexQuota.exe");
      while(StartupIntegration.Enabled){
       if(gate.ShouldLaunch(DesktopDetector.Session())&&!MonitorRunning(target)){
        try{Process.Start(new ProcessStartInfo(target,"--background"){UseShellExecute=false,CreateNoWindow=true});}catch{gate.Retry();}
       }
       if(stop.WaitOne(5000))break;
      }
     }
     mutex.ReleaseMutex();
    }
    return 0;
   }catch{return 1;}
  }
 }
}
