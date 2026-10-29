using System;
using System.Diagnostics;
using System.Drawing;
using System.Threading.Tasks;
using System.Windows.Forms;

namespace CodexQuota {
 internal sealed class SetupForm:Form {
  readonly Button apply=new Button(),fallback=new Button();readonly Label state=new Label(),installation=new Label(),startup=new Label(),visibility=new Label();
  readonly bool demo;readonly int dpi;bool busy,complete;
  int S(int n){return (int)Math.Round(n*dpi/96.0);}
  void Place(Control c,int x,int y,int w,int h){c.SetBounds(S(x),S(y),S(w),S(h));}
  internal SetupForm(bool demo){
   this.demo=demo;dpi=Math.Max(96,Native.Dpi(Native.FindWindow("Shell_TrayWnd",null)));Text="Codex 额度设置"+(demo?" · 示例数据":"");StartPosition=FormStartPosition.CenterScreen;FormBorderStyle=FormBorderStyle.FixedDialog;MaximizeBox=false;MinimizeBox=true;AutoScaleMode=AutoScaleMode.None;ClientSize=new Size(S(450),S(360));BackColor=Color.FromArgb(249,250,247);ForeColor=Color.FromArgb(39,53,47);Font=new Font("Microsoft YaHei UI",S(14),FontStyle.Regular,GraphicsUnit.Pixel);Icon=StatusIcon.ApplicationIcon();
   AddLabel("让额度一直在右下角",24,22,402,32,22,FontStyle.Bold);
   AddLabel(demo?"交互预演：不会安装，也不会修改系统设置。":"一次配置，以后打开 Codex 就能看到。",24,65,402,26,14,FontStyle.Regular);
   Row(installation,"安装到固定位置",108);Row(startup,"随 Codex 启动 · 额度自动刷新",144);Row(visibility,"图标和数字合为一项 · 尝试右侧常显",180);
   Place(state,24,222,402,59);state.Font=new Font(Font.FontFamily,S(12),GraphicsUnit.Pixel);state.Text="默认每 2 分钟刷新，已设置的间隔会保留。";Controls.Add(state);
   apply.Text=SetupIntegration.IsInstalled?"一键配置":"一键安装并配置";Place(apply,24,294,230,42);apply.BackColor=Color.FromArgb(24,115,76);apply.ForeColor=Color.White;apply.FlatStyle=FlatStyle.Flat;apply.FlatAppearance.BorderSize=0;apply.Cursor=Cursors.Hand;Controls.Add(apply);AcceptButton=apply;apply.Click+=async delegate{if(complete)Close();else await Configure();};
   fallback.Text="打开显示设置";Place(fallback,266,294,160,42);fallback.FlatStyle=FlatStyle.Flat;fallback.FlatAppearance.BorderColor=Color.FromArgb(217,224,216);fallback.Visible=false;fallback.Click+=delegate{if(!demo)Process.Start(new ProcessStartInfo("ms-settings:taskbar"){UseShellExecute=true});else state.Text="预演：这里会打开 Windows 任务栏显示设置。";};Controls.Add(fallback);
   FormClosing+=delegate(object sender,FormClosingEventArgs e){if(busy){e.Cancel=true;state.Text="正在完成配置，请稍候…";}};
  }
  void AddLabel(string text,int x,int y,int w,int h,int size,FontStyle style){var label=new Label{Text=text,Font=new Font(Font.FontFamily,S(size),style,GraphicsUnit.Pixel)};Place(label,x,y,w,h);Controls.Add(label);}
  void Row(Label label,string text,int y){label.Text=text;Place(label,24,y,402,25);Controls.Add(label);}
  async Task Configure(){
   if(busy)return;busy=true;apply.Enabled=false;fallback.Visible=false;apply.Text="正在配置…";state.ForeColor=ForeColor;
   try{
    SetupResult result;
    if(demo){state.Text="预演：安装 → 开启联动启动 → 检查右侧位置";await Task.Delay(900);result=new SetupResult{Installed=true,Startup=true,Visible=true,QuotaReady=true};}
    else result=await Task.Run(()=>SetupIntegration.Apply(message=>{if(!IsDisposed)BeginInvoke(new Action(()=>state.Text=message));}));
    installation.Text=result.Installed?"已安装 · 固定位置":"安装尚未完成";startup.Text=result.Startup?"已开启 · 随 Codex 启动，额度自动刷新":"自动启动 · 本次尚未配置";visibility.Text=result.Visible?"已常显 · 一个图标，点击查看额度":result.Installed&&result.Startup?"右侧常显 · 需要检查":"右侧常显 · 本次尚未检查";
    if(result.Installed&&result.Startup&&result.Visible&&result.QuotaReady&&result.Error==""){state.Text=demo?"预演完成。正式版会检查任务栏的实际显示位置。":"配置完成，已读到额度。可以关闭此窗口。";state.ForeColor=Color.FromArgb(24,115,76);apply.Text="完成";complete=true;}
    else{state.Text=result.Error!=""?result.Error:!result.QuotaReady&&result.QuotaNote!=""?result.QuotaNote:result.VisibilityNote;state.ForeColor=Color.FromArgb(151,101,0);apply.Text="重新检查";fallback.Visible=result.Installed&&result.Startup&&!result.Visible;}
   }catch{state.Text="配置未完成，请重新尝试。";apply.Text="重新检查";}
   finally{busy=false;apply.Enabled=true;}
  }
 }
}
