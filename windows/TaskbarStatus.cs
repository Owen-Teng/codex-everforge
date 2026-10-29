using System;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Windows.Forms;

namespace CodexQuota {
 // One non-activating surface confined to our Shell button. Its owner MUST be
 // our own UI window: an Explorer owner implicitly attaches input queues.
 internal sealed class TaskbarStatus:Control {
  [DllImport("user32.dll")] static extern IntPtr GetWindow(IntPtr window,uint command);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr window);
  [DllImport("user32.dll")] static extern bool IsIconic(IntPtr window);
  [DllImport("user32.dll")] static extern bool SetWindowPos(IntPtr window,IntPtr after,int x,int y,int width,int height,uint flags);
  [DllImport("user32.dll")] static extern bool ShowWindow(IntPtr window,int command);
  [DllImport("user32.dll")] static extern IntPtr WindowFromPoint(Point point);
  [DllImport("user32.dll")] static extern IntPtr GetAncestor(IntPtr window,uint flags);
  [DllImport("user32.dll")] static extern IntPtr GetThreadDpiAwarenessContext();
  [DllImport("user32.dll")] static extern IntPtr GetWindowDpiAwarenessContext(IntPtr window);
  [DllImport("user32.dll")] static extern int GetAwarenessFromDpiAwarenessContext(IntPtr context);
  [DllImport("user32.dll")] static extern bool AreDpiAwarenessContextsEqual(IntPtr first,IntPtr second);
  [DllImport("dwmapi.dll")] static extern int DwmGetWindowAttribute(IntPtr window,int attribute,out int value,int size);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern bool SetProp(IntPtr window,string name,IntPtr value);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern IntPtr GetProp(IntPtr window,string name);
  [DllImport("user32.dll",SetLastError=true)] static extern bool UpdateLayeredWindow(IntPtr window,IntPtr destinationDC,IntPtr position,ref Size size,IntPtr sourceDC,ref Point source,uint key,ref Blend blend,uint flags);
  [DllImport("gdi32.dll")] static extern IntPtr CreateCompatibleDC(IntPtr dc);
  [DllImport("gdi32.dll")] static extern IntPtr SelectObject(IntPtr dc,IntPtr item);
  [DllImport("gdi32.dll")] static extern bool DeleteObject(IntPtr item);
  [DllImport("gdi32.dll")] static extern bool DeleteDC(IntPtr dc);
  [StructLayout(LayoutKind.Sequential,Pack=1)] struct Blend {internal byte operation,flags,alpha,format;}
  const string SurfaceProperty="CodexQuota.SurfaceSubmitted.v1",ErrorProperty="CodexQuota.SurfaceError.v1";
  readonly Guid guid;readonly IntPtr owner;readonly Icon logo;readonly ToolTip tip=new ToolTip();
  IntPtr taskbar;int dpi=96;int? remaining;Color tint;bool dark,hovered,dirty=true;Size surfaceSize;Rectangle placedBounds;string inspectionState="";
  internal const int MaxHitNodes=4096,MaxHitCandidates=512;
  internal struct HitProbe {internal IntPtr Surface,Raw,RawRoot,Resolved,Last;internal Point Point;internal int Steps,Candidates,Transparent,Invisible,Outside,Cloaked;internal string End;}
  [ThreadStatic] internal static HitProbe LastHit;
  internal string PlacementNote="正在连接任务栏";
  static string Title(Guid id){return "Codex Quota Status "+id.ToString("D");}
  internal TaskbarStatus(IntPtr owner,Guid guid,Icon logo){
   this.owner=owner;this.guid=guid;this.logo=logo;TabStop=true;Cursor=Cursors.Hand;Visible=false;SetTopLevel(true);
   SetStyle(ControlStyles.UserPaint|ControlStyles.AllPaintingInWmPaint|ControlStyles.OptimizedDoubleBuffer|ControlStyles.Selectable,true);
   AccessibleRole=AccessibleRole.PushButton;AccessibleName="Codex 额度";
  }
  protected override CreateParams CreateParams {get{var p=base.CreateParams;p.Parent=owner;p.Caption=Title(guid);p.Style=unchecked((int)0x80000000);p.ExStyle|=0x08080080;return p;}}
  internal void UpdateValue(int? value,Color color,bool isDark,string tooltip){
   bool changed=remaining!=value||tint!=color||dark!=isDark;
   remaining=value;tint=color;dark=isDark;if(AccessibleName!=tooltip){AccessibleName=tooltip;tip.SetToolTip(this,tooltip);}
   dirty|=changed;Present();
  }
  internal void Place(){
   IntPtr bar=Native.FindWindow("Shell_TrayWnd",null);Native.Rect r=new Native.Rect();Rectangle target=Rectangle.Empty;
   if(bar==IntPtr.Zero||!IsWindowVisible(bar)||IsIconic(bar)||!Native.GetWindowRect(bar,out r)){RecordPlacementFailure("bar-unavailable",bar,r,target,false,false);HideNative("任务栏暂不可用");return;}
   if(FullscreenCovers(bar)){RecordPlacementFailure("fullscreen",bar,r,target,false,false);HideNative("全屏时跟随任务栏隐藏");return;}
   if(!StatusIcon.TryShellBounds(guid,out target)){RecordPlacementFailure("shell-icon-rect-unavailable",bar,r,target,false,false);HideNative("等待本工具的状态项显示在任务栏");return;}
   if(!StatusIcon.ShellInTaskbar(guid,target)){RecordPlacementFailure(StatusIcon.LastShellCheck,bar,r,target,true,StatusIcon.LastShellCheck=="hit-not-shell");HideNative("等待本工具的状态项显示在任务栏");return;}
   int nextDpi=Math.Max(96,Native.Dpi(bar)),w=target.Width,h=target.Height;
   if(taskbar!=bar){taskbar=bar;placedBounds=Rectangle.Empty;dirty=true;}
   if(dpi!=nextDpi||surfaceSize!=new Size(w,h)){dpi=nextDpi;surfaceSize=new Size(w,h);dirty=true;}
   if(!IsHandleCreated){CreateHandle();dirty=true;}
   // Only our own HWND is positioned. No Shell owner or z-order changes.
   // Topmost is confined to a verified visible Shell rectangle; hide if covered.
   if(placedBounds!=target||!IsWindowVisible(Handle)){
    if(!SetWindowPos(Handle,new IntPtr(-1),target.X,target.Y,w,h,0x0010|0x0200)){RecordPlacementFailure("surface-position-failed",bar,r,target,true,true);HideNative("任务栏画面定位失败");return;}
    placedBounds=target;
   }
   Present();
   if(GetProp(Handle,SurfaceProperty)!=IntPtr.Zero){if(!IsWindowVisible(Handle))ShowWindow(Handle,4);PlacementNote="已提交图标和数字画面";inspectionState="placed";}
   else RecordPlacementFailure("surface-submit-failed",bar,r,target,true,true);
  }
  static object RectInfo(Rectangle rect){return new{x=rect.X,y=rect.Y,width=rect.Width,height=rect.Height};}
  static object WindowInfo(IntPtr window){
   uint pid;Native.GetWindowThreadProcessId(window,out pid);var cls=new System.Text.StringBuilder(128);if(window!=IntPtr.Zero)Native.GetClassName(window,cls,cls.Capacity);
   int style=window==IntPtr.Zero?0:Native.GetWindowLong(window,-20);return new{handle="0x"+window.ToInt64().ToString("x"),pid=pid,@class=cls.ToString(),extendedStyle="0x"+style.ToString("x8"),transparent=InputTransparentLayer(style)};
  }
  internal static object DpiInfo(IntPtr window){
   try{IntPtr thread=GetThreadDpiAwarenessContext(),context=window==IntPtr.Zero?IntPtr.Zero:GetWindowDpiAwarenessContext(window);
    return new{threadContext=thread.ToInt64(),threadAwareness=GetAwarenessFromDpiAwarenessContext(thread),threadPerMonitorV2=AreDpiAwarenessContextsEqual(thread,new IntPtr(-4)),windowContext=context.ToInt64(),windowAwareness=GetAwarenessFromDpiAwarenessContext(context),windowDpi=window==IntPtr.Zero?0:Native.Dpi(window)};
   }catch(EntryPointNotFoundException){return new{unavailable=true};}
  }
  internal static object LastHitInfo(){var p=LastHit;return new{point=new{x=p.Point.X,y=p.Point.Y},raw=WindowInfo(p.Raw),rawRoot=WindowInfo(p.RawRoot),resolved=WindowInfo(p.Resolved),lastCandidate=WindowInfo(p.Last),steps=p.Steps,visibleCandidates=p.Candidates,totalLimit=MaxHitNodes,candidateLimit=MaxHitCandidates,termination=p.End,skipped=new{transparent=p.Transparent,invisible=p.Invisible,outside=p.Outside,cloaked=p.Cloaked}};}
  void RecordPlacementFailure(string stage,IntPtr bar,Native.Rect rect,Rectangle target,bool hasTarget,bool hitChecked){
   if(!Program.Inspect||inspectionState==stage)return;inspectionState=stage;
   IntPtr surface=IsHandleCreated?Handle:IntPtr.Zero;Native.Rect actual=new Native.Rect();bool actualFound=surface!=IntPtr.Zero&&Native.GetWindowRect(surface,out actual);
   var report=new{stage=stage,targetFound=hasTarget,targetRect=RectInfo(target),shellRectHresult=StatusIcon.LastShellBoundsResult,bar=WindowInfo(bar),barRect=RectInfo(Rectangle.FromLTRB(rect.left,rect.top,rect.right,rect.bottom)),checkBar=WindowInfo(StatusIcon.LastShellBar),checkBarRect=RectInfo(StatusIcon.LastShellBarBounds),surface=WindowInfo(surface),surfaceRectFound=actualFound,surfaceRect=RectInfo(Rectangle.FromLTRB(actual.left,actual.top,actual.right,actual.bottom)),surfaceVisible=surface!=IntPtr.Zero&&IsWindowVisible(surface),surfaceSubmitted=surface!=IntPtr.Zero&&GetProp(surface,SurfaceProperty)!=IntPtr.Zero,dpi=new{bar=DpiInfo(bar),owner=DpiInfo(owner),surface=DpiInfo(surface)},hitChecked=hitChecked,hit=hitChecked?LastHitInfo():null};
   Program.Trace("taskbar placement-probe "+new System.Web.Script.Serialization.JavaScriptSerializer().Serialize(report));
  }
  internal static bool ShellAtPoint(Guid guid,IntPtr bar,Point point){
   IntPtr hit=HitBelowSurface(guid,point);return hit!=IntPtr.Zero&&GetAncestor(hit,2)==bar;
  }
  // WindowFromPoint may hit our own overlay. Walk below ONLY that surface;
  // do not toggle its enabled/visible state to probe the window underneath it.
  internal static IntPtr ResolveHit(IntPtr surface,IntPtr hit,Point point,Func<IntPtr,IntPtr> next,Func<IntPtr,Point,bool> covers){
   int steps;string end;return ResolveHit(surface,hit,point,next,covers,out steps,out end);
  }
  internal static IntPtr ResolveHit(IntPtr surface,IntPtr hit,Point point,Func<IntPtr,IntPtr> next,Func<IntPtr,Point,bool> covers,out int steps,out string end){
   int candidates;return ResolveHit(surface,hit,point,next,covers,null,out steps,out candidates,out end);
  }
  internal static IntPtr ResolveHit(IntPtr surface,IntPtr hit,Point point,Func<IntPtr,IntPtr> next,Func<IntPtr,Point,bool> covers,Func<IntPtr,bool> visible,out int steps,out int candidates,out string end){
   steps=0;candidates=0;end="direct-hit";if(surface==IntPtr.Zero||hit!=surface)return hit;
   var visited=new HashSet<IntPtr>();visited.Add(surface);IntPtr window=surface;
   for(int i=0;i<MaxHitNodes;i++){
    steps++;window=next(window);if(window==IntPtr.Zero){end="end-of-z-chain";return IntPtr.Zero;}if(!visited.Add(window)){end="cycle";return IntPtr.Zero;}
    if(visible!=null){if(!visible(window))continue;if(++candidates>MaxHitCandidates){end="visible-candidate-limit";return IntPtr.Zero;}}
    if(covers(window,point)){end="covering-window";return window;}
   }
   end="total-node-limit";return IntPtr.Zero;
  }
  static bool VisibleCandidate(IntPtr window){LastHit.Last=window;if(!IsWindowVisible(window)||IsIconic(window)){LastHit.Invisible++;return false;}return true;}
  static bool CoversPoint(IntPtr window,Point point){
   LastHit.Last=window;
   if(InputTransparentLayer(Native.GetWindowLong(window,-20))){LastHit.Transparent++;return false;}
   Native.Rect r;if(!Native.GetWindowRect(window,out r)||!Rectangle.FromLTRB(r.left,r.top,r.right,r.bottom).Contains(point)){LastHit.Outside++;return false;}
   int cloaked;try{if(DwmGetWindowAttribute(window,14,out cloaked,4)==0&&cloaked!=0){LastHit.Cloaked++;return false;}}catch(EntryPointNotFoundException){}catch(DllNotFoundException){}
   return true;
  }
  internal static bool InputTransparentLayer(int extendedStyle){return (extendedStyle&0x00080020)==0x00080020;}
  internal static IntPtr HitBelowSurface(Guid guid,Point point){
   IntPtr surface=FindSurface(guid),hit=WindowFromPoint(point),root=GetAncestor(hit,2);
   LastHit=new HitProbe{Surface=surface,Raw=hit,RawRoot=root,Point=point};
   if(surface!=IntPtr.Zero&&root==surface)hit=surface;
   else if(root!=IntPtr.Zero&&InputTransparentLayer(Native.GetWindowLong(root,-20))){surface=root;hit=root;}
   int steps,candidates;string end;IntPtr resolved=ResolveHit(surface,hit,point,delegate(IntPtr window){return GetWindow(window,2);},CoversPoint,VisibleCandidate,out steps,out candidates,out end);
   LastHit.Resolved=resolved;LastHit.Steps=steps;LastHit.Candidates=candidates;LastHit.End=end;return resolved;
  }
  static bool FullscreenCovers(IntPtr bar){
   IntPtr foreground=Native.GetForegroundWindow();Native.Rect rect;
   if(foreground==IntPtr.Zero||foreground==bar||!Native.GetWindowRect(foreground,out rect))return false;
   uint process;Native.GetWindowThreadProcessId(foreground,out process);
   if(process==(uint)System.Diagnostics.Process.GetCurrentProcess().Id)return false;
   var name=new System.Text.StringBuilder(128);Native.GetClassName(foreground,name,name.Capacity);
   if(name.ToString()=="Progman"||name.ToString()=="WorkerW")return false;
   return (Native.GetWindowLong(foreground,-16)&0x00c00000)==0&&Rectangle.FromLTRB(rect.left,rect.top,rect.right,rect.bottom).Contains(Screen.FromHandle(bar).Bounds);
  }
  void Present(){
   if(!dirty||!IsHandleCreated||surfaceSize.Width<=0||surfaceSize.Height<=0)return;
   IntPtr dc=CreateCompatibleDC(IntPtr.Zero),bitmap=IntPtr.Zero,previous=IntPtr.Zero;
   try{
    using(var frame=RenderSized(remaining,tint,logo,dpi,dark,hovered,surfaceSize.Width,surfaceSize.Height))bitmap=frame.GetHbitmap(Color.Black);
    previous=SelectObject(dc,bitmap);var source=new Point();var size=surfaceSize;var blend=new Blend{alpha=255,format=0};
    // Opaque backing matches the approved preview and covers the numeric fallback
    // beneath it. Both logo and number are in this same submitted bitmap.
    bool submitted=UpdateLayeredWindow(Handle,IntPtr.Zero,IntPtr.Zero,ref size,dc,ref source,0,ref blend,4);
    int error=submitted?0:Marshal.GetLastWin32Error();
    SetProp(Handle,SurfaceProperty,new IntPtr(submitted?1:0));SetProp(Handle,ErrorProperty,new IntPtr(error));
    dirty=!submitted;
    if(!submitted){PlacementNote="横排画面提交失败，保留数字显示";ShowWindow(Handle,0);Program.Trace("taskbar surface error: "+error);}
   }finally{if(previous!=IntPtr.Zero)SelectObject(dc,previous);if(bitmap!=IntPtr.Zero)DeleteObject(bitmap);if(dc!=IntPtr.Zero)DeleteDC(dc);}
  }
  void HideNative(string reason){if(PlacementNote!=reason)Program.Trace("taskbar: "+reason);PlacementNote=reason;if(IsHandleCreated&&IsWindowVisible(Handle))ShowWindow(Handle,0);}
  static IntPtr FindSurface(Guid guid){return Native.FindWindow(null,Title(guid));}
  internal static bool FindBounds(Guid guid,out Rectangle rect){
   rect=Rectangle.Empty;IntPtr bar=Native.FindWindow("Shell_TrayWnd",null),window=FindSurface(guid);Native.Rect r;
   uint process,ownerProcess;IntPtr localOwner=GetWindow(window,4);
   Native.GetWindowThreadProcessId(window,out process);Native.GetWindowThreadProcessId(localOwner,out ownerProcess);
   if(bar==IntPtr.Zero||window==IntPtr.Zero||localOwner==IntPtr.Zero||process!=ownerProcess||!Native.GetWindowRect(window,out r))return false;
   rect=Rectangle.FromLTRB(r.left,r.top,r.right,r.bottom);return true;
  }
  internal static bool VisibleInTaskbar(Guid guid){
   Rectangle rect;IntPtr bar=Native.FindWindow("Shell_TrayWnd",null);Native.Rect r;
   if(!FindBounds(guid,out rect)||!Native.GetWindowRect(bar,out r))return false;
   IntPtr window=FindSurface(guid);
   return GetProp(window,SurfaceProperty)!=IntPtr.Zero&&IsWindowVisible(window)&&Rectangle.FromLTRB(r.left,r.top,r.right,r.bottom).Contains(rect)&&WindowFromPoint(new Point(rect.Left+rect.Width/2,rect.Top+rect.Height/2))==window;
  }
  internal static object SurfaceDiagnostic(Guid guid){IntPtr window=FindSurface(guid);return new{renderer="process-owned-layered",submitted=GetProp(window,SurfaceProperty)!=IntPtr.Zero,error=GetProp(window,ErrorProperty).ToInt64()};}
  internal static Bitmap Render(int? value,Color color,Icon logo,int dpi,bool dark,bool hovered){return RenderSized(value,color,logo,dpi,dark,hovered,(int)Math.Round(32*dpi/96f),(int)Math.Round(48*dpi/96f));}
  static Bitmap RenderSized(int? value,Color color,Icon logo,int dpi,bool dark,bool hovered,int width,int height){
   float scale=dpi/96f,logicalWidth=width/scale,logicalHeight=height/scale;var bitmap=new Bitmap(width,height);
   using(var g=Graphics.FromImage(bitmap)){
    g.Clear(dark?Color.FromArgb(32,34,37):Color.FromArgb(238,238,238));g.ScaleTransform(scale,scale);g.SmoothingMode=SmoothingMode.AntiAlias;
    if(hovered)using(var brush=new SolidBrush(dark?Color.FromArgb(54,58,57):Color.FromArgb(250,250,250)))g.FillRectangle(brush,1,2,logicalWidth-2,logicalHeight-4);
    using(var path=new GraphicsPath())using(var family=new FontFamily("Segoe UI Semibold"))using(var ink=new SolidBrush(color)){
     path.AddString(value.HasValue?value.Value.ToString():"--",family,0,21,new PointF(),StringFormat.GenericTypographic);
     var b=path.GetBounds();float textWidth=Math.Max(23,b.Width),fit=Math.Min(1,(logicalWidth-2)/(18+2+textWidth)),left=(logicalWidth-(18+2+textWidth)*fit)/2,cy=logicalHeight/2;
     using(var mark=logo.ToBitmap())g.DrawImage(mark,new RectangleF(left,cy-9*fit,18*fit,18*fit));
     // One uniform scale for the complete composition. No condensed digits,
     // no microscopic separate logo; the complete group shares one baseline.
     using(var m=new Matrix(fit,0,0,fit,left+(20+(textWidth-b.Width)/2)*fit-b.Left*fit,cy-b.Height*fit/2-b.Top*fit))path.Transform(m);
     g.FillPath(ink,path);
    }
   }return bitmap;
  }
  protected override void OnPaint(PaintEventArgs e){Present();}
  protected override void OnMouseEnter(EventArgs e){hovered=true;dirty=true;Present();base.OnMouseEnter(e);}
  protected override void OnMouseLeave(EventArgs e){hovered=false;dirty=true;Present();base.OnMouseLeave(e);}
  void Invoke(bool menu){Native.PostMessage(owner,StatusIcon.Callback,IntPtr.Zero,new IntPtr(menu?0x7b:0x400));}
  protected override void OnMouseUp(MouseEventArgs e){base.OnMouseUp(e);if(e.Button==MouseButtons.Left||e.Button==MouseButtons.Right)Invoke(e.Button==MouseButtons.Right);}
  protected override void OnKeyDown(KeyEventArgs e){if(e.KeyCode==Keys.Enter||e.KeyCode==Keys.Space){Invoke(false);e.Handled=true;}else if(e.KeyCode==Keys.Apps||(e.KeyCode==Keys.F10&&e.Shift)){Invoke(true);e.Handled=true;}base.OnKeyDown(e);}
  protected override void Dispose(bool disposing){if(disposing)tip.Dispose();base.Dispose(disposing);}
 }
}
