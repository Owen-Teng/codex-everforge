using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using System.Web.Script.Serialization;
using System.Windows.Forms;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;

namespace MakerPopup {
 public sealed class ReplayEnvelope {public int schemaVersion;public string mode,source,verification;public bool? real;public ReplayNotice notification;}
 public sealed class ReplayNotice {public string id,kind,title,body;public long createdAt;public string mode,source,verification;public bool real;}
 internal sealed class ReplayReader {
  readonly string file;readonly bool allowLive;readonly JavaScriptSerializer json=new JavaScriptSerializer{MaxJsonLength=8192,RecursionLimit=16};
  readonly HashSet<string> seen=new HashSet<string>();string previous="";
  internal int Rejected;internal string Status="not_configured";
  internal ReplayReader(string file,bool allowLive=false){this.file=file;this.allowLive=allowLive;if(file!=""){Status="waiting_file";Read(DateTimeOffset.UtcNow.ToUnixTimeSeconds());}}
  internal static bool OriginValid(ReplayEnvelope value,bool allowLive){return value!=null&&value.schemaVersion==1&&((value.mode=="synthetic_x_replay"&&!value.real.HasValue&&string.IsNullOrEmpty(value.source)&&string.IsNullOrEmpty(value.verification))||(allowLive&&value.mode=="x_activity_candidate"&&value.real==true&&value.source=="x_activity_stream"&&value.verification==(value.notification==null?"unverified":"author_claim")));}
  internal static bool Valid(ReplayEnvelope value,long now,bool allowLive=false){if(!OriginValid(value,allowLive)||value.notification==null)return false;var note=value.notification;bool live=value.mode=="x_activity_candidate";return !string.IsNullOrWhiteSpace(note.id)&&note.id.Length<=160&&!string.IsNullOrWhiteSpace(note.title)&&note.title.Length<=120&&(live||note.title.Contains("联调"))&&note.body!=null&&note.body.Length<=1200&&(live?new[]{"notice","announced"}:new[]{"notice","announced","recovered","test","correction"}).Contains(note.kind)&&note.createdAt>0&&note.createdAt<=now+300&&now-note.createdAt<=600;}
  internal static bool SafeFile(string file){if(!Path.IsPathRooted(file)||file.StartsWith(@"\\")||!string.Equals(Path.GetExtension(file),".jsonl",StringComparison.OrdinalIgnoreCase))return false;string path=Path.GetFullPath(file);while(!string.IsNullOrEmpty(path)){if((File.Exists(path)||Directory.Exists(path))&&(File.GetAttributes(path)&FileAttributes.ReparsePoint)!=0)return false;path=Path.GetDirectoryName(path);}return true;}
  internal List<ReplayNotice> Read(long now){var result=new List<ReplayNotice>();if(file=="")return result;try{
   if(!SafeFile(file)){Status="invalid_path";return result;}if(!File.Exists(file)){Status="waiting_file";return result;}var info=new FileInfo(file);if(info.Length>262144){Status="file_too_large";return result;}
   string stamp=info.Length+":"+info.LastWriteTimeUtc.Ticks;if(stamp==previous)return result;previous=stamp;
   string content;using(var stream=new FileStream(file,FileMode.Open,FileAccess.Read,FileShare.ReadWrite|FileShare.Delete)){var buffer=new byte[262145];int used=0,n;while(used<buffer.Length&&(n=stream.Read(buffer,used,buffer.Length-used))>0)used+=n;if(used>262144){Status="file_too_large";return result;}content=new UTF8Encoding(false,true).GetString(buffer,0,used).TrimStart('\ufeff');}
   var lines=content.Split('\n');if(lines.Length>1024){Status="too_many_lines";return result;}Status=allowLive?"live_enabled_file_consumer":"synthetic_x_replay";
   foreach(string raw in lines){string line=raw.Trim();if(line=="")continue;if(line.Length>8192){Rejected++;continue;}ReplayEnvelope envelope;try{var shape=json.Deserialize<Dictionary<string,object>>(line);if(shape==null||!shape.ContainsKey("schemaVersion")||!(shape["schemaVersion"] is int)||(int)shape["schemaVersion"]!=1||!shape.ContainsKey("mode")||!(shape["mode"] is string)||!shape.ContainsKey("notification")){Rejected++;continue;}
    string mode=(string)shape["mode"];if(mode=="x_activity_candidate"){if(!allowLive||!shape.ContainsKey("real")||!(shape["real"] is bool)||!(bool)shape["real"]||!shape.ContainsKey("source")||!(shape["source"] is string)||(string)shape["source"]!="x_activity_stream"||!shape.ContainsKey("verification")||!(shape["verification"] is string)){Rejected++;continue;}}else if(mode!="synthetic_x_replay"||new[]{"real","source","verification"}.Any(shape.ContainsKey)){Rejected++;continue;}
    if(shape["notification"]!=null){var notification=shape["notification"] as Dictionary<string,object>;if(notification==null||new[]{"id","kind","title","body"}.Any(key=>!notification.ContainsKey(key)||!(notification[key] is string))||!notification.ContainsKey("createdAt")||!(notification["createdAt"] is int||notification["createdAt"] is long)){Rejected++;continue;}}envelope=json.Deserialize<ReplayEnvelope>(line);}catch{Rejected++;continue;}
    if(OriginValid(envelope,allowLive)&&envelope.notification==null)continue;
    if(!Valid(envelope,now,allowLive)){Rejected++;continue;}var note=envelope.notification;if(seen.Count>=512&&!seen.Contains(note.id)){Status="session_limit";continue;}if(seen.Add(note.id)){note.mode=envelope.mode;note.real=envelope.mode=="x_activity_candidate";note.source=note.real?envelope.source:"synthetic_x_replay";note.verification=note.real?envelope.verification:"synthetic";result.Add(note);}
   }
  }catch{Status="read_failed";}return result;}
 }
 internal static class Program {
  internal const string Title="Codex Everforge";
  static IntPtr ExistingWindow(){var wait=Stopwatch.StartNew();do{IntPtr found=Native.FindWindow(null,Title);if(found==IntPtr.Zero)found=Native.FindWindow(null,"造物者牌桌");if(found!=IntPtr.Zero&&Native.WindowRunsExecutable(found,Application.ExecutablePath))return found;Thread.Sleep(25);}while(wait.ElapsedMilliseconds<1500);return IntPtr.Zero;}
  [STAThread]static void Main(string[] args){try{Native.SetProcessDpiAwarenessContext(new IntPtr(-4));}catch{}
   var ticket=PopupActivation.Ticket.Read(args);if(!ticket.Valid){Environment.ExitCode=6;return;}
   string file="";bool allowLive=args.Contains("--allow-live");for(int i=0;i<args.Length;i++)if(args[i]=="--notice-file"&&i+1<args.Length)file=args[++i];
   if(file!=""&&!ReplayReader.SafeFile(file)){Environment.ExitCode=7;return;}
   bool first;using(var mutex=new Mutex(true,@"Local\MakerPopup.FE006.v1",out first)){
    if(!first){IntPtr existing=ExistingWindow();if(existing==IntPtr.Zero){Environment.ExitCode=3;return;}int command=args.Contains("--close")?2:args.Contains("--reload")?1:args.Contains("--show")?0:3;bool granted=false;if(command!=2){uint pid;Native.GetWindowThreadProcessId(existing,out pid);granted=Native.AllowSetForegroundWindow(pid);}int flags=command|(granted?0x100:0)|(ticket.Ready?0x200:0)|(ticket.ParentGrantAccepted?0x400:0);if(!Native.PostMessage(existing,Native.Wake,new IntPtr(flags),new IntPtr(ticket.Origin)))Environment.ExitCode=4;return;}
    if(args.Contains("--close")||args.Contains("--reload")){mutex.ReleaseMutex();return;}
    Application.EnableVisualStyles();Application.SetCompatibleTextRenderingDefault(false);
    try{Application.Run(new PopupForm(file,allowLive,ticket));}catch(Exception error){MessageBox.Show("Codex Everforge 窗口未能启动。需要现有 WebView2 Runtime 与完整候选目录。\n"+error.GetType().Name,Title,MessageBoxButtons.OK,MessageBoxIcon.Information);}finally{mutex.ReleaseMutex();}
   }
  }
 }
 internal sealed class ReplayToast:Form {
  readonly System.Windows.Forms.Timer timer=new System.Windows.Forms.Timer();
  protected override bool ShowWithoutActivation{get{return true;}}
  protected override CreateParams CreateParams{get{var p=base.CreateParams;p.ExStyle|=0x08000000|0x80;return p;}}
  internal ReplayToast(ReplayNotice note,int dpi,Action open){Text=note.real?"Codex Everforge · 来源提示":"Codex Everforge · 联调提示";FormBorderStyle=FormBorderStyle.FixedToolWindow;ShowInTaskbar=false;TopMost=true;StartPosition=FormStartPosition.Manual;AutoScaleMode=AutoScaleMode.None;double scale=dpi/96.0;Func<int,int> s=value=>(int)Math.Round(value*scale);BackColor=Color.FromArgb(245,244,239);ClientSize=new Size(s(350),s(140));Font=new Font("Segoe UI",s(13),FontStyle.Regular,GraphicsUnit.Pixel);var title=new Label{Text=note.title,AutoEllipsis=true,UseMnemonic=false,Font=new Font("Segoe UI",s(14),FontStyle.Bold,GraphicsUnit.Pixel),Bounds=new Rectangle(s(12),s(9),s(320),s(25))};var body=new Label{Text=note.body,AutoEllipsis=true,UseMnemonic=false,Bounds=new Rectangle(s(12),s(39),s(320),s(54))};var button=new Button{Text=note.real?"查看消息":"查看联调消息",Bounds=new Rectangle(s(231),s(102),s(108),s(28)),FlatStyle=FlatStyle.Flat};button.Click+=delegate{open();Close();};Controls.Add(title);Controls.Add(body);Controls.Add(button);var work=Screen.PrimaryScreen.WorkingArea;Bounds=PopupForm.Clamp(new Rectangle(work.Right-Width-14,work.Bottom-Height-14,Width,Height),work,12);timer.Interval=20000;timer.Tick+=delegate{Close();};timer.Start();}
  protected override void Dispose(bool disposing){if(disposing)timer.Dispose();base.Dispose(disposing);}
 }
}
