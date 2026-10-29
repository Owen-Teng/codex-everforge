using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.IO;
using System.Linq;
using System.Text;
using System.Web.Script.Serialization;
using System.Windows.Forms;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;

namespace MakerPopup {
 internal sealed class PopupForm:Form {
  readonly WebView2 web=new WebView2();readonly JavaScriptSerializer json=new JavaScriptSerializer{MaxJsonLength=2097152,RecursionLimit=20};
  readonly Timer quotaPoll=new Timer{Interval=5000},poll=new Timer{Interval=750},trayDismiss=new Timer{Interval=1500};
  readonly LocalQuotaReader quotaReader=new LocalQuotaReader();readonly ReplayReader reader;readonly List<ReplayNotice> notes=new List<ReplayNotice>();
  readonly string dataRoot=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"MakerDesktop");
  readonly HostStateWriter writer=new HostStateWriter(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"host-state.json"));
  readonly Queue<object[]> events=new Queue<object[]>();readonly Stopwatch uptime=Stopwatch.StartNew();readonly PopoverSession session=new PopoverSession();
  readonly bool allowLive;ReplayToast toast;bool ready,closing,quotaBusy,preferenceBusy,quotaRefreshRequested,escapePending,recordQueued,layoutQueued;
  bool lastForegroundAccepted,lastSecondaryGrant,parentTicketReady,parentGrantAccepted;long expectedOrigin;int activationRequests,activatedCount,deactivatedCount,escapeKeys,dismissals,preferenceVersion;
  string runtime="",phase="idle",route="card",anchorKind="work-area-fallback",ticketReason="";int activeAnimations,notifications,liveNotifications,syntheticNotifications;
  bool toastKeptFocus=true;Rectangle popupWork=Rectangle.Empty,trayRect=Rectangle.Empty;IntPtr trayWindow;Size roundedSize=Size.Empty;int roundedDpi;

  internal PopupForm(string noticeFile,bool allowLive,PopupActivation.Ticket ticket){
   this.allowLive=allowLive;expectedOrigin=ticket.Origin;parentTicketReady=ticket.Ready;parentGrantAccepted=ticket.ParentGrantAccepted;ticketReason=ticket.Reason;
   Text=Program.Title;FormBorderStyle=FormBorderStyle.None;ShowInTaskbar=false;MinimizeBox=false;MaximizeBox=false;TopMost=false;
   StartPosition=FormStartPosition.Manual;AutoScaleMode=AutoScaleMode.None;BackColor=Color.FromArgb(16,32,45);web.Dock=DockStyle.Fill;Controls.Add(web);
   reader=new ReplayReader(noticeFile,allowLive);quotaPoll.Tick+=delegate{ReadQuota();};poll.Tick+=delegate{ReadNotices();};
   trayDismiss.Tick+=delegate{trayDismiss.Stop();session.WaitingForTray=false;if(Visible&&Native.GetForegroundWindow()!=Handle)HidePopover("tray-command-timeout");};
   Activated+=delegate{activatedCount++;trayDismiss.Stop();session.WaitingForTray=false;Trace("activated");BeginInvoke((Action)FocusBrowser);};
   Deactivate+=delegate{deactivatedCount++;if(closing||!Visible)return;if(PointerOnOwnTray()&&(Native.GetAsyncKeyState(1)&0x8000)!=0){session.WaitingForTray=true;trayDismiss.Start();Trace("deactivated-own-tray");}else HidePopover("deactivated");};
   web.GotFocus+=delegate{Trace("web-focus");};web.LostFocus+=delegate{Trace("web-blur");};
   // Browser input callbacks must return without COM, disk I/O or waiting.
   web.KeyDown+=delegate(object sender,KeyEventArgs e){if(e.KeyCode==Keys.Escape){escapeKeys++;BeginInvoke((Action)delegate{Trace("web-escape");});}};
   Shown+=delegate{session.Show(false);RequestForeground();Initialize();};
  }
  protected override bool ShowWithoutActivation{get{return true;}}
  protected override CreateParams CreateParams{get{var value=base.CreateParams;value.ClassStyle|=0x00020000;return value;}}
  protected override void OnLoad(EventArgs e){RefreshAnchor();MinimumSize=MaximumSize=Size.Empty;Bounds=new Rectangle(popupWork.Left+12,popupWork.Top+12,100,100);LayoutSurface();base.OnLoad(e);}
  internal static Rectangle Clamp(Rectangle r,Rectangle work,int inset){inset=Math.Max(0,Math.Min(inset,Math.Min(work.Width,work.Height)/4));int width=Math.Max(1,Math.Min(r.Width,work.Width-inset*2)),height=Math.Max(1,Math.Min(r.Height,work.Height-inset*2));return new Rectangle(Math.Max(work.Left+inset,Math.Min(r.X,work.Right-inset-width)),Math.Max(work.Top+inset,Math.Min(r.Y,work.Bottom-inset-height)),width,height);}
  int Dpi(){try{return Math.Max(96,(int)Native.GetDpiForWindow(Handle));}catch{return 96;}}
  void RefreshAnchor(){
   trayWindow=Native.FindOwnTray();Native.Rect rect;
   if(trayWindow!=IntPtr.Zero&&Native.IsWindowVisible(trayWindow)&&Native.GetWindowRect(trayWindow,out rect)&&rect.right>rect.left&&rect.bottom>rect.top){trayRect=rect.Value;popupWork=Screen.FromRectangle(trayRect).WorkingArea;anchorKind="owned-taskbar-surface";}
   else{trayRect=Rectangle.Empty;popupWork=popupWork.IsEmpty?Screen.FromPoint(Cursor.Position).WorkingArea:Screen.FromRectangle(popupWork).WorkingArea;anchorKind="work-area-fallback";}
  }
  void LayoutSurface(){
   if(closing)return;Rectangle work;if(session.Mode==SurfaceMode.Card)work=Screen.FromRectangle(Bounds).WorkingArea;else{RefreshAnchor();work=popupWork;}
   MinimumSize=MaximumSize=Size.Empty;Bounds=PopupGeometry.Place(work,trayRect,Dpi(),session.Mode);MinimumSize=MaximumSize=Size;ApplyRoundClip();RequestRecord();
  }
  void ApplyRoundClip(){int dpi=Dpi();if(Size==roundedSize&&dpi==roundedDpi)return;roundedSize=Size;roundedDpi=dpi;var old=Region;Region=PopupGeometry.Clip(Size,dpi);if(old!=null)old.Dispose();}
  void QueueLayout(){if(layoutQueued||closing)return;layoutQueued=true;BeginInvoke((Action)delegate{layoutQueued=false;if(!closing)LayoutSurface();});}

  async void Initialize(){try{
   runtime=CoreWebView2Environment.GetAvailableBrowserVersionString();Directory.CreateDirectory(dataRoot);
   string assets=new[]{@"..\preview\desktop-popup",@"..\..\..\preview\desktop-popup"}.Select(relative=>Path.GetFullPath(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,relative))).FirstOrDefault(path=>File.Exists(Path.Combine(path,"index.html")));if(assets==null)throw new FileNotFoundException();
   var environment=await CoreWebView2Environment.CreateAsync(null,Path.Combine(dataRoot,"WebView"));if(closing)return;await web.EnsureCoreWebView2Async(environment);if(closing)return;
   web.ZoomFactor=1;var settings=web.CoreWebView2.Settings;settings.IsNonClientRegionSupportEnabled=false;settings.AreDevToolsEnabled=false;settings.AreDefaultContextMenusEnabled=false;settings.IsStatusBarEnabled=false;settings.IsZoomControlEnabled=false;settings.AreBrowserAcceleratorKeysEnabled=false;
   settings.AreHostObjectsAllowed=false;settings.IsPasswordAutosaveEnabled=false;settings.IsGeneralAutofillEnabled=false;
   await web.CoreWebView2.AddScriptToExecuteOnDocumentCreatedAsync(PopupScripts.Guard);if(closing)return;
   web.CoreWebView2.SetVirtualHostNameToFolderMapping("maker-popup.invalid",assets,CoreWebView2HostResourceAccessKind.Deny);
   web.CoreWebView2.AddWebResourceRequestedFilter("*",CoreWebView2WebResourceContext.All);
   web.CoreWebView2.WebResourceRequested+=delegate(object sender,CoreWebView2WebResourceRequestedEventArgs e){Uri uri;if(!Uri.TryCreate(e.Request.Uri,UriKind.Absolute,out uri)||uri.Scheme!="https"||uri.Host!="maker-popup.invalid"||!uri.IsDefaultPort||!PopupAssets.Paths.Contains(uri.AbsolutePath))e.Response=web.CoreWebView2.Environment.CreateWebResourceResponse(Stream.Null,403,"Blocked","Content-Type: text/plain\r\n");};
   web.CoreWebView2.NavigationStarting+=delegate(object sender,CoreWebView2NavigationStartingEventArgs e){if(e.Uri!="https://maker-popup.invalid/index.html")e.Cancel=true;};
   web.CoreWebView2.NewWindowRequested+=delegate(object sender,CoreWebView2NewWindowRequestedEventArgs e){e.Handled=true;};
   web.CoreWebView2.PermissionRequested+=delegate(object sender,CoreWebView2PermissionRequestedEventArgs e){e.State=CoreWebView2PermissionState.Deny;};
   web.CoreWebView2.DownloadStarting+=delegate(object sender,CoreWebView2DownloadStartingEventArgs e){e.Cancel=true;};
   web.CoreWebView2.WebMessageReceived+=MessageReceived;web.CoreWebView2.Navigate("https://maker-popup.invalid/index.html");
  }catch(Exception error){if(closing)return;Controls.Clear();Controls.Add(new Label{Dock=DockStyle.Fill,Padding=new Padding(20),Text="Codex Everforge 无法加载界面。请核对本机 WebView2 Runtime 和本地资源。\n"+error.GetType().Name});Trace("web-initialization-failed");}}
  void MessageReceived(object sender,CoreWebView2WebMessageReceivedEventArgs e){if(e.Source!="https://maker-popup.invalid/index.html")return;try{
   string raw=e.WebMessageAsJson;if(raw.Length>8192)return;var data=json.Deserialize<Dictionary<string,object>>(raw);object value;if(!data.TryGetValue("action",out value)||!(value is string))return;string action=(string)value;
   if(action=="ready"){ready=true;Post(new{type="configuration",allowLive=allowLive});NotifyPopup();if(Visible){quotaPoll.Start();poll.Start();ReadQuota();FocusBrowser();}else if(allowLive)poll.Start();foreach(var note in notes)Post(new{type="notice",notification=note});ReadNotices();Trace("page-ready");}
   else if(action=="surface"&&data.TryGetValue("mode",out value)&&value is string)SetSurface((string)value);
   else if(action=="layout"&&data.TryGetValue("expanded",out value)&&value is bool)LayoutSurface();
   else if(action=="close"){if(session.Mode==SurfaceMode.Card)SetSurface("popup");else HidePopover("page-close");}
   else if(action=="resetAlerts"&&data.TryGetValue("enabled",out value)&&value is bool)SetResetAlerts((bool)value);
   else if(action=="openExternal"&&data.TryGetValue("url",out value)&&value is string){Uri uri;if(Uri.TryCreate((string)value,UriKind.Absolute,out uri)&&uri.Scheme=="https"&&uri.UserInfo==""&&uri.IsDefaultPort&&!uri.IsLoopback&&Uri.CheckHostName(uri.Host)==UriHostNameType.Dns&&uri.Host.Contains(".")&&!uri.Host.EndsWith(".local",StringComparison.OrdinalIgnoreCase))Process.Start(new ProcessStartInfo(uri.AbsoluteUri){UseShellExecute=true});}
   else if(action=="metrics"){if(data.TryGetValue("theme",out value)&&UiThemePreference.Valid(value))UiThemePreference.Update(value);if(data.TryGetValue("phase",out value)&&value is string&&new[]{"idle","sealed","flipping","collision","shattering","settled"}.Contains((string)value))phase=(string)value;if(data.TryGetValue("route",out value)&&value is string&&new[]{"card","duel","messages","settings"}.Contains((string)value))route=(string)value;if(data.TryGetValue("activeAnimations",out value)&&value is int)activeAnimations=Math.Max(0,Math.Min(100,(int)value));RequestRecord();}
  }catch{Trace("bridge-message-rejected");}}
  void Post(object message){if(ready&&!closing&&!IsDisposed&&web.CoreWebView2!=null)web.CoreWebView2.PostWebMessageAsJson(json.Serialize(message));}
  void NotifyPopup(){if(ready&&session.TakePageReset())Post(new{type="surface",mode="popup"});}
  void SetSurface(string mode){if(mode=="card"&&Native.GetForegroundWindow()!=Handle)return;var old=session.Mode;if(!session.Surface(mode))return;if(old!=session.Mode){LayoutSurface();Trace("surface-"+mode);if(mode=="popup"){session.RequestPageReset();NotifyPopup();}}}

  async void SetResetAlerts(bool enabled){if(preferenceBusy){Post(new{type="preferenceError"});return;}preferenceBusy=true;preferenceVersion++;try{var state=await quotaReader.SetResetAlerts(enabled);if(!closing){Post(new{type="quota",status=state==null?"unavailable":"connected",state=state});if(state==null)Post(new{type="preferenceError"});}}catch{if(!closing){Post(new{type="quota",status="unavailable",state=(LocalQuotaState)null});Post(new{type="preferenceError"});}}finally{preferenceBusy=false;}}
  async void ReadQuota(){
   if(!ready||preferenceBusy||closing||!Visible)return;if(quotaBusy){quotaRefreshRequested=true;return;}quotaBusy=true;quotaRefreshRequested=false;int version=preferenceVersion,epoch=session.Epoch;
   if(quotaReader.Status!="connected")Post(new{type="quota",status="checking"});
   try{var state=await quotaReader.Read();if(!closing&&ready&&session.Current(epoch)&&!preferenceBusy&&version==preferenceVersion)Post(new{type="quota",status=state==null?"unavailable":"connected",state=state});}
   finally{quotaBusy=false;if(!closing){RequestRecord();if(Visible&&quotaRefreshRequested)ReadQuota();}}
  }
  void ReadNotices(){if(!ready||closing)return;foreach(var note in reader.Read(DateTimeOffset.UtcNow.ToUnixTimeSeconds())){notes.Add(note);if(notes.Count>20)notes.RemoveAt(0);Post(new{type="notice",notification=note});notifications++;if(note.real)liveNotifications++;else syntheticNotifications++;if(toast!=null&&!toast.IsDisposed)toast.Close();IntPtr foreground=Native.GetForegroundWindow();toast=new ReplayToast(note,Dpi(),delegate{ShowPopover(Native.GetForegroundWindow().ToInt64());Post(new{type="navigate",route="messages"});});toast.Show(this);toastKeptFocus=Native.GetForegroundWindow()==foreground;}RequestRecord();}

  void RequestForeground(){
   if(closing||!Visible)return;long before=Native.GetForegroundWindow().ToInt64();if(!PopoverSession.CanRequestForeground(expectedOrigin,before,Handle.ToInt64())){Trace("foreground-stale-origin");HidePopover("stale-open-intent");return;}
   activationRequests++;lastForegroundAccepted=Native.SetForegroundWindow(Handle);Trace(lastForegroundAccepted?"foreground-api-accepted":"foreground-api-denied");int epoch=session.Epoch;
   BeginInvoke((Action)delegate{if(closing||!session.Current(epoch))return;if(Native.GetForegroundWindow()==Handle){Trace("foreground-observed");FocusBrowser();}else{Trace("foreground-unconfirmed");HidePopover("foreground-not-acquired");}});
  }
  void FocusBrowser(){if(!closing&&ready&&Visible&&Native.GetForegroundWindow()==Handle&&!web.ContainsFocus)web.Focus();}
  void ShowPopover(long origin){if(closing)return;trayDismiss.Stop();expectedOrigin=origin;session.Show(true);LayoutSurface();NotifyPopup();if(!Visible)Show();RequestForeground();if(ready&&Visible){quotaPoll.Start();poll.Start();ReadQuota();ReadNotices();}}
  void HidePopover(string reason){if(closing||!Visible)return;trayDismiss.Stop();quotaPoll.Stop();quotaRefreshRequested=false;if(!allowLive)poll.Stop();session.Hide();Hide();LayoutSurface();NotifyPopup();dismissals++;Trace("hidden-"+reason);}
  bool PointerOnOwnTray(){
   IntPtr hit=Native.GetAncestor(Native.WindowFromPoint(Cursor.Position),2);if(trayWindow!=IntPtr.Zero&&hit==trayWindow)return true;if(!trayRect.Contains(Cursor.Position))return false;var name=new StringBuilder(64);Native.GetClassName(hit,name,name.Capacity);return name.ToString()=="Shell_TrayWnd";
  }
  void QueueEscape(){if(escapePending||closing||!Visible)return;escapePending=true;int epoch=session.Epoch;SurfaceMode mode=session.Mode;BeginInvoke((Action)(async delegate{try{if(closing||!session.Current(epoch))return;Trace("native-escape");if(!ready||web.CoreWebView2==null){HidePopover("escape-loading");return;}string result=await web.CoreWebView2.ExecuteScriptAsync(PopupScripts.Escape);if(!closing&&session.Current(epoch)&&result=="\"outer\""){if(mode==SurfaceMode.Card){if(session.Mode==SurfaceMode.Card)SetSurface("popup");}else HidePopover("native-escape");}}catch{Trace("native-escape-failed");}finally{escapePending=false;}}));}
  protected override bool ProcessCmdKey(ref Message msg,Keys keyData){if(keyData==Keys.Escape&&!web.ContainsFocus){if((msg.LParam.ToInt64()&(1L<<30))==0)QueueEscape();return true;}return base.ProcessCmdKey(ref msg,keyData);}
  void Trace(string name){if(closing)return;if(events.Count==96)events.Dequeue();Native.Rect actual;Rectangle bounds=Native.GetWindowRect(Handle,out actual)?actual.Value:Bounds;var work=Screen.FromRectangle(bounds).WorkingArea;events.Enqueue(new object[]{uptime.ElapsedMilliseconds,session.Epoch,name,session.Mode.ToString().ToLowerInvariant(),Visible,Native.GetForegroundWindow().ToInt64(),bounds.X,bounds.Y,bounds.Width,bounds.Height,Dpi(),work.X,work.Y,work.Width,work.Height});RequestRecord();}
  void RequestRecord(){if(recordQueued||closing||!IsHandleCreated)return;recordQueued=true;BeginInvoke((Action)delegate{recordQueued=false;if(!closing)Record();});}
  void Record(){try{var work=Screen.FromControl(this).WorkingArea;IntPtr foreground=Native.GetForegroundWindow();uint pid;Native.GetWindowThreadProcessId(foreground,out pid);writer.Queue(json.Serialize(new{title=Text,ready=ready,runtime=runtime,dpi=Dpi(),configuredWebZoom=1,x=Left,y=Top,width=Width,height=Height,clientWidth=ClientSize.Width,clientHeight=ClientSize.Height,workArea=new{x=work.X,y=work.Y,width=work.Width,height=work.Height},surfaceMode=session.Mode.ToString().ToLowerInvariant(),route=route,phase=phase,activeAnimations=activeAnimations,noticeState=reader.Status,noticesReceived=notifications,rejected=reader.Rejected,toastKeptFocus=toastKeptFocus,source="local_file_consumer",liveEnabled=allowLive,liveNoticesReceived=liveNotifications,syntheticNoticesReceived=syntheticNotifications,liveConnectionVerified=false,quotaBridge=quotaReader.Status,frameStyle="single-rounded-region",radiusLogical=19,showInTaskbar=ShowInTaskbar,popoverVisible=Visible,quotaPolling=quotaPoll.Enabled,noticePolling=poll.Enabled,foregroundWindow=foreground.ToInt64(),foregroundProcessId=pid,isForeground=foreground==Handle,webFocused=web.ContainsFocus,lastForegroundAccepted=lastForegroundAccepted,lastSecondaryGrant=lastSecondaryGrant,parentTicketReady=parentTicketReady,parentGrantAccepted=parentGrantAccepted,ticketReason=ticketReason,expectedOrigin=expectedOrigin,activationRequests=activationRequests,activatedCount=activatedCount,deactivatedCount=deactivatedCount,escapeKeys=escapeKeys,dismissals=dismissals,epoch=session.Epoch,waitingForTray=session.WaitingForTray,anchorKind=anchorKind,interactionEventColumns=new[]{"elapsedMs","epoch","event","mode","visible","foregroundHwnd","x","y","width","height","dpi","workX","workY","workWidth","workHeight"},interactionEvents=events.ToArray(),diagnosticWriteFailures=writer.Failures,osNotificationCenter=false}));}catch{}}
  protected override void WndProc(ref Message m){
   if(m.Msg==0x0112&&((m.WParam.ToInt64()&0xFFF0)==0xF030||(m.WParam.ToInt64()&0xFFF0)==0xF000))return;
   if((uint)m.Msg==Native.Wake){int flags=(int)m.WParam.ToInt64(),command=flags&0xff;if(command==2){Close();return;}lastSecondaryGrant=(flags&0x100)!=0;parentTicketReady=(flags&0x200)!=0;parentGrantAccepted=(flags&0x400)!=0;ticketReason=parentTicketReady?"parent_grant_attempt_completed":"legacy_no_parent_grant";Trace("wake-"+command);if(command==1){if(web.CoreWebView2!=null){ready=false;quotaPoll.Stop();web.CoreWebView2.Reload();}return;}if(command==3&&Visible){HidePopover("tray-toggle");return;}ShowPopover(m.LParam.ToInt64());return;}
   base.WndProc(ref m);if(m.Msg==0x02E0)QueueLayout();
  }
  protected override void OnFormClosing(FormClosingEventArgs e){Post(new{type="cancel"});closing=true;poll.Stop();quotaPoll.Stop();trayDismiss.Stop();trayDismiss.Dispose();quotaPoll.Dispose();quotaReader.Dispose();if(toast!=null&&!toast.IsDisposed)toast.Close();web.Dispose();poll.Dispose();writer.Dispose();base.OnFormClosing(e);}
 }
}
