using System;
using System.Drawing;
namespace MakerPopup {
 internal enum SurfaceMode { Popup,Card }
 internal sealed class PopoverSession {
  internal bool Visible,WaitingForTray;internal int Epoch;internal SurfaceMode Mode=SurfaceMode.Popup;bool pendingPageReset;
  internal void Show(bool reopen){Visible=true;WaitingForTray=false;Mode=SurfaceMode.Popup;pendingPageReset|=reopen;Epoch++;}
  internal void Hide(){Visible=false;WaitingForTray=false;Mode=SurfaceMode.Popup;pendingPageReset=true;Epoch++;}
  internal void RequestPageReset(){pendingPageReset=true;}
  internal bool TakePageReset(){if(!pendingPageReset)return false;pendingPageReset=false;return true;}
  internal bool Surface(string mode){if(!Visible)return false;if(mode=="card"){Mode=SurfaceMode.Card;return true;}if(mode=="popup"){Mode=SurfaceMode.Popup;return true;}return false;}
  internal bool Current(int epoch){return Visible&&Epoch==epoch;}
  internal static bool CanRequestForeground(long expected,long current,long own){return current==own||current==expected;}
 }
 internal static class PopupGeometry {
  internal const int LogicalWidth=470,LogicalHeight=650,LogicalRadius=19;
  internal static Rectangle Place(Rectangle work,Rectangle anchor,int dpi,SurfaceMode mode){
   dpi=Math.Max(96,dpi);int inset=Math.Max(1,(int)Math.Round(12*dpi/96.0)),gap=(int)Math.Round(12*dpi/96.0);
   int width=Math.Max(1,Math.Min((int)Math.Round(LogicalWidth*dpi/96.0),work.Width-Math.Min(inset*2,Math.Max(0,work.Width-1))));
   int height=Math.Max(1,Math.Min((int)Math.Round(LogicalHeight*dpi/96.0),work.Height-Math.Min(inset*2,Math.Max(0,work.Height-1))));
   int x,y;
   if(mode==SurfaceMode.Card){x=work.Left+(work.Width-width)/2;y=work.Top+(work.Height-height)/2;}
   else{x=work.Right-width-gap;y=work.Bottom-height-gap;}
   int padX=Math.Min(inset,Math.Max(0,(work.Width-width)/2)),padY=Math.Min(inset,Math.Max(0,(work.Height-height)/2));
   x=Math.Max(work.Left+padX,Math.Min(x,work.Right-padX-width));y=Math.Max(work.Top+padY,Math.Min(y,work.Bottom-padY-height));
   return new Rectangle(x,y,width,height);
  }
  internal static Region Clip(Size size,int dpi){
   if(size.Width<2||size.Height<2)return new Region(new Rectangle(Point.Empty,size));
   using(var path=new System.Drawing.Drawing2D.GraphicsPath()){
    int diameter=Math.Min((int)Math.Round(38*Math.Max(96,dpi)/96.0),Math.Min(size.Width,size.Height));
    path.AddArc(0,0,diameter,diameter,180,90);path.AddArc(size.Width-diameter,0,diameter,diameter,270,90);path.AddArc(size.Width-diameter,size.Height-diameter,diameter,diameter,0,90);path.AddArc(0,size.Height-diameter,diameter,diameter,90,90);path.CloseFigure();return new Region(path);
   }
  }
 }
 internal static class PopupScripts {
  internal const string Guard=@"(()=>{const inner=new WeakSet();let composing=false;window.addEventListener('compositionstart',()=>{composing=true;},true);window.addEventListener('compositionend',()=>{composing=false;},true);window.__makerCompositionActive=()=>composing;window.addEventListener('keydown',e=>{if(e.key!=='Escape')return;if(e.isComposing||composing||e.keyCode===229){e.stopImmediatePropagation();return;}if(e.repeat){e.preventDefault();e.stopImmediatePropagation();return;}if(document.querySelector('dialog[open]'))inner.add(e);},true);window.addEventListener('keydown',e=>{if(e.key==='Escape'&&!e.__makerNativeEscape&&!e.defaultPrevented&&!e.isComposing&&!inner.has(e)&&!document.querySelector('dialog[open]'))window.chrome.webview.postMessage({action:'close'});});})();";
  internal const string Escape=@"(()=>{if(window.__makerCompositionActive?.())return 'composition';const initial=document.querySelectorAll('dialog[open]');const before=initial[initial.length-1];const event=new KeyboardEvent('keydown',{key:'Escape',code:'Escape',bubbles:true,cancelable:true});Object.defineProperty(event,'__makerNativeEscape',{value:true});(document.activeElement||document).dispatchEvent(event);if(event.defaultPrevented)return 'page';const dialogs=document.querySelectorAll('dialog[open]');const dialog=before||dialogs[dialogs.length-1];if(dialog){if(dialog.open&&dialog.dispatchEvent(new Event('cancel',{cancelable:true})))dialog.close();return 'dialog';}return 'outer';})()";
 }
}
