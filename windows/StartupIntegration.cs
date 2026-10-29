using System;
using System.Diagnostics;
using System.IO;
using System.Threading;
using Microsoft.Win32;

namespace CodexQuota {
 internal static class StartupIntegration {
  internal const string RunPath=@"Software\Microsoft\Windows\CurrentVersion\Run";
  internal const string ValueName="CodexQuotaMonitor.CodexStartup";
  internal const string StopEvent=@"Local\CodexQuotaMonitor.Launcher.Stop.v1";
  internal static string LauncherPath {get{return Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"CodexQuotaLauncher.exe");}}
  internal static bool Enabled {get{try{using(var key=Registry.CurrentUser.OpenSubKey(RunPath)){return string.Equals(Convert.ToString(key==null?null:key.GetValue(ValueName)),"\""+LauncherPath+"\"",StringComparison.OrdinalIgnoreCase);}}catch{return false;}}}
  internal static void SetEnabled(bool enabled){
   if(enabled&&!File.Exists(LauncherPath))throw new FileNotFoundException("缺少启动组件，请完整安装工具。",LauncherPath);
   using(var key=Registry.CurrentUser.CreateSubKey(RunPath)){
    if(enabled)key.SetValue(ValueName,"\""+LauncherPath+"\"",RegistryValueKind.String);
    else if(Enabled)key.DeleteValue(ValueName,false);
   }
   if(enabled){Process.Start(new ProcessStartInfo(LauncherPath){UseShellExecute=false,CreateNoWindow=true});}
   else {try{using(var stop=EventWaitHandle.OpenExisting(StopEvent)){stop.Set();}}catch(WaitHandleCannotBeOpenedException){}}
  }
 }
}
