using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.IO;
using System.Linq;
using System.Net.NetworkInformation;
using System.Runtime.InteropServices;
using System.Threading;
using System.Web.Script.Serialization;
using System.Windows.Forms;
using Microsoft.Win32;

namespace CodexQuota {
 public sealed class QuotaWindow { public string name; public int? remaining; public long? durationMins; public long? resetsAt; }
 public sealed class Choice { public string id; public string name; }
 public sealed class ResetEvent { public string id,kind,sourceUrl,summary,verification; public long? publishedAt; }
 public sealed class ResetInfo { public string state,sourceKind,sourceLabel,reason; public long? lastCheckedAt,lastSuccessAt; public bool sourceFresh; public ResetEvent @event; public Dictionary<string,object> localRecovery; }
 public sealed class UsageInfo { public string state,reason; public bool rankingEligible; }
 public sealed class AlertPreferences { public bool resetAlertsEnabled; }
 public sealed class NotificationMessage { public string id,kind,title,body; public long? createdAt; }
 // The core owns event decisions. This additional consumer guard prevents
 // replay when a JSON snapshot is re-emitted, the app restarts or alerts reopen.
 internal sealed class NotificationGate {
  readonly HashSet<string> seen=new HashSet<string>(),tests=new HashSet<string>();
  bool initialized,wasEnabled,sourceWasFresh;
  internal bool Accept(NotificationMessage notice,bool enabled,bool explicitTest,long now,bool sourceFresh,string sourceKind=""){
   bool initial=!initialized,reopening=enabled&&!wasEnabled,sourceRecovered=sourceFresh&&!sourceWasFresh;
   initialized=true;wasEnabled=enabled;sourceWasFresh=sourceFresh;
   if(notice==null||string.IsNullOrEmpty(notice.id))return false;
   bool test=notice.kind=="test";var ids=test?tests:seen;
   if(!ids.Add(notice.id))return false;
   // Core persists authoritative IDs. Bound the extra in-memory UI guard.
   if(ids.Count>256){ids.Clear();ids.Add(notice.id);}
   if(!notice.createdAt.HasValue||notice.createdAt.Value<=0||notice.createdAt.Value>now+300||now-notice.createdAt.Value>600)return false;
   // A new test emitted by the core after baseline comes only from an explicit
   // local action (web or native). Do not replay a test present at startup.
   if(test)return enabled&&(explicitTest||!initial);
   bool local=notice.kind=="local_recovery"||notice.kind=="recovered";
   bool source=notice.kind=="notice"||notice.kind=="announced"||notice.kind=="announcement"||notice.kind=="reset";
   bool cloud=sourceKind=="cloud_feed"||sourceKind=="synthetic_cloud"||sourceKind=="public_aggregator";
   // correction has no machine-verifiable relationship in v0.1: details only.
   // The cloud core owns persistent baseline/seen/recovery eligibility. Do not
   // swallow its first valid catch-up notice after an ordinary network outage.
   return enabled&&!initial&&!reopening&&(local||(source&&sourceFresh&&(!sourceRecovered||cloud)));
  }
 }
 public sealed class Snapshot {
  public string state="loading", color="gray", bucket="", bucketName="", scope="", message="正在读取额度…", configWarning="";
  public int? remaining; public QuotaWindow[] windows; public Choice[] choices;
  public long updatedAt,lastSuccess,nextRefresh; public int intervalSeconds=120; public bool stale,partial,demo;
  public ResetInfo reset; public UsageInfo usage; public AlertPreferences preferences; public NotificationMessage notification; public object inbox;
 }
 // Internal R11 loopback integration only. Normal clients use the endpoint
 // compiled into quota-core and have no address/settings override UI.
 internal sealed class WorkerLaunchOptions {
  internal readonly string TestURL,SettingsRoot;
  internal WorkerLaunchOptions(){TestURL="";SettingsRoot="";}
  WorkerLaunchOptions(string url,string root){TestURL=url;SettingsRoot=root;}
  static bool Overlaps(string first,string second){first=first.TrimEnd('\\','/');second=second.TrimEnd('\\','/');return first.Equals(second,StringComparison.OrdinalIgnoreCase)||first.StartsWith(second+"\\",StringComparison.OrdinalIgnoreCase)||second.StartsWith(first+"\\",StringComparison.OrdinalIgnoreCase);}
  static string IsolatedRoot(string value){
   if(string.IsNullOrWhiteSpace(value)||value.Any(char.IsControl)||value.IndexOf('"')>=0||!System.Text.RegularExpressions.Regex.IsMatch(value,@"\A[A-Za-z]:[\\/].+\z"))return null;
   string root=Path.GetFullPath(value).TrimEnd('\\','/');if(root.Length<=3||File.Exists(root))return null;
   foreach(string appData in new[]{Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),Environment.GetEnvironmentVariable("APPDATA"),Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData)}){
    if(!string.IsNullOrEmpty(appData)&&Overlaps(root,Path.Combine(Path.GetFullPath(appData),"CodexQuotaMonitor")))return null;
   }
   string installed=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","CodexQuotaMonitor");if(Overlaps(root,installed))return null;
   // Avoid a lexical test path being redirected into real state by a junction.
   for(var folder=new DirectoryInfo(root);folder!=null;folder=folder.Parent)if(folder.Exists&&(folder.Attributes&FileAttributes.ReparsePoint)!=0)return null;
   return root;
  }
  internal static bool TryParse(string[] args,out WorkerLaunchOptions options){
   options=new WorkerLaunchOptions();string url=null,root=null,demo=null;int demos=0;
   foreach(string arg in args){
    if(arg.StartsWith("--demo=",StringComparison.Ordinal)){demo=arg.Substring(7);demos++;}
    if(arg.StartsWith("--reset-feed-test-url",StringComparison.Ordinal)){if(!arg.StartsWith("--reset-feed-test-url=",StringComparison.Ordinal)||url!=null)return false;url=arg.Substring(22);}
    if(arg.StartsWith("--settings-root",StringComparison.Ordinal)){if(!arg.StartsWith("--settings-root=",StringComparison.Ordinal)||root!=null)return false;root=arg.Substring(16);}
   }
   if(url==null&&root==null)return true;
   if(args.Contains("--setup")||args.Contains("--configure"))return false;
   if(url==null||root==null||demos!=1||!new[]{"75","100","60","59","20","19","0","multi","error","login"}.Contains(demo))return false;
   try{
    Uri uri;if(!Uri.TryCreate(url,UriKind.Absolute,out uri)||uri.Scheme!=Uri.UriSchemeHttp||!string.IsNullOrEmpty(uri.UserInfo)||!string.IsNullOrEmpty(uri.Query)||!string.IsNullOrEmpty(uri.Fragment)||uri.AbsolutePath!="/v1/feed"||uri.Port<1||uri.Port>65535)return false;
    if(!System.Text.RegularExpressions.Regex.IsMatch(url,@"\Ahttp://(?:127\.0\.0\.1|\[::1\])(?::[0-9]{1,5})?/v1/feed\z",System.Text.RegularExpressions.RegexOptions.IgnoreCase|System.Text.RegularExpressions.RegexOptions.CultureInvariant))return false;
    string isolated=IsolatedRoot(root);if(isolated==null)return false;
    // Framework Uri expands ::1 in AbsoluteUri; preserve the explicit accepted
    // literal form rather than changing it before core validates it again.
    string canonical="http://"+(uri.HostNameType==UriHostNameType.IPv6?"[::1]":"127.0.0.1")+(uri.IsDefaultPort?"":":"+uri.Port)+"/v1/feed";
    options=new WorkerLaunchOptions(canonical,isolated);return true;
   }catch(ArgumentException){return false;}catch(NotSupportedException){return false;}catch(IOException){return false;}catch(UnauthorizedAccessException){return false;}
  }
  internal string Arguments(string demo){
   if(TestURL=="")return demo!=""?"--demo="+demo:"--live";
   // Validated URL contains no whitespace/quotes. Normalized root contains no
   // quote/control and no trailing slash, so this is one Windows argv token.
   return "--demo="+demo+" --live --reset-feed-test-url="+TestURL+" \"--settings-root="+SettingsRoot+"\"";
  }
 }
 static class Native {
  [DllImport("user32.dll")] internal static extern bool SetProcessDpiAwarenessContext(IntPtr value);
  [DllImport("user32.dll")] internal static extern uint GetDpiForWindow(IntPtr hwnd);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] internal static extern IntPtr FindWindow(string cls,string title);
  [DllImport("user32.dll")] internal static extern bool PostMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] internal static extern uint RegisterWindowMessage(string name);
  [DllImport("user32.dll")] internal static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] internal static extern bool GetWindowRect(IntPtr h,out Rect r);
  [DllImport("user32.dll")] internal static extern int GetWindowLong(IntPtr h,int index);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] internal static extern int GetClassName(IntPtr h,System.Text.StringBuilder b,int max);
  [DllImport("user32.dll")] internal static extern bool SetForegroundWindow(IntPtr window);
  [DllImport("user32.dll")] internal static extern uint GetWindowThreadProcessId(IntPtr window,out uint process);
  [DllImport("user32.dll")] internal static extern bool DestroyIcon(IntPtr h);
  [DllImport("user32.dll")] static extern bool SystemParametersInfo(uint action,uint parameter,out bool value,uint flags);
  internal static bool MotionEnabled {get{bool enabled;return SystemParametersInfo(0x1042,0,out enabled,0)&&enabled;}}
  [StructLayout(LayoutKind.Sequential)] internal struct Rect {public int left,top,right,bottom;}
  internal static int Dpi(IntPtr h){try{return (int)GetDpiForWindow(h);}catch{return 96;}}
 }
 static class Program {
  internal static string Demo="";
  internal static bool Inspect=false,Background=false;
  internal static WorkerLaunchOptions WorkerOptions=new WorkerLaunchOptions();
  internal static uint Wake=Native.RegisterWindowMessage("CodexQuotaMonitor.Show.v1");
  internal static string Title {get{return "Codex Quota Monitor"+(Demo!=""?" · DEMO":"");}}
  internal static void Trace(string text){if(Inspect)try{File.AppendAllText(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"ui-inspect.log"),DateTime.Now.ToString("HH:mm:ss.fff")+" "+text+Environment.NewLine);}catch{}}
  [STAThread] static void Main(string[] args){
   try{Native.SetProcessDpiAwarenessContext(new IntPtr(-4));}catch{}
   foreach(string a in args){if(a.StartsWith("--demo=")) Demo=a.Substring(7);if(a=="--inspect")Inspect=true;if(a=="--background")Background=true;}
   if(!WorkerLaunchOptions.TryParse(args,out WorkerOptions)){Console.Error.WriteLine("invalid_loopback_test_options");Environment.ExitCode=2;return;}
   if(Demo!=""&&!new[]{"75","100","60","59","20","19","0","multi","error","login"}.Contains(Demo))Demo="75";
   if(args.Contains("--quit")){Native.PostMessage(Native.FindWindow(null,Title),0x0010,IntPtr.Zero,IntPtr.Zero);return;}
   if(args.Contains("--icon-status")){Console.WriteLine(new JavaScriptSerializer().Serialize(StatusIcon.Diagnostic(Demo!="")));return;}
   if(args.Contains("--setup-status")){Console.WriteLine(new JavaScriptSerializer().Serialize(new{visible=StatusIcon.InTaskbar(SetupIntegration.InstalledIcon)}));return;}
   if(args.Contains("--configure")){var result=SetupIntegration.Apply(delegate(string message){});Console.WriteLine(new JavaScriptSerializer().Serialize(new{installed=result.Installed,startup=result.Startup,visible=result.Visible,quotaReady=result.QuotaReady,quotaNote=result.QuotaNote,error=result.Error,note=result.VisibilityNote}));Environment.ExitCode=result.Installed&&result.Startup&&result.Visible&&result.QuotaReady&&result.Error==""?0:1;return;}
   try{Native.SetProcessDpiAwarenessContext(new IntPtr(-4));}catch{}
   Application.EnableVisualStyles();Application.SetCompatibleTextRenderingDefault(false);
   if(args.Contains("--setup")||(Demo==""&&!Background&&(!SetupIntegration.IsInstalled||!SetupIntegration.Configured))){Application.Run(new SetupForm(Demo!=""));return;}
   bool first; using(var mutex=new Mutex(true,"Local\\CodexQuotaMonitor.v1"+(Demo!=""?".demo":""),out first)) {
    if(!first){if(!Background)Native.PostMessage(Native.FindWindow(null,Title),Wake,IntPtr.Zero,IntPtr.Zero);return;}
    Application.Run(new QuotaBar());mutex.ReleaseMutex();
   }
  }
 }
 public sealed class QuotaBar:Form {
  readonly JavaScriptSerializer json=new JavaScriptSerializer();
  WorkerTransport worker;bool closing,dark;int dpi=96;
  readonly System.Windows.Forms.Timer pulse=new System.Windows.Forms.Timer();
  DateTime retryWorkerAfter=DateTime.MinValue,lastSnapshot=DateTime.UtcNow;
  Snapshot snapshot=new Snapshot();DetailPopup detail;StatusIcon statusIcon;
  readonly NotificationGate notificationGate=new NotificationGate();
  DateTime testRequestedUntil=DateTime.MinValue;
  bool? pendingAlerts;
  bool? lastPreferenceRequested;bool preferenceSaveFailed;
  internal string NotificationSubmission="尚未提交通知；展示状态未知";
  Color Surface {get{return dark?Color.FromArgb(36,55,51):Color.FromArgb(249,250,247);}}
  Color Border {get{return dark?Color.FromArgb(74,93,84):Color.FromArgb(217,224,216);}}
  internal Color Ink {get{return dark?Color.FromArgb(237,244,239):Color.FromArgb(39,53,47);}}
  internal Color Sub {get{return dark?Color.FromArgb(178,192,184):Color.FromArgb(113,128,118);}}
  internal Color Tone(string tone){switch(tone){case "green":return dark?Color.FromArgb(124,221,166):Color.FromArgb(24,115,76);case "yellow":return dark?Color.FromArgb(244,202,101):Color.FromArgb(151,101,0);case "red":return dark?Color.FromArgb(255,150,144):Color.FromArgb(192,57,50);default:return Sub;}}
  protected override bool ShowWithoutActivation {get{return true;}}
  public QuotaBar(){
   Text=Program.Title;ShowInTaskbar=false;FormBorderStyle=FormBorderStyle.None;StartPosition=FormStartPosition.Manual;Location=new Point(-32000,-32000);Size=new Size(1,1);Opacity=0;
   pulse.Interval=1000;pulse.Tick+=Pulse;
   SystemEvents.PowerModeChanged+=PowerChanged;SystemEvents.DisplaySettingsChanged+=DisplaysChanged;SystemEvents.UserPreferenceChanged+=ThemeChanged;NetworkChange.NetworkAvailabilityChanged+=NetworkChanged;
  }
  protected override void OnShown(EventArgs e){base.OnShown(e);Hide();ReadTheme();statusIcon=new StatusIcon(Handle,Program.Demo!="");UpdateStatusIcon();BuildMenu();StartWorker();pulse.Start();}
  void UI(Action action){if(closing||IsDisposed||!IsHandleCreated)return;try{BeginInvoke((Action)delegate{if(!closing&&!IsDisposed)action();});}catch(InvalidOperationException){}}
  void WorkerUI(WorkerTransport source,Action action){UI(delegate{if(worker==source&&source.Alive)action();});}
  void ReadTheme(){try{using(var key=Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize")){dark=key!=null&&Convert.ToInt32(key.GetValue("SystemUsesLightTheme",1))==0;}}catch{dark=false;}}
  void UpdateStatusIcon(){if(statusIcon!=null)statusIcon.Update(snapshot.state=="ready"&&!snapshot.stale?snapshot.remaining:null,Tone(snapshot.color),Program.Demo!="",dark);}
  void StartWorker(){
   if(closing)return;retryWorkerAfter=DateTime.UtcNow.AddSeconds(30);
   if(worker!=null){if(worker.Alive)return;worker.Stop();worker=null;}
   try{
    string exe=Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"quota-core.exe");
    if(!File.Exists(exe)){WorkerFailed("缺少 quota-core.exe，请完整解压程序包。");return;}
    var start=new ProcessStartInfo(exe,Program.WorkerOptions.Arguments(Program.Demo));start.UseShellExecute=false;start.CreateNoWindow=true;start.RedirectStandardInput=true;start.RedirectStandardOutput=true;start.RedirectStandardError=true;
    worker=new WorkerTransport(start,
     delegate(WorkerTransport source,string line){try{var data=new JavaScriptSerializer().Deserialize<Snapshot>(line);if(data!=null)WorkerUI(source,delegate{UpdateSnapshot(data);});}catch{WorkerUI(source,delegate{WorkerFailed("额度数据格式无法读取，请重新启动工具。");});}},
     delegate(WorkerTransport source,string line){if(line=="live_preferences_not_saved")WorkerUI(source,delegate{preferenceSaveFailed=true;if(lastPreferenceRequested==true)pendingAlerts=null;NotificationSubmission=lastPreferenceRequested==false?"本次已关，重启后设置可能恢复":"开启未保存；当前仍为关闭";if(detail!=null&&!detail.IsDisposed)detail.UpdateValue(snapshot);});},
     delegate(WorkerTransport source,string message){UI(delegate{if(worker==source){if(lastPreferenceRequested.HasValue){pendingAlerts=lastPreferenceRequested;preferenceSaveFailed=true;NotificationSubmission=lastPreferenceRequested==false?"本次已关；恢复连接后重试保存":"开启尚未确认；恢复连接后重试";}WorkerFailed(message);}});});
    // Replay the current local intent before normal commands on a replacement.
    if(lastPreferenceRequested.HasValue){pendingAlerts=lastPreferenceRequested;preferenceSaveFailed=false;worker.TrySend(json.Serialize(new{action="reset_alerts",enabled=lastPreferenceRequested.Value}),true);}
    worker.Start();
   }catch{if(worker!=null){worker.Stop();worker=null;}WorkerFailed("暂时无法启动读取程序，将自动重试；请确认程序包完整。");}
  }
  internal void Send(object command){
   if(closing)return;
   try{
    if(worker==null||!worker.Alive)StartWorker();
    if(worker==null||!worker.Alive)return;
    string line=json.Serialize(command);var fields=json.Deserialize<Dictionary<string,object>>(line);object action;
    bool preference=fields.TryGetValue("action",out action)&&Convert.ToString(action)=="reset_alerts";
    if(!worker.TrySend(line,preference)){
     if(preference){preferenceSaveFailed=true;NotificationSubmission=lastPreferenceRequested==false?"本次已关；设置未保存，请重试":"开启未提交，请重试";}
     else NotificationSubmission="额度命令未提交，请稍后重试。";
     UI(delegate{if(detail!=null&&!detail.IsDisposed)detail.UpdateValue(snapshot);});
    }
   }catch{UI(delegate{WorkerFailed("暂时无法连接读取进程，将自动重试。");});}
  }
  internal void RefreshQuota(){Send(new{action="refresh"});}
  internal bool AlertsEnabled(Snapshot data){return lastPreferenceRequested==false||pendingAlerts==false?false:data.preferences!=null&&data.preferences.resetAlertsEnabled;}
  internal void SetResetAlerts(bool enabled){pendingAlerts=enabled;lastPreferenceRequested=enabled;preferenceSaveFailed=false;NotificationSubmission=enabled?"正在请求开启；等待本机确认":"本次关闭已生效；等待保存确认";Send(new{action="reset_alerts",enabled=enabled});if(detail!=null&&!detail.IsDisposed)detail.UpdateValue(snapshot);}
  internal void RequestTestNotification(){
   if(snapshot.preferences==null||!AlertsEnabled(snapshot)){NotificationSubmission="请先开启本机提醒，再发送测试";return;}
   testRequestedUntil=DateTime.UtcNow.AddSeconds(90);NotificationSubmission="已请求测试；操作系统提交状态未知";Send(new{action="test_notification"});
   if(detail!=null&&!detail.IsDisposed)detail.UpdateValue(snapshot);
  }
  void ConsumeNotification(Snapshot data){
   if(pendingAlerts.HasValue&&data.preferences!=null&&data.preferences.resetAlertsEnabled==pendingAlerts.Value){pendingAlerts=null;if(!preferenceSaveFailed)NotificationSubmission="提醒设置已由本机确认；通知展示状态未知";}
   bool enabled=AlertsEnabled(data);
   var n=data.notification;
   if(!notificationGate.Accept(n,enabled,DateTime.UtcNow<=testRequestedUntil,DateTimeOffset.UtcNow.ToUnixTimeSeconds(),data.reset!=null&&data.reset.sourceFresh,data.reset==null?"":data.reset.sourceKind))return;
   bool syntheticSource=data.reset!=null&&data.reset.sourceKind=="synthetic_cloud";
   bool syntheticCloud=syntheticSource&&data.demo&&Program.Demo!="";
   // R11's loopback integration requires explicit --demo and marks the source.
   // Never let synthetic cloud input notify an ordinary production instance.
   if(n.kind!="test"&&((Program.Demo!=""&&!syntheticCloud)||(syntheticSource&&!syntheticCloud)))return;
   bool test=n.kind=="test";if(test)testRequestedUntil=DateTime.MinValue;
   string title=test?"测试提醒":n.kind=="local_recovery"||n.kind=="recovered"?"本机额度已回升":"重置来源公告 · 未独立核实";
   if(!test&&n.kind!="local_recovery"&&n.kind!="recovered"&&data.reset!=null&&data.reset.sourceKind=="public_aggregator")title="Tibo 公开消息 · 聚合来源";
   if(syntheticCloud&&!test)title="联调 · "+title;
   string body=test?"这只是通知通路测试，不表示额度已恢复或发布了重置公告。":n.body;
   bool submitted=statusIcon!=null&&statusIcon.ShowNotification(title,body);
   // A Shell submission is not evidence of a visible toast. Persist that exact
   // delivery stage so the inbox does not mistake worker output for delivery.
   if(!test&&data.reset!=null&&data.reset.sourceKind=="public_aggregator"&&(n.kind=="notice"||n.kind=="announced"))Send(new{action="notification_result",id=n.id,status=submitted?"submitted":"failed",reason=submitted?"windows_shell_accepted_visibility_unknown":"windows_shell_submit_failed"});
   NotificationSubmission=submitted?"已提交给 Windows；是否显示未知":"未能提交给 Windows；请查看应用内详情";
   Program.Trace("notification "+(test?"test":"change")+" "+(submitted?"submitted":"not-submitted"));
  }
  void WorkerFailed(string message){if(closing)return;snapshot.remaining=null;snapshot.state="error";snapshot.color="gray";snapshot.stale=snapshot.lastSuccess>0;snapshot.message=message;UpdateSnapshot(snapshot);}
  void UpdateSnapshot(Snapshot value){if(value.state=="ready")Program.Trace("ready success="+value.lastSuccess+" next="+value.nextRefresh);snapshot=value;lastSnapshot=DateTime.UtcNow;AccessibleName="Codex "+(value.remaining.HasValue?(value.stale?"旧值 ":"剩余 ")+value.remaining.Value+" / 100":"额度暂不可用");AccessibleDescription=value.scope+"。点击查看详情，右键打开设置。";UpdateStatusIcon();ConsumeNotification(value);BuildMenu();Invalidate();if(detail!=null&&!detail.IsDisposed)detail.UpdateValue(value);}
  void BuildMenu(){
   var menu=new ContextMenuStrip();menu.Items.Add("查看额度详情",null,delegate{ShowDetails();});menu.Items.Add("立即刷新",null,delegate{RefreshQuota();});
   var alerts=new ToolStripMenuItem(snapshot.preferences==null?"重置提醒 · 读取服务待升级":"重置消息提醒");alerts.Checked=AlertsEnabled(snapshot);alerts.Enabled=snapshot.preferences!=null;alerts.Click+=delegate{SetResetAlerts(!AlertsEnabled(snapshot));};menu.Items.Add(alerts);
   var testNotice=new ToolStripMenuItem("发送测试提醒");testNotice.Enabled=snapshot.preferences!=null&&AlertsEnabled(snapshot);testNotice.Click+=delegate{RequestTestNotification();};menu.Items.Add(testNotice);
   var periods=new ToolStripMenuItem("自动刷新间隔");foreach(int seconds in new[]{60,120,300}){int n=seconds;var item=new ToolStripMenuItem((seconds/60)+" 分钟");item.Checked=snapshot.intervalSeconds==seconds;item.Click+=delegate{Send(new{action="interval",seconds=n});};periods.DropDownItems.Add(item);}menu.Items.Add(periods);
   if(snapshot.choices!=null&&snapshot.choices.Length>0){var buckets=new ToolStripMenuItem("显示哪个额度桶");foreach(var c in snapshot.choices){string id=c.id;var item=new ToolStripMenuItem(c.name.Replace("&","&&"));item.Checked=snapshot.bucket==id;item.Click+=delegate{Send(new{action="select",bucket=id});};buckets.DropDownItems.Add(item);}menu.Items.Add(buckets);}
   if(Program.Demo==""){menu.Items.Add("设置 · 一键配置…",null,delegate{CloseDetails();SetupIntegration.Show();});var startup=new ToolStripMenuItem("随 Codex 启动");startup.Checked=StartupIntegration.Enabled;startup.Click+=delegate{try{StartupIntegration.SetEnabled(!StartupIntegration.Enabled);BuildMenu();}catch{MessageBox.Show("无法修改启动设置，请确认启动组件在同一目录。","Codex 额度");}};menu.Items.Add(startup);}
   menu.Items.Add("选择 Codex 可执行文件…",null,delegate{using(var picker=new OpenFileDialog()){picker.Title="选择官方 Codex CLI 的 codex.exe";picker.Filter="Codex 可执行文件|codex.exe|可执行文件|*.exe";if(picker.ShowDialog()==DialogResult.OK)Send(new{action="executable",executable=picker.FileName});}});
   if(Program.Demo!=""){var scenarios=new ToolStripMenuItem("DEMO · 切换示例");foreach(string v in new[]{"100","60","59","20","19","0","75","multi","error","login"}){string s=v;var item=new ToolStripMenuItem(v);item.Click+=delegate{Send(new{action="demo",scenario=s});};scenarios.DropDownItems.Add(item);}menu.Items.Add(scenarios);}
   menu.Items.Add(new ToolStripSeparator());menu.Items.Add("退出",null,delegate{Close();});ContextMenuTheme.Apply(menu,ContextMenuTheme.ReadPreference(),ContextMenuTheme.DpiAtCursor(IntPtr.Zero));var old=ContextMenuStrip;ContextMenuStrip=menu;if(old!=null)old.Dispose();
  }

  internal void CloseDetails(){if(detail!=null&&!detail.IsDisposed){detail.Close();detail=null;}}
  internal void ShowContextMenu(Form anchor=null){if(ContextMenuStrip!=null)ContextMenuTheme.Apply(ContextMenuStrip,ContextMenuTheme.ReadPreference(),ContextMenuTheme.DpiAtCursor(anchor==null?IntPtr.Zero:anchor.Handle));if(anchor==null){CloseDetails();Native.SetForegroundWindow(Handle);if(ContextMenuStrip!=null)ContextMenuStrip.Show(Cursor.Position);}else{Native.SetForegroundWindow(anchor.Handle);if(ContextMenuStrip!=null)ContextMenuStrip.Show(anchor,anchor.PointToClient(Cursor.Position));}}
  internal void ShowDetails(){
   // Full x64 packages supply B; older/ARM64 packages retain native details.
   string maker=Path.Combine(AppDomain.CurrentDomain.BaseDirectory,@"popup\MakerPopup.exe");
   if(Program.Demo==""&&File.Exists(maker)&&new[]{"Microsoft.Web.WebView2.Core.dll","Microsoft.Web.WebView2.WinForms.dll","WebView2Loader.dll"}.All(file=>File.Exists(Path.Combine(Path.GetDirectoryName(maker),file)))&&File.Exists(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,@"preview\desktop-popup\index.html"))){try{using(var child=PopupActivation.Launcher.Start(maker,Program.Trace)){}return;}catch{Program.Trace("maker popup unavailable; native detail fallback");}}
   if(detail!=null&&!detail.IsDisposed){CloseDetails();return;}
   IntPtr bar=Native.FindWindow("Shell_TrayWnd",null);dpi=Math.Max(96,Native.Dpi(bar));
   detail=new DetailPopup(this,snapshot,dpi,Surface,Border);detail.Show();detail.Activate();
  }
  internal void PlacePopup(Form popup){
   Rectangle iconRect;Rectangle work;
   if(StatusIcon.TryBounds(Program.Demo!="",out iconRect)){work=Screen.FromRectangle(iconRect).WorkingArea;}
   else{work=Screen.PrimaryScreen.WorkingArea;iconRect=new Rectangle(work.Right-32,work.Bottom,32,32);}
   int x=iconRect.Right-popup.Width,y=iconRect.Top-popup.Height-8;
   if(y<work.Top)y=iconRect.Bottom+8;
   popup.Location=new Point(Math.Max(work.Left,Math.Min(x,work.Right-popup.Width)),Math.Max(work.Top,Math.Min(y,work.Bottom-popup.Height)));
  }
  bool WorkerAlive(){return worker!=null&&worker.Alive;}
  void Pulse(object sender,EventArgs e){
   if(statusIcon!=null)statusIcon.Register();
   if(closing)return;if(DateTime.UtcNow>=retryWorkerAfter&&!WorkerAlive())StartWorker();
   if(snapshot.state=="ready"&&(DateTime.UtcNow-lastSnapshot).TotalSeconds>Math.Max(300,snapshot.intervalSeconds*2)){WorkerFailed("额度长时间未更新，旧值已过期；正在自动重试。");RefreshQuota();}
  }
  void PowerChanged(object sender,PowerModeChangedEventArgs e){if(e.Mode==PowerModes.Resume)UI(delegate{snapshot.remaining=null;snapshot.color="gray";UpdateStatusIcon();RefreshQuota();});}
  void DisplaysChanged(object sender,EventArgs e){UI(delegate{UpdateStatusIcon();CloseDetails();});}
  void ThemeChanged(object sender,UserPreferenceChangedEventArgs e){UI(delegate{ReadTheme();UpdateStatusIcon();CloseDetails();});}
  void NetworkChanged(object sender,NetworkAvailabilityEventArgs e){if(e.IsAvailable)UI(delegate{RefreshQuota();});}
  protected override void WndProc(ref Message m){
   if((uint)m.Msg==Program.Wake){if(detail!=null&&!detail.IsDisposed)detail.Activate();else ShowDetails();return;}
   if((uint)m.Msg==StatusIcon.TaskbarCreated){if(statusIcon!=null)statusIcon.ShellRestarted();return;}
   if(m.Msg==StatusIcon.Callback){
    int action=(int)(m.LParam.ToInt64()&0xffff);
    if(action==0x400||action==0x401){ShowDetails();return;}
    if(action==0x7b&&ContextMenuStrip!=null){ShowContextMenu();return;}
   }
   base.WndProc(ref m);
  }
  protected override void OnFormClosing(FormClosingEventArgs e){closing=true;pulse.Stop();CloseDetails();if(statusIcon!=null)statusIcon.Dispose();SystemEvents.PowerModeChanged-=PowerChanged;SystemEvents.DisplaySettingsChanged-=DisplaysChanged;SystemEvents.UserPreferenceChanged-=ThemeChanged;NetworkChange.NetworkAvailabilityChanged-=NetworkChanged;
   if(worker!=null){var previous=worker;worker=null;previous.Stop();}
   pulse.Dispose();base.OnFormClosing(e);
  }
 }
}

namespace PopupActivation {
 using System;
 using System.Diagnostics;
 using System.Globalization;
 using System.Runtime.InteropServices;
 using System.Threading;

 internal static class ActivationNative {
  [DllImport("user32.dll")] internal static extern bool AllowSetForegroundWindow(uint pid);
  [DllImport("user32.dll")] internal static extern System.IntPtr GetForegroundWindow();
 }
 internal sealed class Ticket {
  internal const string Prefix="Local\\MakerPopup.N52.";
  internal bool Provided,Ready,ParentGrantAccepted,Valid=true;internal long Origin;internal string Reason="legacy_no_parent_grant";
  internal static bool ValidName(string name){Guid value;return name!=null&&name.StartsWith(Prefix,StringComparison.Ordinal)&&name.Length==Prefix.Length+32&&Guid.TryParseExact(name.Substring(Prefix.Length),"N",out value);}
  internal static Ticket Read(string[] args){
   var ticket=new Ticket{Origin=ActivationNative.GetForegroundWindow().ToInt64()};string name=null,origin=null;
   for(int i=0;i<args.Length;i++){
    if(args[i]=="--foreground-ticket"){if(name!=null||i+1>=args.Length){ticket.Valid=false;break;}name=args[++i];}
    else if(args[i]=="--foreground-origin"){if(origin!=null||i+1>=args.Length){ticket.Valid=false;break;}origin=args[++i];}
   }
   ticket.Provided=name!=null||origin!=null;if(!ticket.Provided&&ticket.Valid)return ticket;
   long parsed;
   if(!ticket.Valid||!ValidName(name)||!long.TryParse(origin,NumberStyles.AllowLeadingSign,CultureInfo.InvariantCulture,out parsed)){ticket.Valid=false;ticket.Reason="invalid_parent_ticket";return ticket;}
   ticket.Origin=parsed;
   try{using(var ready=EventWaitHandle.OpenExisting(name+".ready"))using(var ack=EventWaitHandle.OpenExisting(name+".ack"))using(var granted=EventWaitHandle.OpenExisting(name+".granted")){ticket.Ready=ready.WaitOne(1500);ticket.ParentGrantAccepted=ticket.Ready&&granted.WaitOne(0);ack.Set();ticket.Valid=ticket.Ready;ticket.Reason=ticket.Ready?"parent_grant_attempt_completed":"parent_grant_timeout";}}
   catch{ticket.Valid=false;ticket.Reason="parent_grant_ticket_missing";}
   return ticket;
  }
 }
 internal static class Launcher {
  static int leases;internal static int ActiveLeases{get{return Interlocked.CompareExchange(ref leases,0,0);}}
  internal static Process Start(string exe,Action<string> log){return StartCore(exe,log,ActivationNative.AllowSetForegroundWindow,ActivationNative.GetForegroundWindow,null);}
  // The injected operations are for the non-GUI console handshake check only.
  internal static Process StartCore(string exe,Action<string> log,Func<uint,bool> grant,Func<IntPtr> foreground,Action<ProcessStartInfo> configure){
   if(Interlocked.Increment(ref leases)>8){Interlocked.Decrement(ref leases);ThreadPool.QueueUserWorkItem(delegate{if(log!=null)log("popup foreground handoff busy");});return null;}
   string name=Ticket.Prefix+Guid.NewGuid().ToString("N");EventWaitHandle ready=null,ack=null,granted=null;Process child=null;
   try{
    ready=new EventWaitHandle(false,EventResetMode.ManualReset,name+".ready");ack=new EventWaitHandle(false,EventResetMode.ManualReset,name+".ack");granted=new EventWaitHandle(false,EventResetMode.ManualReset,name+".granted");
    long origin=foreground().ToInt64();var start=new ProcessStartInfo(exe,"--foreground-ticket "+name+" --foreground-origin "+origin.ToString(CultureInfo.InvariantCulture)){UseShellExecute=false,CreateNoWindow=true,WorkingDirectory=System.IO.Path.GetDirectoryName(exe)};if(configure!=null)configure(start);
    child=Process.Start(start);if(child==null)throw new InvalidOperationException("Popup did not start");uint pid=(uint)child.Id;bool allowed=false;try{allowed=grant(pid);}catch{}
    // Ready means the grant was attempted, not that the OS accepted activation.
    if(allowed)granted.Set();ready.Set();var readyLease=ready;var ackLease=ack;var grantedLease=granted;
    ThreadPool.QueueUserWorkItem(delegate{try{bool received=ackLease.WaitOne(5000);if(log!=null)log("popup foreground handoff pid="+pid+" allowed="+allowed+" acknowledged="+received);}catch{}finally{readyLease.Dispose();ackLease.Dispose();grantedLease.Dispose();Interlocked.Decrement(ref leases);}});
    return child;
   }catch{if(child!=null)child.Dispose();if(ready!=null)ready.Dispose();if(ack!=null)ack.Dispose();if(granted!=null)granted.Dispose();Interlocked.Decrement(ref leases);throw;}
  }
 }
}
