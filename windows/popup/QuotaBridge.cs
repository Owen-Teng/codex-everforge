using System;
using System.Net.Http;
using System.Text;
using System.Threading.Tasks;
using System.Web.Script.Serialization;
namespace MakerPopup {
 public sealed class LocalQuotaWindow {public string name;public int? remaining;public long? durationMins,resetsAt;}
 public sealed class LocalQuota {public string state,bucket,scope;public int? remaining;public long updatedAt,lastSuccess,nextRefresh;public int intervalSeconds;public bool stale,partial,demo;public LocalQuotaWindow[] windows;}
 public sealed class LocalResetEvent {public string id,kind,summary,verification,sourceUrl;public long publishedAt;}
 public sealed class LocalRecovery {public int? beforeRemaining,afterRemaining;public long? observedAt;}
 public sealed class LocalReset {public string state,sourceKind,sourceLabel,reason;public bool sourceFresh;public long? lastCheckedAt,lastSuccessAt;public LocalResetEvent @event;public LocalRecovery localRecovery;}
 public sealed class LocalPreferences {public bool resetAlertsEnabled;}
 public sealed class LocalQuotaState {public int schemaVersion;public string mode;public LocalQuota quota;public LocalReset reset;public LocalPreferences preferences;public object inbox;}
 // Fixed reads and an explicit preference write to the existing local worker.
 // No arbitrary URL, upstream refresh, credentials,
 // identity, usage totals or disk snapshots are read/written by this consumer.
 internal sealed class LocalQuotaReader:IDisposable {
  readonly HttpClient client=new HttpClient(new HttpClientHandler{UseProxy=false,AllowAutoRedirect=false}){Timeout=TimeSpan.FromMilliseconds(2500),MaxResponseContentBufferSize=262144};
  readonly JavaScriptSerializer json=new JavaScriptSerializer{MaxJsonLength=262144,RecursionLimit=20};
  internal string Status="waiting";
  internal LocalQuotaState Project(string text){var state=json.Deserialize<LocalQuotaState>(text);if(state==null||state.schemaVersion!=1||state.mode!="live_local"||state.quota==null||state.quota.demo)throw new FormatException();return state;}
  internal async Task<LocalQuotaState> Read(){try{using(var response=await client.GetAsync("http://127.0.0.1:43188/api/live/state")){if(!response.IsSuccessStatusCode)throw new FormatException();var state=Project(await response.Content.ReadAsStringAsync());Status="connected";return state;}}catch{Status="unavailable";return null;}}
  internal async Task<LocalQuotaState> SetResetAlerts(bool enabled){
   try{
    using(var request=new HttpRequestMessage(HttpMethod.Post,"http://127.0.0.1:43188/api/live/preferences")){
     request.Headers.Add("Origin","http://127.0.0.1:43188");
     request.Content=new StringContent(enabled?"{\"resetAlertsEnabled\":true}":"{\"resetAlertsEnabled\":false}",Encoding.UTF8,"application/json");
     using(var response=await client.SendAsync(request)){
      if(!response.IsSuccessStatusCode)return null;
      var state=Project(await response.Content.ReadAsStringAsync());
      if(state.preferences==null||state.preferences.resetAlertsEnabled!=enabled)return null;
      Status="connected";return state;
     }
    }
   }catch{return null;}
  }
  public void Dispose(){client.Dispose();}
 }
}
