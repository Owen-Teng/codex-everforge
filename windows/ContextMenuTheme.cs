using System;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.IO;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Web.Script.Serialization;
using System.Windows.Forms;

namespace CodexQuota {
 // Paint/layout only: the native ToolStrip keeps selection, actions, submenus and auto-close.
 internal static class ContextMenuTheme {
  internal sealed class Palette {
   internal readonly string Id;internal readonly Color Paper,Ink,Muted,Line,Accent,Selected,Disabled;
   internal Palette(string id,string paper,string ink,string muted,string line,string accent,string selected,string disabled){Id=id;Paper=Hex(paper);Ink=Hex(ink);Muted=Hex(muted);Line=Hex(line);Accent=Hex(accent);Selected=Hex(selected);Disabled=Hex(disabled);}
   static Color Hex(string s){return ColorTranslator.FromHtml(s);}
  }
  internal static Palette Colors(string id){
   if(id=="a")return new Palette("a","#f8f8f3","#203f35","#586b60","#cfdbcf","#825f23","#e6eee5","#7b877c");
   if(id=="c")return new Palette("c","#edf2f4","#203942","#4c6570","#ccd8de","#825f23","#dce8ed","#768a92");
   return new Palette("b","#14222e","#f6efdf","#bdcbd2","#435664","#e4c27c","#263d4a","#7f949f");
  }
  internal static string PreferencePath {get{return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"MakerDesktop","ui-theme.json");}}
  internal static string ReadPreference(){return ReadPreference(PreferencePath);}
  internal static string ReadPreference(string path){
   try{var info=new FileInfo(path);if(!info.Exists||info.Length>256||(info.Attributes&FileAttributes.ReparsePoint)!=0)return "b";
    byte[] bytes=new byte[257];int used=0,n;using(var stream=new FileStream(path,FileMode.Open,FileAccess.Read,FileShare.ReadWrite|FileShare.Delete)){while(used<bytes.Length&&(n=stream.Read(bytes,used,bytes.Length-used))>0)used+=n;}if(used>256)return "b";
    var raw=new System.Text.UTF8Encoding(false,true).GetString(bytes,0,used).TrimStart('\ufeff');var data=new JavaScriptSerializer{MaxJsonLength=256,RecursionLimit=8}.DeserializeObject(raw) as System.Collections.Generic.Dictionary<string,object>;
    object schema,id;if(data!=null&&data.TryGetValue("schema",out schema)&&schema is int&&(int)schema==1&&data.TryGetValue("theme",out id)&&id is string&&new[]{"a","b","c"}.ContainsValue((string)id))return (string)id;
   }catch(IOException){}catch(UnauthorizedAccessException){}catch(ArgumentException){}catch(InvalidOperationException){}
   return "b";
  }
  static bool ContainsValue(this string[] values,string value){foreach(string v in values)if(v==value)return true;return false;}
  [DllImport("user32.dll")] static extern IntPtr WindowFromPoint(Point point);
  [DllImport("user32.dll")] static extern uint GetDpiForWindow(IntPtr window);
  internal static int DpiAtCursor(IntPtr anchor){try{uint dpi=GetDpiForWindow(anchor!=IntPtr.Zero?anchor:WindowFromPoint(Cursor.Position));return dpi>=96&&dpi<=768?(int)dpi:96;}catch(EntryPointNotFoundException){return 96;}}
  sealed class Resources {internal Font Font;internal string Theme;internal int Dpi;internal bool HighContrast;}
  static readonly ConditionalWeakTable<ContextMenuStrip,Resources> owned=new ConditionalWeakTable<ContextMenuStrip,Resources>();
  static readonly string preferredFont=ChooseFont();
  static string ChooseFont(){using(var fonts=new System.Drawing.Text.InstalledFontCollection()){var available=new System.Collections.Generic.HashSet<string>(StringComparer.OrdinalIgnoreCase);foreach(var family in fonts.Families){available.Add(family.Name);family.Dispose();}foreach(string name in new[]{"Microsoft YaHei UI","Microsoft YaHei","微软雅黑","Segoe UI"})if(available.Contains(name))return name;}return SystemFonts.MenuFont.FontFamily.Name;}
  internal static void Apply(ContextMenuStrip menu,string id,int dpi){Apply(menu,id,dpi,SystemInformation.HighContrast);}
  internal static void Apply(ContextMenuStrip menu,string id,int dpi,bool highContrast){
   if(menu==null||menu.IsDisposed)return;dpi=Math.Max(96,Math.Min(768,dpi));var palette=Colors(id);Resources resources;
   if(!owned.TryGetValue(menu,out resources)){resources=new Resources();owned.Add(menu,resources);var retained=resources;menu.Disposed+=delegate{if(retained.Font!=null){retained.Font.Dispose();retained.Font=null;}};}
   if(resources.Font!=null&&resources.Theme==palette.Id&&resources.Dpi==dpi&&resources.HighContrast==highContrast)return;
   var previous=resources.Font;float logical=Math.Max(14f,SystemFonts.MenuFont.SizeInPoints*96f/72f);
   var font=new Font(highContrast?SystemFonts.MenuFont.FontFamily.Name:preferredFont,logical*dpi/96f,FontStyle.Regular,GraphicsUnit.Pixel,highContrast?SystemFonts.MenuFont.GdiCharSet:(byte)134);
   ToolStripRenderer renderer=highContrast?(ToolStripRenderer)new ToolStripSystemRenderer():new Renderer(palette,dpi);
   menu.SuspendLayout();ApplyStrip(menu,palette,dpi,font,renderer,highContrast);menu.ResumeLayout(true);
   resources.Font=font;resources.Theme=palette.Id;resources.Dpi=dpi;resources.HighContrast=highContrast;if(previous!=null)previous.Dispose();
  }
  static int S(int value,int dpi){return (int)Math.Round(value*dpi/96.0);}
  static void ApplyStrip(ToolStripDropDown strip,Palette p,int dpi,Font font,ToolStripRenderer renderer,bool hc){
   strip.Font=font;strip.Renderer=renderer;strip.BackColor=hc?SystemColors.Menu:p.Paper;strip.ForeColor=hc?SystemColors.MenuText:p.Ink;
   var drop=strip as ToolStripDropDownMenu;if(drop!=null){drop.ShowImageMargin=false;drop.ShowCheckMargin=true;}
   strip.Padding=new Padding(S(6,dpi));strip.ImageScalingSize=new Size(S(16,dpi),S(16,dpi));
   int index=0;foreach(ToolStripItem item in strip.Items){item.Font=font;item.BackColor=strip.BackColor;item.ForeColor=strip.ForeColor;
    if(item is ToolStripSeparator)item.Margin=new Padding(0,S(4,dpi),0,S(4,dpi));else item.Padding=new Padding(S(4,dpi),S(index==0?9:5,dpi),S(10,dpi),S(index==strip.Items.Count-1?9:5,dpi));index++;
    var child=item as ToolStripMenuItem;if(child!=null&&child.HasDropDownItems)ApplyStrip(child.DropDown,p,dpi,font,renderer,hc);
   }
  }
  internal sealed class Renderer:ToolStripProfessionalRenderer {
   readonly Palette p;readonly int dpi;
   internal Renderer(Palette palette,int dpi):base(){p=palette;this.dpi=dpi;RoundedEdges=false;}
   protected override void OnRenderToolStripBackground(ToolStripRenderEventArgs e){using(var b=new SolidBrush(p.Paper))e.Graphics.FillRectangle(b,e.AffectedBounds);}
   protected override void OnRenderImageMargin(ToolStripRenderEventArgs e){using(var b=new SolidBrush(p.Paper))e.Graphics.FillRectangle(b,e.AffectedBounds);}
   protected override void OnRenderToolStripBorder(ToolStripRenderEventArgs e){using(var pen=new Pen(p.Line))e.Graphics.DrawRectangle(pen,0,0,e.ToolStrip.Width-1,e.ToolStrip.Height-1);}
   protected override void OnRenderMenuItemBackground(ToolStripItemRenderEventArgs e){
    if(!e.Item.Enabled||!e.Item.Selected&&!e.Item.Pressed)return;
    var r=new Rectangle(S(2,dpi),0,e.Item.Width-S(4,dpi)-1,e.Item.Height-1);if(r.Width<1||r.Height<1)return;
    using(var path=Round(r,S(4,dpi)))using(var b=new SolidBrush(p.Selected))using(var accent=new SolidBrush(p.Accent)){e.Graphics.FillPath(b,path);e.Graphics.FillRectangle(accent,r.Left+S(1,dpi),r.Top+S(5,dpi),S(2,dpi),Math.Max(1,r.Height-S(10,dpi)));}
   }
   protected override void OnRenderItemText(ToolStripItemTextRenderEventArgs e){
    // Keep native text rectangles/shortcut alignment; explicitly draw the selected CJK family.
    var old=e.Graphics.TextRenderingHint;e.Graphics.TextRenderingHint=System.Drawing.Text.TextRenderingHint.ClearTypeGridFit;
    using(var format=new StringFormat(StringFormat.GenericTypographic))using(var brush=new SolidBrush(e.Item.Enabled?p.Ink:p.Disabled)){
     format.FormatFlags=StringFormatFlags.NoWrap;format.LineAlignment=StringAlignment.Center;format.Alignment=(e.TextFormat&TextFormatFlags.Right)!=0?StringAlignment.Far:StringAlignment.Near;format.Trimming=StringTrimming.EllipsisCharacter;
     format.HotkeyPrefix=(e.TextFormat&TextFormatFlags.NoPrefix)!=0?System.Drawing.Text.HotkeyPrefix.None:(e.TextFormat&TextFormatFlags.HidePrefix)!=0?System.Drawing.Text.HotkeyPrefix.Hide:System.Drawing.Text.HotkeyPrefix.Show;
     if((e.TextFormat&TextFormatFlags.RightToLeft)!=0)format.FormatFlags|=StringFormatFlags.DirectionRightToLeft;
     var bounds=new RectangleF(e.TextRectangle.X,S(2,dpi),e.TextRectangle.Width,Math.Max(1,e.Item.Height-S(4,dpi)));e.Graphics.DrawString(e.Text,e.Item.Font,brush,bounds,format);
    }e.Graphics.TextRenderingHint=old;
   }
   protected override void OnRenderSeparator(ToolStripSeparatorRenderEventArgs e){int y=e.Item.Height/2;using(var pen=new Pen(p.Line))e.Graphics.DrawLine(pen,S(10,dpi),y,e.Item.Width-S(10,dpi),y);}
   protected override void OnRenderArrow(ToolStripArrowRenderEventArgs e){e.ArrowColor=e.Item.Enabled?p.Accent:p.Disabled;base.OnRenderArrow(e);}
   protected override void OnRenderItemCheck(ToolStripItemImageRenderEventArgs e){
    var r=e.ImageRectangle;float cx=r.Left+r.Width/2f,cy=r.Top+r.Height/2f,s=dpi/96f;
    var old=e.Graphics.SmoothingMode;e.Graphics.SmoothingMode=SmoothingMode.AntiAlias;
    using(var pen=new Pen(e.Item.Enabled?p.Accent:p.Disabled,Math.Max(1.5f,1.8f*s))){pen.StartCap=pen.EndCap=LineCap.Round;e.Graphics.DrawLines(pen,new[]{new PointF(cx-4*s,cy),new PointF(cx-1*s,cy+3*s),new PointF(cx+5*s,cy-4*s)});}
    e.Graphics.SmoothingMode=old;
   }
   static GraphicsPath Round(Rectangle r,int radius){var path=new GraphicsPath();int d=radius*2;path.AddArc(r.Left,r.Top,d,d,180,90);path.AddArc(r.Right-d,r.Top,d,d,270,90);path.AddArc(r.Right-d,r.Bottom-d,d,d,0,90);path.AddArc(r.Left,r.Bottom-d,d,d,90,90);path.CloseFigure();return path;}
  }
 }
}
