using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Linq;
using System.Windows.Forms;

namespace CodexQuota {
 internal sealed class SymbolButton:Button {
  internal bool RefreshSymbol,Busy;internal float Angle;bool hovered;
  internal SymbolButton(){FlatStyle=FlatStyle.Flat;FlatAppearance.BorderSize=0;Cursor=Cursors.Hand;Text="";SetStyle(ControlStyles.OptimizedDoubleBuffer|ControlStyles.UserPaint|ControlStyles.AllPaintingInWmPaint,true);}
  protected override void OnMouseEnter(EventArgs e){hovered=true;Invalidate();base.OnMouseEnter(e);}
  protected override void OnMouseLeave(EventArgs e){hovered=false;Invalidate();base.OnMouseLeave(e);}
  protected override void OnPaint(PaintEventArgs e){
   e.Graphics.Clear(BackColor);if(hovered&&Enabled)using(var b=new SolidBrush(Color.FromArgb(18,ForeColor)))e.Graphics.FillRectangle(b,ClientRectangle);
   e.Graphics.SmoothingMode=SmoothingMode.AntiAlias;float s=Width/28f;
   using(var p=new Pen(ForeColor,1.5f*s)){p.StartCap=p.EndCap=LineCap.Round;
    if(RefreshSymbol){e.Graphics.TranslateTransform(Width/2f,Height/2f);if(Busy)e.Graphics.RotateTransform(Angle);e.Graphics.DrawArc(p,-6*s,-6*s,12*s,12*s,38,285);e.Graphics.DrawLines(p,new[]{new PointF(6*s,-7*s),new PointF(6*s,-2*s),new PointF(s,-2*s)});e.Graphics.ResetTransform();}
    else{e.Graphics.DrawLine(p,10*s,10*s,18*s,18*s);e.Graphics.DrawLine(p,18*s,10*s,10*s,18*s);}
   }
   if(Focused&&ShowFocusCues)ControlPaint.DrawFocusRectangle(e.Graphics,new Rectangle(2,2,Width-4,Height-4),ForeColor,BackColor);
  }
 }
 internal sealed class QuotaMeter:Control {
  internal double Current,Target,From;internal long Started;internal Color Track,Fill;
  internal QuotaMeter(){SetStyle(ControlStyles.OptimizedDoubleBuffer|ControlStyles.UserPaint|ControlStyles.AllPaintingInWmPaint,true);AccessibleRole=AccessibleRole.ProgressBar;}
  protected override AccessibleObject CreateAccessibilityInstance(){return new MeterAccessible(this);}
  sealed class MeterAccessible:ControlAccessibleObject {
   readonly QuotaMeter meter;internal MeterAccessible(QuotaMeter meter):base(meter){this.meter=meter;}
   public override AccessibleRole Role {get{return AccessibleRole.ProgressBar;}}
   public override string Value {get{return Math.Round(meter.Target*100).ToString();}set{}}
   public override AccessibleStates State {get{return base.State|AccessibleStates.ReadOnly;}}
  }
  internal void Set(int remaining,Color color,bool animate,long now){double next=Math.Max(0,Math.Min(100,remaining))/100.0;Fill=color;From=Current;Target=next;Started=now;if(!animate)Current=Target;Invalidate();}
  internal bool Tick(long now){if(Current==Target)return false;double t=Math.Min(1,(now-Started)/300.0);Current=t>=1?Target:From+(Target-From)*(1-Math.Pow(1-t,3));Invalidate();return Current!=Target;}
  protected override void OnPaint(PaintEventArgs e){using(var b=new SolidBrush(Track))e.Graphics.FillRectangle(b,ClientRectangle);using(var b=new SolidBrush(Fill))e.Graphics.FillRectangle(b,0,0,(int)Math.Round(Width*Current),Height);}
 }
 public sealed class DetailPopup:Form {
  readonly QuotaBar owner;readonly int dpi;readonly Color border;readonly Font normal,small,bold,big;
  readonly Timer motion=new Timer();readonly Stopwatch clock=Stopwatch.StartNew();readonly ToolTip tips=new ToolTip();
  readonly List<QuotaMeter> meters=new List<QuotaMeter>();readonly List<Label> names=new List<Label>(),numbers=new List<Label>(),resets=new List<Label>();
  Label value,scope,status,condition;Panel rows;SymbolButton refresh;Image appImage;bool updating,loading,opening;int? lastValue;long valueChanged,opened;Color valueTarget;
  Label resetInfo,notificationInfo;CheckBox resetAlerts;Button testNotice;bool settingAlerts;
  int S(int n){return (int)Math.Round(n*dpi/96.0);}
  public DetailPopup(QuotaBar owner,Snapshot data,int dpi,Color surface,Color border){
   this.owner=owner;this.dpi=dpi;this.border=border;FormBorderStyle=FormBorderStyle.None;ShowInTaskbar=Program.Inspect;TopMost=true;AutoScaleMode=AutoScaleMode.None;StartPosition=FormStartPosition.Manual;BackColor=surface;ForeColor=owner.Ink;KeyPreview=true;Text="Codex 额度详情"+(Program.Demo!=""?" · DEMO":"");AccessibleName=Text;DoubleBuffered=true;
   normal=new Font("Segoe UI",S(12),FontStyle.Regular,GraphicsUnit.Pixel);small=new Font("Segoe UI",S(11),FontStyle.Regular,GraphicsUnit.Pixel);bold=new Font("Segoe UI Semibold",S(13),FontStyle.Regular,GraphicsUnit.Pixel);big=new Font("Segoe UI Semibold",S(35),FontStyle.Regular,GraphicsUnit.Pixel);Font=normal;Width=S(310);
   using(var icon=StatusIcon.ApplicationIcon())appImage=icon.ToBitmap();var picture=new PictureBox{Image=appImage,SizeMode=PictureBoxSizeMode.Zoom};picture.SetBounds(S(16),S(16),S(18),S(18));Controls.Add(picture);
   LabelAt("Codex 额度",42,14,145,24,bold);if(Program.Demo!="")LabelAt("示例数据",202,18,60,18,small).ForeColor=owner.Sub;
   var close=new SymbolButton{AccessibleName="关闭详情",BackColor=surface,ForeColor=owner.Sub};close.SetBounds(S(268),S(10),S(28),S(28));close.Click+=delegate{Close();};Controls.Add(close);
   value=LabelAt("--",16,49,98,45,big);LabelAt("剩余 / 100",115,69,80,20,small).ForeColor=owner.Sub;
   condition=LabelAt("",214,69,80,20,small);condition.TextAlign=ContentAlignment.TopRight;
   scope=LabelAt("",16,102,278,22,small);scope.ForeColor=owner.Sub;scope.AutoEllipsis=true;
   rows=new Panel();rows.SetBounds(S(16),S(132),S(278),S(104));rows.AutoScroll=true;Controls.Add(rows);
   status=LabelAt("",16,246,236,44,small);status.ForeColor=owner.Sub;
   refresh=new SymbolButton{RefreshSymbol=true,AccessibleName="立即刷新",BackColor=surface,ForeColor=owner.Sub};refresh.SetBounds(S(266),S(246),S(28),S(28));refresh.Click+=delegate{owner.RefreshQuota();};tips.SetToolTip(refresh,"立即刷新；平时自动更新，无需手动操作");Controls.Add(refresh);
   resetInfo=LabelAt("",16,295,278,74,small);resetInfo.ForeColor=owner.Sub;
   resetAlerts=new CheckBox{Text="重置消息提醒",AccessibleName="重置消息提醒",Font=normal,ForeColor=owner.Ink,UseVisualStyleBackColor=false,BackColor=surface};resetAlerts.SetBounds(S(16),S(375),S(160),S(27));resetAlerts.CheckedChanged+=delegate{if(!settingAlerts)owner.SetResetAlerts(resetAlerts.Checked);};Controls.Add(resetAlerts);
   testNotice=new Button{Text="测试提醒",AccessibleName="发送测试提醒",Font=small,FlatStyle=FlatStyle.Flat,ForeColor=owner.Ink,BackColor=surface};testNotice.SetBounds(S(202),S(375),S(92),S(27));testNotice.Click+=delegate{owner.RequestTestNotification();};Controls.Add(testNotice);
   notificationInfo=LabelAt("",16,406,278,33,small);notificationInfo.ForeColor=owner.Sub;
   motion.Interval=30;motion.Tick+=Animate;
   Deactivate+=delegate{if(!updating&&!Program.Inspect)Close();};KeyDown+=delegate(object sender,KeyEventArgs e){if(e.KeyCode==Keys.Escape){e.Handled=true;Close();}else if(e.KeyCode==Keys.Apps||(e.KeyCode==Keys.F10&&e.Shift)){e.Handled=true;owner.ShowContextMenu(this);}};
   MouseUp+=delegate(object sender,MouseEventArgs e){if(e.Button==MouseButtons.Right)owner.ShowContextMenu(this);};
   foreach(Control control in Controls)control.MouseUp+=delegate(object sender,MouseEventArgs e){if(e.Button==MouseButtons.Right)owner.ShowContextMenu(this);};
   UpdateValue(data);owner.PlacePopup(this);
  }
  protected override void OnShown(EventArgs e){base.OnShown(e);if(Native.MotionEnabled){opened=clock.ElapsedMilliseconds;opening=true;Opacity=.75;motion.Start();}}
  Label LabelAt(string text,int x,int y,int w,int h,Font f){var l=new Label{Text=text,Font=f,UseMnemonic=false};l.SetBounds(S(x),S(y),S(w),S(h));Controls.Add(l);return l;}
  static string TimeText(long unix,string format){try{return DateTimeOffset.FromUnixTimeSeconds(unix).LocalDateTime.ToString(format);}catch{return "未知";}}
  internal static string SourceStatus(Snapshot data,bool enabled){
   if(data.preferences==null)return "读取服务待升级";
   if(!enabled)return "提醒关闭；停止源轮询，额度仍自动";
   var reset=data.reset;bool cloud=reset!=null&&(reset.sourceKind=="cloud_feed"||reset.sourceKind=="synthetic_cloud");
   if(cloud){
    string reason=reset.reason??"";
    if(reason=="cloud_endpoint_not_configured")return "云公告服务尚未接通；额度仍自动更新";
    if(reason.StartsWith("cloud_upstream_",StringComparison.Ordinal))return "云服务已响应；X 来源尚未确认在线";
    if(reset.sourceFresh)return "云公告源可用；作者声明不等于本机恢复";
    if(reason=="upstream_stale")return "云公告信息已过期；不判断本机恢复";
    if(reason=="upstream_freshness_unknown")return "X 来源时效尚未核验；暂不发送公告提醒";
    return "暂时无法读取云公告源；不判断本机恢复";
   }
   return reset!=null&&reset.sourceFresh?"第三方来源；公告不等于本机恢复":reset!=null&&reset.reason=="upstream_freshness_unknown"?"来源可读取，但消息时效尚未核验；暂不发送消息提醒":reset!=null&&reset.reason=="upstream_stale"?"来源信息已过期；不判断额度恢复":"暂时无法读取来源；不判断额度恢复";
  }
  void BuildRows(int count){
   if(meters.Count==count)return;
   foreach(Control c in rows.Controls.Cast<Control>().ToArray()){rows.Controls.Remove(c);c.Dispose();}meters.Clear();names.Clear();numbers.Clear();resets.Clear();
   for(int i=0;i<count;i++){
    var row=new Panel();row.SetBounds(0,S(i*54),S(274),S(54));rows.Controls.Add(row);
    var name=new Label{Font=normal,UseMnemonic=false,AutoEllipsis=true};name.SetBounds(0,0,S(216),S(18));row.Controls.Add(name);names.Add(name);
    var number=new Label{Font=normal,TextAlign=ContentAlignment.TopRight};number.SetBounds(S(217),0,S(57),S(18));row.Controls.Add(number);numbers.Add(number);
    var meter=new QuotaMeter{Track=border};meter.SetBounds(0,S(22),S(274),S(4));row.Controls.Add(meter);meters.Add(meter);
    var reset=new Label{Font=small,ForeColor=owner.Sub,UseMnemonic=false};reset.SetBounds(0,S(32),S(274),S(20));row.Controls.Add(reset);resets.Add(reset);
    foreach(Control c in row.Controls)c.MouseUp+=delegate(object sender,MouseEventArgs e){if(e.Button==MouseButtons.Right)owner.ShowContextMenu(this);};
   }
  }
  public void UpdateValue(Snapshot data){
   updating=true;SuspendLayout();long now=clock.ElapsedMilliseconds;bool animate=Visible&&Native.MotionEnabled;
   value.Text=data.remaining.HasValue?data.remaining.Value.ToString():"--";valueTarget=owner.Tone(data.color);
   if(animate&&data.remaining.HasValue&&lastValue.HasValue&&data.remaining!=lastValue)valueChanged=now+1;else valueChanged=0;
   value.ForeColor=valueChanged>0?owner.Sub:valueTarget;if(data.remaining.HasValue)lastValue=data.remaining;
   condition.Text=data.stale?"旧值":data.partial?"部分窗口":!data.remaining.HasValue?(data.state=="loading"?"检查中":"暂不可用"):data.remaining>=60?"充足":data.remaining>=20?"留意余量":"额度较低";condition.ForeColor=valueTarget;
   var windowData=(data.windows??new QuotaWindow[0]).Where(w=>w!=null).ToArray();int count=windowData.Length;scope.Text=(data.bucketName??"Codex")+(count>1?" · 显示较低余量":string.IsNullOrEmpty(data.scope)?"":" · "+data.scope);tips.SetToolTip(scope,scope.Text);
   BuildRows(count);
   for(int i=0;i<count;i++){
    var w=windowData[i];bool available=data.state=="ready"&&!data.stale&&w.remaining.HasValue&&w.remaining.Value>=0&&w.remaining.Value<=100;int rowValue=w.remaining.GetValueOrDefault();names[i].Text=w.name;numbers[i].Text=available?rowValue.ToString():"--";Color tone=available?owner.Tone(rowValue>=60?"green":rowValue>=20?"yellow":"red"):owner.Sub;numbers[i].ForeColor=tone;
    meters[i].Set(available?rowValue:0,tone,animate&&available,now);meters[i].AccessibleName=w.name+"剩余 "+(available?rowValue+" / 100":"暂不可用");
    resets[i].Text=w.resetsAt.HasValue?"重置："+TimeText(w.resetsAt.Value,"MM-dd HH:mm")+"（本地时间）":"重置时间未提供";
   }
   rows.Height=S(Math.Min(count,3)*54);int footer=132+Math.Min(count,3)*54+12;status.Top=S(footer);refresh.Top=S(footer-2);
   loading=data.state=="loading";refresh.Busy=loading;refresh.Enabled=!loading;
   string message=loading?"正在自动检查额度…":data.state=="ready"&&!data.partial?"已更新 · 每 "+(data.intervalSeconds/60)+" 分钟自动刷新":data.message;
   if(!string.IsNullOrEmpty(data.configWarning))message+="\n"+data.configWarning;
   status.Text=message;status.ForeColor=data.state=="error"?owner.Tone("red"):owner.Sub;
   status.Height=Math.Max(S(30),TextRenderer.MeasureText(message,small,new Size(status.Width,0),TextFormatFlags.WordBreak|TextFormatFlags.NoPrefix).Height+S(4));
   string timing=(data.lastSuccess>0?"上次成功："+TimeText(data.lastSuccess,"MM-dd HH:mm:ss"):"尚无成功更新")+(data.nextRefresh>0?"\n下次检查："+TimeText(data.nextRefresh,"HH:mm:ss"):"");tips.SetToolTip(status,timing);tips.SetToolTip(value,"剩余额度，以 100 为满额");
   int reminderTop=S(footer)+status.Height+S(10);
   bool capable=data.preferences!=null;
   string source=data.reset==null?"来源状态未提供":string.IsNullOrEmpty(data.reset.sourceLabel)?"来源未提供":data.reset.sourceLabel;
   string sourceState=SourceStatus(data,owner.AlertsEnabled(data));
   string recovery=data.reset!=null&&data.reset.localRecovery!=null?"本机额度已回升；原因尚未确认":"尚未观察到本机回升";
   string usage=data.usage!=null&&data.usage.state=="available"?"用量接口可用，暂不用于排名":"用量接口暂不可用，暂不用于排名";
   resetInfo.Text=source+"\n"+sourceState+"\n"+recovery+"\n"+usage;resetInfo.Top=reminderTop;resetInfo.Height=Math.Max(S(72),TextRenderer.MeasureText(resetInfo.Text,small,new Size(resetInfo.Width,0),TextFormatFlags.WordBreak|TextFormatFlags.NoPrefix).Height+S(4));tips.SetToolTip(resetInfo,resetInfo.Text+(data.reset!=null&&data.reset.@event!=null?"\n来源原文："+data.reset.@event.summary:""));
   resetAlerts.Top=resetInfo.Bottom+S(3);testNotice.Top=resetAlerts.Top;settingAlerts=true;resetAlerts.Enabled=capable;resetAlerts.Checked=capable&&owner.AlertsEnabled(data);settingAlerts=false;testNotice.Enabled=capable&&owner.AlertsEnabled(data);
   notificationInfo.Top=resetAlerts.Bottom+S(5);notificationInfo.Text=owner.NotificationSubmission;
   Height=notificationInfo.Bottom+S(12);ResumeLayout();owner.PlacePopup(this);Invalidate();updating=false;
   if(animate&&(loading||valueChanged>0||meters.Any(m=>m.Current!=m.Target)))motion.Start();else if(!opening)motion.Stop();
  }
  void Animate(object sender,EventArgs e){
   long now=clock.ElapsedMilliseconds;bool more=false;
   if(!Native.MotionEnabled){opening=false;Opacity=1;valueChanged=0;value.ForeColor=valueTarget;foreach(var m in meters){m.Current=m.Target;m.Invalidate();}refresh.Angle=0;refresh.Invalidate();motion.Stop();return;}
   if(opening){double t=Math.Min(1,(now-opened)/160.0);Opacity=.75+.25*t;opening=t<1;more|=opening;}
   if(valueChanged>0){double t=Math.Min(1,Math.Max(0,(now-valueChanged)/250.0));Color from=owner.Sub;value.ForeColor=Color.FromArgb((int)(from.R+(valueTarget.R-from.R)*t),(int)(from.G+(valueTarget.G-from.G)*t),(int)(from.B+(valueTarget.B-from.B)*t));if(t>=1)valueChanged=0;else more=true;}
   foreach(var meter in meters)more|=meter.Tick(now);
   if(loading){refresh.Angle=now%800*360f/800;refresh.Invalidate();more=true;}
   if(!more)motion.Stop();
  }
  protected override void OnPaint(PaintEventArgs e){base.OnPaint(e);using(var p=new Pen(border))e.Graphics.DrawRectangle(p,0,0,Width-1,Height-1);}
  protected override void Dispose(bool disposing){if(disposing){motion.Stop();motion.Dispose();tips.Dispose();if(appImage!=null)appImage.Dispose();normal.Dispose();small.Dispose();bold.Dispose();big.Dispose();}base.Dispose(disposing);}
 }
}
