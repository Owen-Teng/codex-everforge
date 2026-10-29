using System;
using System.Drawing;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;
using System.IO;
namespace MakerPopup {
 internal static class Native {
  [StructLayout(LayoutKind.Sequential)] internal struct Rect {internal int left,top,right,bottom;internal Rectangle Value{get{return Rectangle.FromLTRB(left,top,right,bottom);}}}
  [DllImport("user32.dll")] internal static extern bool SetProcessDpiAwarenessContext(IntPtr value);
  [DllImport("user32.dll")] internal static extern uint GetDpiForWindow(IntPtr window);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] internal static extern IntPtr FindWindow(string cls,string title);
  [DllImport("user32.dll")] internal static extern bool PostMessage(IntPtr window,uint message,IntPtr w,IntPtr l);
  [DllImport("user32.dll")] internal static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] internal static extern bool SetForegroundWindow(IntPtr window);
  [DllImport("user32.dll")] internal static extern bool AllowSetForegroundWindow(uint pid);
  [DllImport("user32.dll")] internal static extern uint GetWindowThreadProcessId(IntPtr window,out uint process);
  [DllImport("user32.dll")] internal static extern IntPtr WindowFromPoint(Point point);
  [DllImport("user32.dll")] internal static extern IntPtr GetAncestor(IntPtr window,uint flags);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] internal static extern int GetClassName(IntPtr window,StringBuilder text,int count);
  [DllImport("user32.dll")] internal static extern bool GetWindowRect(IntPtr window,out Rect rect);
  [DllImport("user32.dll")] internal static extern bool IsWindowVisible(IntPtr window);
  [DllImport("user32.dll")] internal static extern short GetAsyncKeyState(int key);
  [DllImport("kernel32.dll")] static extern IntPtr OpenProcess(uint access,bool inherit,uint process);
  [DllImport("kernel32.dll",CharSet=CharSet.Unicode)] static extern bool QueryFullProcessImageName(IntPtr process,uint flags,StringBuilder path,ref uint length);
  [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
  internal const uint Wake=0x8000+44;
  internal static bool WindowRunsExecutable(IntPtr window,string expected){uint pid;GetWindowThreadProcessId(window,out pid);IntPtr process=OpenProcess(0x1000,false,pid);if(process==IntPtr.Zero)return false;try{var path=new StringBuilder(1024);uint length=(uint)path.Capacity;return QueryFullProcessImageName(process,0,path,ref length)&&string.Equals(path.ToString(),expected,StringComparison.OrdinalIgnoreCase);}finally{CloseHandle(process);}}
  internal static string MainExecutable{get{return Path.GetFullPath(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,@"..\CodexQuota.exe"));}}
  internal static IntPtr FindOwnTray(){
   const string seed="96676735-c5d2-48f1-96b5-4553a6fb177f";string root=Path.GetDirectoryName(MainExecutable).TrimEnd(Path.DirectorySeparatorChar);string installed=Path.GetFullPath(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","CodexQuotaMonitor"));Guid id;
   if(string.Equals(root,installed,StringComparison.OrdinalIgnoreCase))id=new Guid(seed);else using(var hash=SHA256.Create()){byte[] digest=hash.ComputeHash(Encoding.UTF8.GetBytes(seed+"\n"+root.ToUpperInvariant()));var bytes=new byte[16];Array.Copy(digest,bytes,16);id=new Guid(bytes);}
   IntPtr window=FindWindow(null,"Codex Quota Status "+id.ToString("D"));return window!=IntPtr.Zero&&WindowRunsExecutable(window,MainExecutable)?window:IntPtr.Zero;
  }
 }
}
