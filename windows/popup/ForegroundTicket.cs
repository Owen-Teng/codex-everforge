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
