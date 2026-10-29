using System;
using System.Diagnostics;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Threading;
using Microsoft.Win32;

namespace CodexQuota {
 internal sealed class SetupResult {
  internal bool Installed,Startup,Visible,QuotaReady;
  internal string Error="",VisibilityNote="",QuotaNote="";
 }
 internal static class SetupIntegration {
  internal static readonly Guid InstalledIcon=new Guid("96676735-c5d2-48f1-96b5-4553a6fb177f");
  internal static readonly string InstallDirectory=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","CodexQuotaMonitor");
  static readonly string[] Files={"CodexQuota.exe","quota-core.exe","CodexQuotaLauncher.exe"};
  internal static readonly string[] StaticFiles={"preview/community-mvp/index.html","preview/community-mvp/styles.css","preview/community-mvp/app.js","preview/community-mvp/translations.js","preview/community-mvp/translations-r2.js","preview/community-mvp/domain.js","preview/community-mvp/live.js","preview/community-mvp/assets/maker.png","preview/community-mvp/assets/favicon.svg"};
  // Included only when an x64 package supplies its popup marker; partial packages fail validation.
  internal static readonly string[] PopupFiles={"popup/MakerPopup.exe","popup/Microsoft.Web.WebView2.Core.dll","popup/Microsoft.Web.WebView2.WinForms.dll","popup/WebView2Loader.dll","popup/WebView2-LICENSE.txt","preview/desktop-popup/index.html","preview/desktop-popup/base.css","preview/desktop-popup/growth.css","preview/desktop-popup/popup.css","preview/desktop-popup/maker-home.css","preview/desktop-popup/translations-base.js","preview/desktop-popup/translations.js","preview/desktop-popup/popup-translations.js","preview/desktop-popup/rules.js","preview/desktop-popup/domain.js","preview/desktop-popup/growth.js","preview/desktop-popup/popup-battle.js","preview/desktop-popup/maker-home.js","preview/desktop-popup/popup-view.js","preview/desktop-popup/app.js","preview/desktop-popup/assets/maker.png","preview/desktop-popup/assets/favicon.svg","preview/desktop-popup/character-ui.js","preview/desktop-popup/inbox-ui.js","preview/desktop-popup/character-ui.css","preview/desktop-popup/upgrade-motion.js","preview/desktop-popup/assets/characters/male-a-seeker.png","preview/desktop-popup/assets/characters/male-a-torchbearer.png","preview/desktop-popup/assets/characters/male-a-shaper.png","preview/desktop-popup/assets/characters/male-a-maker.png","preview/desktop-popup/assets/characters/male-a-worldweaver.png","preview/desktop-popup/assets/characters/male-a-worldbuilder.png","preview/desktop-popup/assets/characters/male-b-seeker.png","preview/desktop-popup/assets/characters/male-b-torchbearer.png","preview/desktop-popup/assets/characters/male-b-shaper.png","preview/desktop-popup/assets/characters/male-b-maker.png","preview/desktop-popup/assets/characters/male-b-worldweaver.png","preview/desktop-popup/assets/characters/male-b-worldbuilder.png","preview/desktop-popup/assets/characters/female-a-seeker.png","preview/desktop-popup/assets/characters/female-a-torchbearer.png","preview/desktop-popup/assets/characters/female-a-shaper.png","preview/desktop-popup/assets/characters/female-a-maker.png","preview/desktop-popup/assets/characters/female-a-worldweaver.png","preview/desktop-popup/assets/characters/female-a-worldbuilder.png","preview/desktop-popup/assets/characters/female-b-seeker.png","preview/desktop-popup/assets/characters/female-b-torchbearer.png","preview/desktop-popup/assets/characters/female-b-shaper.png","preview/desktop-popup/assets/characters/female-b-maker.png","preview/desktop-popup/assets/characters/female-b-worldweaver.png","preview/desktop-popup/assets/characters/female-b-worldbuilder.png"};
  static string[] PackageFiles(string source){return Files.Concat(StaticFiles).Concat(File.Exists(OwnedPath(source,"popup/MakerPopup.exe"))?PopupFiles:new string[0]).ToArray();}
  const string Preferences=@"Software\CodexQuotaMonitor";
  internal static bool IsInstalled {get{return SamePath(AppDomain.CurrentDomain.BaseDirectory,InstallDirectory);}}
  internal static bool Configured {get{try{using(var key=Registry.CurrentUser.OpenSubKey(Preferences))return key!=null&&Convert.ToInt32(key.GetValue("SetupVersion",0))>=6;}catch{return false;}}}
  static bool SamePath(string a,string b){return string.Equals(Path.GetFullPath(a).TrimEnd('\\'),Path.GetFullPath(b).TrimEnd('\\'),StringComparison.OrdinalIgnoreCase);}
  internal static void Show(){Process.Start(new ProcessStartInfo(ApplicationPath(),"--setup"){UseShellExecute=false,CreateNoWindow=true});}
  static string ApplicationPath(){return Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"CodexQuota.exe");}
  static void Start(string path,string args){
   // Background helpers must not inherit a command-line caller's output pipe.
   using(var process=Process.Start(new ProcessStartInfo(path,args){UseShellExecute=false,CreateNoWindow=true,RedirectStandardOutput=true,RedirectStandardError=true})){
    process.StandardOutput.Close();process.StandardError.Close();
   }
  }
  static void CopyOwnedFile(string source,string target){
   if(File.Exists(target)){
    if(File.ReadAllBytes(source).SequenceEqual(File.ReadAllBytes(target)))return;
    File.SetAttributes(target,File.GetAttributes(target)&~FileAttributes.ReadOnly);
   }
   File.Copy(source,target,true);
  }
  static Process[] Owned(string name){return Process.GetProcessesByName(name).Where(p=>{try{if(SamePath(p.MainModule.FileName,Path.Combine(InstallDirectory,name+".exe")))return true;}catch{}p.Dispose();return false;}).ToArray();}
  internal static void RestartMonitor(string executable,bool demo,Guid? promote){
   IntPtr window=Native.FindWindow(null,"Codex Quota Monitor"+(demo?" · DEMO":""));uint pid;
   if(window!=IntPtr.Zero){
    Native.GetWindowThreadProcessId(window,out pid);
    using(var process=Process.GetProcessById((int)pid)){
     if(process.Id==Process.GetCurrentProcess().Id||!SamePath(process.MainModule.FileName,executable))throw new IOException("另一个位置的额度工具正在运行，请先退出它再配置。");
     Native.PostMessage(window,0x0010,IntPtr.Zero,IntPtr.Zero);
     if(!process.WaitForExit(6000))throw new IOException("额度工具还在关闭，请稍后重新检查。");
    }
   }
   if(promote.HasValue){Thread.Sleep(200);PromoteIcon(promote.Value,executable);}
   // Explorer caches promotion for a running process on tested Windows 11 builds.
   // Restart just our monitor, never Explorer or Codex, to pick up its own preference.
   Start(executable,demo?"--demo=75 --background":"--background");
  }
  static void StopInstalled(){
   // Only processes from our fixed installation directory can be stopped.
   foreach(var popup in Process.GetProcessesByName("MakerPopup"))using(popup){
    bool owned=false;try{owned=SamePath(popup.MainModule.FileName,Path.Combine(InstallDirectory,@"popup\MakerPopup.exe"));}catch{}
    if(owned&&!popup.HasExited){popup.CloseMainWindow();if(!popup.WaitForExit(6000))throw new IOException("牌桌窗口还在关闭，请先关闭后重试更新。");}
   }
   try{using(var stop=EventWaitHandle.OpenExisting(StartupIntegration.StopEvent))stop.Set();}catch(WaitHandleCannotBeOpenedException){}
   foreach(var p in Owned("CodexQuotaLauncher"))using(p){if(!p.WaitForExit(2500)){p.Kill();p.WaitForExit(2500);}}
   var running=Owned("CodexQuota");
   try{
    if(running.Length>0){using(var quit=Process.Start(new ProcessStartInfo(Path.Combine(InstallDirectory,"CodexQuota.exe"),"--quit"){UseShellExecute=false,CreateNoWindow=true}))quit.WaitForExit(2000);}
    foreach(var p in running){if(!p.WaitForExit(6000)){p.Kill();p.WaitForExit(2500);}}
   }finally{foreach(var p in running)p.Dispose();}
   foreach(var p in Owned("quota-core"))using(p){if(!p.HasExited){p.Kill();p.WaitForExit(2500);}}
  }
  static void CopyPackage(){
   if(IsInstalled){ValidatePackage(AppDomain.CurrentDomain.BaseDirectory);return;}
   InstallFiles(AppDomain.CurrentDomain.BaseDirectory,InstallDirectory,StopInstalled,delegate{
    Start(Path.Combine(InstallDirectory,"CodexQuotaLauncher.exe"),"");Start(Path.Combine(InstallDirectory,"CodexQuota.exe"),"--background");
   });
  }
  // The exact production file transaction can be tested in a temporary directory
  // without stopping the user's monitor or writing any startup/notification settings.
  static string OwnedPath(string root,string file){
   root=Path.GetFullPath(root).TrimEnd(Path.DirectorySeparatorChar,Path.AltDirectorySeparatorChar);string path=Path.GetFullPath(Path.Combine(root,file));
   if(!path.StartsWith(root+Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase))throw new IOException("安装文件路径无效。");
   string current=root;if(Directory.Exists(current)&&(File.GetAttributes(current)&FileAttributes.ReparsePoint)!=0)throw new IOException("安装资源路径不能是链接。");
   foreach(string part in file.Split(new[]{'/', '\\'},StringSplitOptions.RemoveEmptyEntries)){current=Path.Combine(current,part);if((File.Exists(current)||Directory.Exists(current))&&(File.GetAttributes(current)&FileAttributes.ReparsePoint)!=0)throw new IOException("安装资源路径不能是链接。");}
   return path;
  }
  static void ValidatePackage(string source){foreach(string file in PackageFiles(source)){string path=OwnedPath(source,file);if(!File.Exists(path)||new FileInfo(path).Length==0)throw new IOException("请先完整解压下载包（包括实时页面和牌桌资源），再打开 CodexQuota.exe。");}}
  static void InstallFiles(string source,string destination,Action stop,Action restore){
   ValidatePackage(source);var critical=PackageFiles(source);var attempted=new HashSet<string>();
   if(Directory.Exists(destination)&&(File.GetAttributes(destination)&FileAttributes.ReparsePoint)!=0)throw new IOException("安装目录是链接，请选择普通的本地安装目录。");
   foreach(string file in critical)OwnedPath(destination,file);
   Directory.CreateDirectory(destination);
   string stage=Path.Combine(destination,"update-"+Guid.NewGuid().ToString("N"));Directory.CreateDirectory(stage);
   bool stopped=false,success=false;
   try{
    foreach(string file in critical){string staged=OwnedPath(stage,file);Directory.CreateDirectory(Path.GetDirectoryName(staged));File.Copy(OwnedPath(source,file),staged);string old=OwnedPath(destination,file);if(File.Exists(old))File.Copy(old,staged+".previous");}
    stop();stopped=true;
    foreach(string file in critical){string target=OwnedPath(destination,file);Directory.CreateDirectory(Path.GetDirectoryName(target));attempted.Add(file);CopyOwnedFile(OwnedPath(stage,file),target);if(!File.ReadAllBytes(OwnedPath(stage,file)).SequenceEqual(File.ReadAllBytes(target)))throw new IOException("安装文件校验失败。");}
    foreach(string file in new[]{"README.md","THIRD_PARTY_NOTICES.md"})if(File.Exists(Path.Combine(source,file)))CopyOwnedFile(Path.Combine(source,file),Path.Combine(destination,file));
    string licenses=Path.Combine(source,"licenses");if(Directory.Exists(licenses)){string target=Path.Combine(destination,"licenses");Directory.CreateDirectory(target);foreach(string file in Directory.GetFiles(licenses))CopyOwnedFile(file,Path.Combine(target,Path.GetFileName(file)));}
    foreach(string file in new[]{"docs/VALIDATION.md","docs/WINDOWS_V07.md","docs/WINDOWS_INSTALL_TEST.md","docs/images/windows-v07-native-sizes.png","docs/images/windows-v06-oneclick.jpg"}){
     string origin=Path.Combine(source,file),target=Path.Combine(destination,file);
     if(File.Exists(origin)){Directory.CreateDirectory(Path.GetDirectoryName(target));CopyOwnedFile(origin,target);}
    }
    success=true;
   }catch{
    if(stopped){foreach(string file in critical){string previous=Path.Combine(stage,file+".previous");try{string target=OwnedPath(destination,file);if(File.Exists(previous))CopyOwnedFile(previous,target);else if(attempted.Contains(file)&&File.Exists(target))File.Delete(target);}catch{}}
     try{restore();}catch{}}
    throw;
   }finally{
    // Never recursively delete a computed directory; preserve rollback files after a failure.
    if(success){foreach(string file in critical){string owned=OwnedPath(stage,file);foreach(string path in new[]{owned,owned+".previous"})if(File.Exists(path)){File.SetAttributes(path,FileAttributes.Normal);File.Delete(path);}}
     foreach(string directory in critical.Select(x=>Path.GetDirectoryName(x)).Where(x=>!string.IsNullOrEmpty(x)).SelectMany(x=>{var paths=new List<string>();while(!string.IsNullOrEmpty(x)){paths.Add(x);x=Path.GetDirectoryName(x);}return paths;}).Distinct().OrderByDescending(x=>x.Length))Directory.Delete(OwnedPath(stage,directory),false);
     Directory.Delete(stage,false);}
   }
  }
  sealed class Availability {public bool available {get;set;}}
  static string CheckQuota(string executable){
   if(!File.Exists(executable))return "缺少额度读取组件，请完整解压后重新安装。";
   try{
    using(var process=Process.Start(new ProcessStartInfo(executable,"--check"){UseShellExecute=false,CreateNoWindow=true,RedirectStandardInput=true,RedirectStandardOutput=true,RedirectStandardError=true})){
     process.StandardInput.Close();var output=process.StandardOutput.ReadToEndAsync();var errors=process.StandardError.ReadToEndAsync();
     if(!process.WaitForExit(35000)){try{process.Kill();process.WaitForExit(3000);}catch{}return "已安装，额度检查超时；联网后点击重新检查。";}
     var result=new System.Web.Script.Serialization.JavaScriptSerializer().Deserialize<Availability>(output.Result);
     if(process.ExitCode==0&&result!=null&&result.available)return "";
     string reason=errors.Result;
     if(reason.Contains("未找到 Codex CLI"))return "已安装，但未找到 Codex CLI；请先安装官方 CLI 并登录，再重新检查。";
     if(reason.Contains("登录")||reason.Contains("login"))return "已安装，请先在 Codex CLI 中登录 ChatGPT 账号，再重新检查。";
     return "已安装，但尚未读到额度；请检查网络及 Codex 登录状态，再重新检查。";
    }
   }catch{return "已安装，但额度检查未完成；请重新检查。";}
  }
  static void CreateSettingsShortcut(){
   // A visible Start-menu entry lets a new user reopen settings even if Windows hides the icon.
   object shell=null,shortcut=null;
   try{
    Type type=Type.GetTypeFromProgID("WScript.Shell");shell=Activator.CreateInstance(type);
    string link=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Programs),"Codex 额度设置.lnk");
    shortcut=type.InvokeMember("CreateShortcut",BindingFlags.InvokeMethod,null,shell,new object[]{link});Type st=shortcut.GetType();
    st.InvokeMember("TargetPath",BindingFlags.SetProperty,null,shortcut,new object[]{Path.Combine(InstallDirectory,"CodexQuota.exe")});
    st.InvokeMember("Arguments",BindingFlags.SetProperty,null,shortcut,new object[]{"--setup"});
    st.InvokeMember("Description",BindingFlags.SetProperty,null,shortcut,new object[]{"配置 Codex 额度显示与自动启动"});
    st.InvokeMember("Save",BindingFlags.InvokeMethod,null,shortcut,new object[0]);
   }finally{if(shortcut!=null)System.Runtime.InteropServices.Marshal.FinalReleaseComObject(shortcut);if(shell!=null)System.Runtime.InteropServices.Marshal.FinalReleaseComObject(shell);}
  }
  internal static bool PromoteIcon(Guid guid,string executable){
   // Windows 11 implementation detail, not a supported Shell API. Never change
   // global overflow policies or another app's settings. Verify actual visibility separately.
   using(var root=Registry.CurrentUser.OpenSubKey(@"Control Panel\NotifyIconSettings",true)){
    if(root==null)return false;
    foreach(string name in root.GetSubKeyNames())using(var key=root.OpenSubKey(name,true)){
     Guid found;string path=Convert.ToString(key.GetValue("ExecutablePath",""));
     if(!Guid.TryParse(Convert.ToString(key.GetValue("IconGuid","")),out found)||found!=guid||string.IsNullOrEmpty(path))continue;
     if(!SamePath(Environment.ExpandEnvironmentVariables(path),executable))continue;
     key.SetValue("IsPromoted",1,RegistryValueKind.DWord);return true;
    }
   }
   return false;
  }
  internal static SetupResult Apply(Action<string> progress){
   var result=new SetupResult();bool first;
   using(var mutex=new Mutex(true,@"Local\CodexQuotaMonitor.Setup.Apply.v1",out first)){
    if(!first){result.Error="另一个配置窗口正在处理，请稍后重新检查。";return result;}
    try{
     progress("正在安装到当前用户目录…");CopyPackage();result.Installed=true;
     progress("正在启用随 Codex 启动…");
     using(var launcher=Process.Start(new ProcessStartInfo(Path.Combine(InstallDirectory,"CodexQuotaLauncher.exe"),"--enable"){UseShellExecute=false,CreateNoWindow=true,RedirectStandardOutput=true,RedirectStandardError=true})){
      if(!launcher.WaitForExit(10000)||launcher.ExitCode!=0)throw new IOException("随 Codex 启动未能开启，请关闭其他配置窗口后重试。");
     }
     result.Startup=true;
     Start(Path.Combine(InstallDirectory,"CodexQuota.exe"),"--background");
     progress("正在设置右侧常显，并检查实际位置…");
     bool wrote=false,restarted=false;
     for(int i=0;i<20;i++){
      if(!wrote)try{wrote=PromoteIcon(InstalledIcon,Path.Combine(InstallDirectory,"CodexQuota.exe"));}catch(UnauthorizedAccessException){}catch(System.Security.SecurityException){}
      if(wrote&&StatusIcon.InTaskbar(InstalledIcon)){result.Visible=true;break;}
      if(wrote&&!restarted){RestartMonitor(Path.Combine(InstallDirectory,"CodexQuota.exe"),false,InstalledIcon);restarted=true;}
      Thread.Sleep(400);
     }
     result.VisibilityNote=result.Visible?"已在任务栏右侧显示":"横排显示尚未就绪。请先检查本工具已开启右侧显示，再点击重新检查。";
     progress("正在确认额度可以读取…");result.QuotaNote=CheckQuota(Path.Combine(InstallDirectory,"quota-core.exe"));result.QuotaReady=result.QuotaNote=="";
     try{CreateSettingsShortcut();}catch{result.Error="开始菜单入口未能创建，仍可从图标右键打开设置。";}
     using(var key=Registry.CurrentUser.CreateSubKey(Preferences))key.SetValue("SetupVersion",7,RegistryValueKind.DWord);
    }catch(Exception ex){result.Error=ex is UnauthorizedAccessException?"无法更新安装文件，请检查安装目录是否可写，再重试。":ex is IOException&&ex.Message.Length<90?ex.Message:"配置未完成。请确认程序包已完整解压，再重新尝试。";}
    finally{mutex.ReleaseMutex();}
   }
   return result;
  }
 }
}
