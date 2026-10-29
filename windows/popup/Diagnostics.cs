using System;using System.IO;using System.Text;using System.Threading;namespace MakerPopup {
 internal sealed class HostStateWriter:IDisposable {
  readonly object gate=new object();readonly string file;readonly System.Threading.Timer timer;string pending,lastWritten;bool scheduled,writing,disposed;int failures;
  internal int Failures{get{return Interlocked.CompareExchange(ref failures,0,0);}}
  internal HostStateWriter(string file){this.file=file;timer=new System.Threading.Timer(Flush,null,Timeout.Infinite,Timeout.Infinite);}
  internal void Queue(string snapshot){if(snapshot==null||snapshot.Length>32768)return;lock(gate){if(disposed||snapshot==pending||(!writing&&pending==null&&snapshot==lastWritten))return;pending=snapshot;if(!scheduled&&!writing){scheduled=true;timer.Change(1000,Timeout.Infinite);}}}
  void Flush(object unused){string snapshot;lock(gate){scheduled=false;if(disposed||pending==null)return;snapshot=pending;pending=null;writing=true;}bool success=false;string temporary=file+".tmp";try{Directory.CreateDirectory(Path.GetDirectoryName(file));File.WriteAllText(temporary,snapshot,new UTF8Encoding(false));if(File.Exists(file))File.Replace(temporary,file,null);else File.Move(temporary,file);success=true;}catch{Interlocked.Increment(ref failures);}finally{lock(gate){writing=false;if(success)lastWritten=snapshot;if(!disposed&&pending!=null&&!scheduled){scheduled=true;timer.Change(1000,Timeout.Infinite);}}}}
  public void Dispose(){lock(gate){disposed=true;pending=null;timer.Dispose();}}
 }
}
