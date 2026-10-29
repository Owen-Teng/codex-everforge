using System;
using System.Diagnostics;
using System.IO;

namespace CodexQuota {
 internal static class DesktopDetector {
  internal static bool IsCodexDesktop(string path){
   if(string.IsNullOrEmpty(path))return false;
   try{
    string root=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles),"WindowsApps")+Path.DirectorySeparatorChar;
    string full=Path.GetFullPath(path);if(!full.StartsWith(root,StringComparison.OrdinalIgnoreCase))return false;
    string[] parts=full.Substring(root.Length).Split(Path.DirectorySeparatorChar);
    return parts.Length==3&&parts[0].StartsWith("OpenAI.Codex_",StringComparison.OrdinalIgnoreCase)&&parts[0].EndsWith("__2p2nqsd0c76g0",StringComparison.OrdinalIgnoreCase)&&parts[1].Equals("app",StringComparison.OrdinalIgnoreCase)&&(parts[2].Equals("ChatGPT.exe",StringComparison.OrdinalIgnoreCase)||parts[2].Equals("Codex.exe",StringComparison.OrdinalIgnoreCase));
   }catch{return false;}
  }
  static string Find(bool session){
   foreach(string name in new[]{"ChatGPT","Codex"}){
    var processes=Process.GetProcessesByName(name);
    try{foreach(var process in processes){try{if(process.MainWindowHandle!=IntPtr.Zero&&IsCodexDesktop(process.MainModule.FileName))return session?process.Id+":"+process.StartTime.ToUniversalTime().Ticks:process.MainModule.FileName;}catch{}}}
    finally{foreach(var process in processes)process.Dispose();}
   }
   return null;
  }
  internal static string Session(){return Find(true);}
  internal static string IconPath(){return Find(false);}
 }
}
