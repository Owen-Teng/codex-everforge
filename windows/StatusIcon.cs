using System;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.IO;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;
using System.Windows.Forms;

namespace CodexQuota {
 // Monotonic, bounded retry state, separately exercised without a live Shell.
 internal sealed class ShellRegistration {
  internal bool Registered {get;private set;}
  long nextAttempt;int failures;
  internal void Lost(){Registered=false;nextAttempt=0;failures=0;}
  internal bool Ensure(long now,Func<bool> add){
   if(Registered)return true;if(now<nextAttempt)return false;
   if(add()){Registered=true;failures=0;return true;}
   failures=Math.Min(6,failures+1);nextAttempt=now+Math.Min(30000,1000L<<(failures-1));return false;
  }
 }
 internal sealed class StatusIcon:IDisposable {
  internal const int Callback=0x580;
  internal static readonly uint TaskbarCreated=Native.RegisterWindowMessage("TaskbarCreated");
  internal static Guid Identifier(bool demo,bool brand=false){
   string seed=brand?(demo?"62a61e46-4b73-441f-a9fa-0f78e29b6261":"ea9e3f75-c939-4e73-9122-56ea3caed326"):(demo?"75506648-f843-4f35-9725-fbbd99b98935":"96676735-c5d2-48f1-96b5-4553a6fb177f");
   string location=Path.GetFullPath(AppDomain.CurrentDomain.BaseDirectory).TrimEnd(Path.DirectorySeparatorChar);
   string installed=Path.GetFullPath(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","CodexQuotaMonitor"));
   if(string.Equals(location,installed,StringComparison.OrdinalIgnoreCase))return new Guid(seed);
   // Windows binds an unsigned icon GUID to its executable path. Portable copies
   // need a separate stable identity so they cannot block the installed version.
   using(var hash=SHA256.Create()){byte[] digest=hash.ComputeHash(Encoding.UTF8.GetBytes(seed+"\n"+location.ToUpperInvariant()));var bytes=new byte[16];Array.Copy(digest,bytes,16);return new Guid(bytes);}
  }
  [StructLayout(LayoutKind.Sequential,CharSet=CharSet.Unicode)] struct Data {
   internal uint size;internal IntPtr window;internal uint id,flags,message;internal IntPtr icon;
   [MarshalAs(UnmanagedType.ByValTStr,SizeConst=128)] internal string tip;
   internal uint state,mask;
   [MarshalAs(UnmanagedType.ByValTStr,SizeConst=256)] internal string info;
   internal uint version;
   [MarshalAs(UnmanagedType.ByValTStr,SizeConst=64)] internal string title;
   internal uint infoFlags;internal Guid guid;internal IntPtr balloonIcon;
  }
  [StructLayout(LayoutKind.Sequential)] struct Id {internal uint size;internal IntPtr window;internal uint id;internal Guid guid;}
  [DllImport("shell32.dll",CharSet=CharSet.Unicode,EntryPoint="Shell_NotifyIconW")] static extern bool Notify(uint action,ref Data data);
  [DllImport("shell32.dll")] static extern int Shell_NotifyIconGetRect(ref Id id,out Native.Rect rect);
  readonly IntPtr window;readonly Guid guid;Icon icon,appIcon;TaskbarStatus component;string tooltip="Codex 额度";bool disposed,dark;
  readonly ShellRegistration registration=new ShellRegistration();readonly System.Diagnostics.Stopwatch clock=System.Diagnostics.Stopwatch.StartNew();
  IntPtr shellWindow;bool hasValue;int? renderedValue;Color renderedColor;bool renderedDemo,renderedDark;
  readonly Timer fade=new Timer();int frame;int? previous,current;Color tint;
  internal StatusIcon(IntPtr window,bool demo){this.window=window;guid=Identifier(demo);appIcon=ApplicationIcon();component=new TaskbarStatus(window,guid,appIcon);fade.Interval=50;fade.Tick+=delegate{if(++frame>=5)fade.Stop();component.UpdateValue(current,Color.FromArgb(Math.Min(255,175+frame*16),tint),dark,tooltip);};}
  Data Create(){return new Data{size=(uint)Marshal.SizeOf(typeof(Data)),window=window,id=1,guid=guid,flags=1|2|4|0x20|0x80,message=Callback,icon=icon==null?IntPtr.Zero:icon.Handle,tip=tooltip,info="",title=""};}
  internal void ShellRestarted(){registration.Lost();Register();}
  internal void Register(){
   if(disposed||icon==null)return;
   IntPtr shell=Native.FindWindow("Shell_TrayWnd",null);
   if(shell!=shellWindow){shellWindow=shell;registration.Lost();}
   if(shell!=IntPtr.Zero)registration.Ensure(clock.ElapsedMilliseconds,delegate{var data=Create();if(!Notify(0,ref data))return false;data.version=4;Notify(4,ref data);return true;});
   component.Place();
  }
  internal void Update(int? remaining,Color color,bool demo,bool dark=false){
   if(disposed)return;
   if(hasValue&&renderedValue==remaining&&renderedColor==color&&renderedDemo==demo&&renderedDark==dark)return;
   hasValue=true;renderedValue=remaining;renderedColor=color;renderedDemo=demo;renderedDark=dark;
   tooltip="Codex 额度 · "+(remaining.HasValue?"剩余 "+remaining.Value+" / 100":"正在读取或暂不可用")+(demo?" · DEMO 示例数据":"");
   fade.Stop();current=remaining;tint=color;frame=0;this.dark=dark;
   bool animate=remaining.HasValue&&previous.HasValue&&remaining!=previous&&Native.MotionEnabled;
   // Keep the essential value in the Shell icon as a fallback. A failed wide
   // surface must never silently leave the user with only a brand logo.
   Replace(NumericFallback(remaining,color));component.UpdateValue(remaining,animate?Color.FromArgb(175,color):color,dark,tooltip);Register();if(animate)fade.Start();if(remaining.HasValue)previous=remaining;
  }
  static Icon NumericFallback(int? value,Color color){
   using(var bitmap=new Bitmap(64,64))using(var g=Graphics.FromImage(bitmap))using(var path=new GraphicsPath())using(var family=new FontFamily("Segoe UI Semibold"))using(var brush=new SolidBrush(color)){
    g.Clear(Color.Transparent);g.SmoothingMode=SmoothingMode.AntiAlias;
    path.AddString(value.HasValue?value.Value.ToString():"--",family,0,52,new PointF(),StringFormat.GenericTypographic);var bounds=path.GetBounds();float scale=Math.Min(56/bounds.Width,50/bounds.Height);
    using(var transform=new Matrix(scale,0,0,scale,(64-bounds.Width*scale)/2-bounds.Left*scale,(64-bounds.Height*scale)/2-bounds.Top*scale))path.Transform(transform);
    g.FillPath(brush,path);return FromBitmap(bitmap);
   }
  }
  void Replace(Icon next){var old=icon;icon=next;try{if(registration.Registered){var data=Create();if(!Notify(1,ref data))registration.Lost();}}finally{if(old!=null)old.Dispose();}}
  internal static Icon ApplicationIcon(){
   try{string path=DesktopDetector.IconPath();if(path!=null){var found=Icon.ExtractAssociatedIcon(path);if(found!=null)return found;}}catch{}
   // A neutral C is a fallback when the desktop app is not running. No official asset is bundled.
   using(var bitmap=new Bitmap(32,32))using(var g=Graphics.FromImage(bitmap))using(var font=new Font("Segoe UI",23,FontStyle.Regular,GraphicsUnit.Pixel)){
    g.Clear(Color.Transparent);g.TextRenderingHint=System.Drawing.Text.TextRenderingHint.AntiAliasGridFit;
    using(var format=new StringFormat{Alignment=StringAlignment.Center,LineAlignment=StringAlignment.Center})g.DrawString("C",font,Brushes.Gray,new RectangleF(0,0,32,32),format);
    return FromBitmap(bitmap);
   }
  }
  static Icon FromBitmap(Bitmap bitmap){IntPtr handle=bitmap.GetHicon();try{using(var borrowed=Icon.FromHandle(handle))return (Icon)borrowed.Clone();}finally{Native.DestroyIcon(handle);}}
  internal static bool TryBounds(bool demo,out Rectangle rect,bool brand=false){
   return TryBounds(Identifier(demo,brand),out rect);
  }
  internal static bool TryBounds(Guid guid,out Rectangle rect){
   if(TaskbarStatus.FindBounds(guid,out rect))return true;
   return TryShellBounds(guid,out rect);
  }
  internal static bool TryShellBounds(Guid guid,out Rectangle rect){
   var id=new Id{size=(uint)Marshal.SizeOf(typeof(Id)),guid=guid};Native.Rect r;
   LastShellBoundsResult=Shell_NotifyIconGetRect(ref id,out r);rect=Rectangle.FromLTRB(r.left,r.top,r.right,r.bottom);return LastShellBoundsResult==0;
  }
  [ThreadStatic] internal static int LastShellBoundsResult;
  [ThreadStatic] internal static string LastShellCheck;
  [ThreadStatic] internal static IntPtr LastShellBar;
  [ThreadStatic] internal static Rectangle LastShellBarBounds;
  internal static bool InTaskbar(Guid guid){
   return TaskbarStatus.VisibleInTaskbar(guid);
  }
  internal static bool ShellInTaskbar(Guid guid){
   Rectangle rect;return TryShellBounds(guid,out rect)&&ShellInTaskbar(guid,rect);
  }
  internal static bool ShellInTaskbar(Guid guid,Rectangle rect){
   Native.Rect bar;IntPtr h=Native.FindWindow("Shell_TrayWnd",null);
   LastShellBar=h;LastShellBarBounds=Rectangle.Empty;
   if(rect.Width<=0||rect.Height<=0){LastShellCheck="invalid-target-size";return false;}
   if(h==IntPtr.Zero){LastShellCheck="shell-window-missing";return false;}
   if(!Native.GetWindowRect(h,out bar)){LastShellCheck="shell-bounds-unavailable";return false;}
   LastShellBarBounds=Rectangle.FromLTRB(bar.left,bar.top,bar.right,bar.bottom);
   if(!LastShellBarBounds.Contains(rect)){LastShellCheck="target-outside-shell";return false;}
   // A GUID rectangle plus native hit-test avoids synchronous UIA providers on
   // our UI thread. Ambiguous/covered rectangles do not get an overlay.
   bool hit=TaskbarStatus.ShellAtPoint(guid,h,new Point(rect.Left+rect.Width/2,rect.Top+rect.Height/2));LastShellCheck=hit?"matched-shell":"hit-not-shell";return hit;
  }
  static object Describe(bool demo,bool brand){Rectangle rect;bool found=TryBounds(demo,out rect,brand);
   bool inBar=found&&InTaskbar(Identifier(demo,brand));
   return new {registered=found,inTaskbar=inBar,x=rect.X,y=rect.Y,width=rect.Width,height=rect.Height};
  }
  internal static object Diagnostic(bool demo){return new{mode="taskbar-component",combined=Describe(demo,false),surface=TaskbarStatus.SurfaceDiagnostic(Identifier(demo)),legacyLogo=Describe(demo,true)};}
  internal void Focus(){if(disposed)return;var data=Create();Notify(3,ref data);}
  internal bool ShowNotification(string title,string body){
   if(disposed||icon==null)return false;
   // NIF_INFO + NIF_GUID modifies our existing status item. No extra icon,
   // browser permission or change to the approved TaskbarStatus renderer.
   var data=Create();data.flags=0x10|0x20|0x40;data.title=BalloonText(title,63);data.info=BalloonText(body,255);data.infoFlags=1|0x80;
   return Notify(1,ref data); // OS submission only; display/read state is unknown.
  }
  static string BalloonText(string text,int max){text=(text??"").Replace('\0',' ');if(text.Length<=max)return text;int end=max;if(char.IsHighSurrogate(text[end-1]))--end;return text.Substring(0,end);}
  public void Dispose(){if(disposed)return;fade.Stop();fade.Dispose();component.Dispose();var data=Create();Notify(2,ref data);disposed=true;if(icon!=null)icon.Dispose();if(appIcon!=null)appIcon.Dispose();}
 }
}
