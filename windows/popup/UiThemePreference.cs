using System;
using System.Collections.Generic;
using System.IO;
using System.Text;
using System.Web.Script.Serialization;

namespace MakerPopup {
 // An enum-only, local projection. No account/profile/quota data or new scheduler.
 internal static class UiThemePreference {
  static readonly object gate=new object();
  internal static string FilePath {get{return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"MakerDesktop","ui-theme.json");}}
  internal static bool Valid(object value){return value is string&&((string)value=="a"||(string)value=="b"||(string)value=="c");}
  internal static bool Read(string path,out string theme){
   theme=null;
   try{var info=new FileInfo(path);if(!info.Exists||info.Length>256||(info.Attributes&FileAttributes.ReparsePoint)!=0)return false;
    byte[] bytes=new byte[257];int used=0,n;using(var stream=new FileStream(path,FileMode.Open,FileAccess.Read,FileShare.ReadWrite|FileShare.Delete)){while(used<bytes.Length&&(n=stream.Read(bytes,used,bytes.Length-used))>0)used+=n;}
    if(used>256)return false;string raw=new UTF8Encoding(false,true).GetString(bytes,0,used).TrimStart('\ufeff');
    var data=new JavaScriptSerializer{MaxJsonLength=256,RecursionLimit=8}.DeserializeObject(raw) as Dictionary<string,object>;object schema,value;
    if(data==null||!data.TryGetValue("schema",out schema)||!(schema is int)||(int)schema!=1||!data.TryGetValue("theme",out value)||!Valid(value))return false;theme=(string)value;return true;
   }catch(IOException){}catch(UnauthorizedAccessException){}catch(ArgumentException){}catch(InvalidOperationException){}
   return false;
  }
  internal static bool Update(object theme){return Write(FilePath,theme,Commit);}
  internal static bool Write(string path,object theme,Action<string,string,bool> commit){
   if(!Valid(theme))return false;
   lock(gate){string previous;bool exists=File.Exists(path);if(exists){if(!Read(path,out previous))return false;if(previous==(string)theme)return true;}
    string temporary=path+".tmp-"+Guid.NewGuid().ToString("N");
    try{Directory.CreateDirectory(Path.GetDirectoryName(path));byte[] payload=new UTF8Encoding(false).GetBytes("{\"schema\":1,\"theme\":\""+(string)theme+"\"}");
     using(var stream=new FileStream(temporary,FileMode.CreateNew,FileAccess.Write,FileShare.None)){stream.Write(payload,0,payload.Length);stream.Flush(true);}commit(temporary,path,exists);return true;
    }catch(IOException){}catch(UnauthorizedAccessException){}catch(ArgumentException){}catch(InvalidOperationException){}
    finally{try{if(File.Exists(temporary))File.Delete(temporary);}catch(IOException){}catch(UnauthorizedAccessException){}}
    return false;
   }
  }
  internal static void Commit(string temporary,string path,bool exists){if(exists)File.Replace(temporary,path,null);else File.Move(temporary,path);}
 }
}
