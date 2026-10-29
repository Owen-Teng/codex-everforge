/* N54: attributed reset events first, source-provided text next; read-only. */
(()=>{'use strict';
 const text=(v,n=2000)=>typeof v==='string'?v.slice(0,n).replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f\u200b-\u200f\u202a-\u202e\u2066-\u2069]/g,''):'';
 const stamp=v=>Number.isSafeInteger(v)&&v>0&&v<4102444800?v:null;
 const array=(v,n=80)=>Array.isArray(v)?v.slice(0,n):[];
 const postId=v=>typeof v==='string'&&/^\d{5,25}$/.test(v)?v:'';
 const kinds=new Set(['notice','announced','signal','negative','question','related','insufficient','withdrawn','removed','expired']);
 const labels={notice:'重置预告',announced:'重置声明待核',signal:'重置线索',negative:'尚未重置',question:'关于重置的问句',related:'普通动态',insufficient:'还需要上下文',withdrawn:'消息已撤回',removed:'来源已移除',expired:'历史消息'};
 const summaries={notice:'有了重置安排，具体时间再看看原文。',announced:'来源提到了重置完成，具体内容还需要核对。',signal:'有些重置的线索，范围和是否完成还没说清。',negative:'这条还没确认重置，先别着急。',question:'这是一句询问，还不是重置消息。',related:'这是一条日常动态，还不是重置消息。',insufficient:'目前内容不够完整，先留意后续消息。',withdrawn:'这条消息撤回了，先别按它安排。',removed:'这条内容已移除，等新的消息吧。',expired:'这是之前的消息，当前额度去牌桌看看吧。'};
 const fallback=kind=>({kind,label:labels[kind],summary:summaries[kind]});
 function previewFor(m){
  if(['withdrawn','expired','removed'].includes(m.status))return fallback(m.status);
  if(m.textCompleteness==='removed')return fallback('removed');
  if(m.status==='not_in_latest_snapshot')return {kind:'insufficient',label:'等一等后续消息',summary:'这次更新暂时没找到它，先不判断重置。'};
  const p=m.preview;
  if(p&&kinds.has(p.kind)&&/[\u3400-\u9fff]/.test(p.label)&&/[\u3400-\u9fff]/.test(p.summary)){
   if(m.category==='signal'&&p.kind==='announced')return fallback('signal');
   return {kind:p.kind,label:text(p.label,64),summary:text(p.summary,300)};
  }
  const raw=m.originalText+' '+m.translation;
  if(!m.author||m.author==='作者未标注'||!m.originalText||!m.textCompleteness)return fallback('insufficient');
  if(m.category==='related'||/\b(?:not enough resets|me too|lol|lmao|just kidding)\b/i.test(raw))return fallback('related');
  if(/\b(?:no|not|never|hasn['’]t|haven['’]t|isn['’]t|didn['’]t|won['’]t)\b.{0,28}\breset|\breset.{0,24}\b(?:not|no)\b|(?:没有|并未|尚未|不会|未)(?:完成|发生)?重置/i.test(raw))return fallback('negative');
  if(/[?？]/.test(raw))return fallback('question');
  return fallback(['signal','notice','announced'].includes(m.category)?m.category:'insufficient');
 }
 function postURL(raw,id=''){
  try{const u=new URL(raw);if(u.protocol!=='https:'||!['x.com','www.x.com','twitter.com','www.twitter.com'].includes(u.hostname)||u.username||u.password||u.port)return '';
   const match=u.pathname.match(/^\/[A-Za-z0-9_]{1,30}\/status\/(\d{5,25})\/?$/);if(!match||id&&match[1]!==id)return '';u.search='';u.hash='';return u.href;}catch{return '';}
 }
 function attribution(raw){
  if(!raw||typeof raw!=='object')return null;
  try{const u=new URL(raw.url);if(u.protocol!=='https:'||u.username||u.password||u.port)return null;
   const name=({'codex-resets.com':'Codex Resets','www.codex-resets.com':'Codex Resets','aihot.news':'AIHOT','www.aihot.news':'AIHOT'})[u.hostname];
   if(!name)return null;u.search='';u.hash='';return {name,url:u.href};}catch{return null;}
 }
 function credits(values){const seen=new Set();return values.map(attribution).filter(a=>a&&!seen.has(a.url)&&seen.add(a.url));}
 function reasonLabel(reason){
  const r=text(reason,800).toLowerCase();if(!r)return '';
  if(r==='public_source_upstream_delayed')return '上游采集有延迟，消息可能尚未更新。';
  if(r==='public_sources_partial')return '部分来源暂不可用，详情见各来源状态。';
  if(/http_429/.test(r))return /retry_after/.test(r)?'来源限流，等待来源要求的重试时间。':'来源限流，按预约自动重试。';
  if(/http_5xx/.test(r))return '来源服务暂时出错，按预约自动重试。';
  if(/http_other/.test(r))return '来源暂未接受请求，等待下次自动检查。';
  if(/canceled/.test(r))return '来源请求已中断，等待下次自动检查。';
  if(/disabled|not_enabled/.test(r))return '这个来源已关闭。';
  if(/not_configured|missing_config/.test(r))return '这个来源还没有配置。';
  if(/timeout|deadline/.test(r))return '这次更新超时了，稍后再试。';
  if(/budget|limit|rate|429/.test(r))return '来源检查受到限制，正在等待下次更新。';
  if(/network|connect|unavailable/.test(r))return '来源暂时连不上。';
  if(/parse|invalid|schema|body/.test(r))return '这次数据暂时读不了，等下次更新。';
  return '暂未取得进一步来源说明。';
 }
 function sourceFor(s){return {id:text(s?.id,200),label:text(s?.label,200)||'公开消息来源',state:text(s?.state,80),reasonCode:text(s?.reason,120),reason:reasonLabel(s?.reason),lastAttemptAt:stamp(s?.lastAttemptAt),lastSuccessAt:stamp(s?.lastSuccessAt),nextPollAt:stamp(s?.nextPollAt),upstreamCheckedAt:stamp(s?.upstreamCheckedAt),upstreamCollectedAt:stamp(s?.upstreamCollectedAt),origin:text(s?.origin,120),attribution:attribution(s?.attribution)};}
 const isTibo=m=>/^(?:@?thsottiaux|Tibo(?: Sottiaux)?)$/i.test(m.author.trim())&&/\/thsottiaux\/status\//i.test(m.url);
 function displayFor(m,item){
  if(['withdrawn','removed','expired'].includes(m.preview.kind)||m.status==='not_in_latest_snapshot')return {text:m.preview.summary,basis:'rule_summary',source:'Codex Everforge'};
  const d=item.displayText;
  if(d&&text(d.text,16000).trim()&&['source_translation','rule_summary'].includes(d.basis))return {text:text(d.text,16000),basis:d.basis,source:text(d.source,100)||'来源'};
  if(m.translation.trim()&&m.textCompleteness==='full')return {text:m.translation,basis:'source_translation',source:'来源'};
  return {text:m.preview.summary,basis:'rule_summary',source:'Codex Everforge'};
 }
 const phases=new Set(['announced','author_completed','externally_observed','signal','withdrawn','expired']);
 const evidenceLabels={author_announcement:'作者预告',author_completion:'作者完成声明',contextual_author_completion:'作者完成声明 · 引用链',source_recorded_completion:'来源收录的完成声明',external_observation:'外部观测',reset_signal:'重置线索',withdrawn:'撤回记录'};
 function eventFor(item,messages,index){
  if(!item||typeof item!=='object'||!text(item.id,256)||!phases.has(item.phase)||!['reset','banked_credit'].includes(item.kind))return null;
  const e={id:text(item.id,256),domId:'reset-'+encodeURIComponent(text(item.id,256)),kind:item.kind,phase:item.phase,announcedAt:stamp(item.announcedAt),observedAt:stamp(item.observedAt),confirmedAt:stamp(item.confirmedAt),collectedAt:stamp(item.collectedAt),postIds:array(item.postIds).map(postId).filter(Boolean),evidence:[],attributions:[],deliveryState:text(item.delivery?.state,80)};
  for(const v of array(item.evidence)){
   if(!v||typeof v!=='object')continue;
   const id=postId(v.postId),url=postURL(v.url,id)||attribution({url:v.url})?.url||'';
   e.evidence.push({id:text(v.id,256),type:text(v.type,80),postId:id,url,publishedAt:stamp(v.publishedAt),observedAt:stamp(v.observedAt),collectedAt:stamp(v.collectedAt),attribution:attribution(v.attribution)});
  }
  e.attributions=credits([...array(item.attributions),...e.evidence.map(v=>v.attribution)]);
  const authorTypes=e.phase==='author_completed'?['author_completion','contextual_author_completion','source_recorded_completion']:['author_announcement'];
  e.tibo=e.evidence.some(v=>/\/thsottiaux\/status\//i.test(v.url)&&authorTypes.includes(v.type))||messages.some(m=>e.postIds.includes(m.postId)&&isTibo(m)&&m.category===(e.phase==='author_completed'?'announced':'notice'));
  return e;
 }
 function eventCopy(e){
  if(e.kind==='banked_credit')return ({announced:['重置卡有新安排啦','作者预告了重置卡，具体怎么用看看原文。','重置卡预告'],author_completed:['有重置卡的好消息','来源收录了发放消息，是否可用去账户看看吧。','发放声明'],externally_observed:['有重置卡的新记录','公开监控记下了这条消息，是否可用还要看看账户。','外部观测'],withdrawn:['这条重置卡消息撤回了','先别按这条安排，等新的消息。','已撤回'],expired:['之前的重置卡消息','这是历史记录，当前是否可用去账户看看。','历史消息'],signal:['有些重置卡的线索','细节还没说清，先留意一下。','范围待明']})[e.phase];
  return ({announced:[e.tibo?'Tibo 又要重置啦，快点灯！':'有新的重置预告啦','先期待一下，具体安排看看原文。','重置预告'],author_completed:[e.tibo?'Tibo 说，重置已完成啦':'来源收录了重置完成的消息','去牌桌看看额度有没有回来吧。','作者完成声明'],externally_observed:['有重置的好消息','公开监控有了新记录，看看额度有没有回来。','外部观测'],withdrawn:['这条重置消息撤回了','先别按这条安排，等新的消息。','已撤回'],expired:['之前的重置消息','这是历史记录，当前额度去牌桌看看吧。','历史消息'],signal:['有些重置的线索','范围和是否完成还没说清，先留意一下。','范围待明']})[e.phase];
 }
 function normalize(raw){
  if(!raw||raw.schemaVersion!==1||!Array.isArray(raw.messages)||!raw.source&&!Array.isArray(raw.sources))return null;
  const sources=array(raw.sources,20).filter(s=>s&&typeof s==='object').map(sourceFor),source=sourceFor(raw.source||array(raw.sources)[0]),seen=new Set(),messages=[];
  // Match the worker's bounded 200-message snapshot; do not silently hide its tail.
  for(const item of raw.messages.slice(0,200)){
   const id=postId(item?.postId);if(!id||seen.has(id))continue;seen.add(id);
   const a=typeof item.author==='string'?item.author:item.author?.displayName||item.author?.name||item.author?.handle||'作者未标注';
   const p=item.preview,m={postId:id,url:postURL(item.url,id),author:text(a,100),publishedAt:stamp(item.publishedAt),originalText:text(item.originalText,16000),translation:text(item.translation,16000),textCompleteness:text(item.textCompleteness,80),category:text(item.category,80),status:text(item.status,80),preview:p&&typeof p==='object'?{kind:text(p.kind,40),label:text(p.label,64),summary:text(p.summary,300)}:null,resetEventIds:array(item.resetEventIds).map(v=>text(v,256))};
   m.preview=previewFor(m);m.displayText=displayFor(m,item);
   m.attributions=credits([...array(item.sources).filter(v=>typeof v==='string').map(id=>sources.find(s=>s.id===id)?.attribution||(/^aihot\.news\//.test(id)?{url:'https://aihot.news/codex-reset'}:null)),...(array(item.sources).length?[]:[source.attribution])]);
   messages.push(m);
  }
  const eventIds=new Set(),resetEvents=[];
  for(const [i,item] of array(raw.resetEvents).entries()){const e=eventFor(item,messages,i);if(e&&!eventIds.has(e.id)){eventIds.add(e.id);resetEvents.push(e);}}
  resetEvents.sort((a,b)=>(b.confirmedAt||b.observedAt||b.announcedAt||b.collectedAt||0)-(a.confirmedAt||a.observedAt||a.announcedAt||a.collectedAt||0));
  return {revision:text(String(raw.revision??''),120),source,sources,messages,resetEvents};
 }
 window.MakerInboxUI={normalize,previewFor,create(api){
  let inbox=null,transport='waiting',signature='',refresh=()=>{};
  const {esc,t,tx}=api;
  const time=v=>v?new Intl.DateTimeFormat(api.state().language,{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}).format(new Date(v*1000)):t('未知');
  const link=(url,label,id)=>'<a'+(id?' id="'+esc(id)+'"':'')+' href="'+esc(url)+'" target="_blank" rel="noopener noreferrer">'+esc(label)+api.icon('external')+'</a>';
  const creditLine=(values,scope)=>values.length?'<div class="message-attributions">消息来自 '+values.map((a,i)=>link(a.url,a.name,'credit-'+scope+'-'+i)).join('<span aria-hidden="true"> · </span>')+'</div>':'';
  function health(){
   const saved=!!(inbox?.messages.length||inbox?.resetEvents.length);
   if(transport==='offline')return saved?'已保存消息，稍后自动重连':'暂时连不上，稍后自动重连';
   if(transport==='checking')return '正在看看有没有新消息';
   if(!inbox)return transport==='waiting'?'正在读取消息':'这次消息暂时没读到';
   const status=inbox.source.state;
   if(status==='partial')return '部分来源可用，自动同步中';
   if(['offline','error','unavailable'].includes(status))return saved?'已保存消息，稍后自动重连':'暂时连不上，稍后自动重连';
   if(status==='stale')return saved?'已保存旧消息，稍后自动重连':'暂无可用消息，稍后自动重连';
   if(status==='backoff')return saved?'已保存消息，等待自动重试':'暂未取得消息，等待自动重试';
   if(inbox.source.reasonCode==='public_source_upstream_delayed')return '更新有延迟，稍后自动检查';
   return ({available:'消息已更新，自动同步中',cached:'已保存消息，等待自动同步',ready:'消息已更新，自动同步中',ok:'消息已更新，自动同步中',healthy:'消息已更新，自动同步中',connected:'消息已更新，自动同步中',disabled:'消息来源已关闭',not_configured:'消息来源还未配置',checking:'正在看看有没有新消息',polling:'正在看看有没有新消息',idle:'等待自动更新'})[status]||'消息状态待确认';
  }
  function renderEvent(e){
   const [title,summary,badge]=eventCopy(e),at=e.confirmedAt||e.observedAt||e.announcedAt||e.collectedAt;
   const preferred=e.phase==='author_completed'?['author_completion','contextual_author_completion','source_recorded_completion']:e.phase==='announced'?['author_announcement']:[];
   const original=e.evidence.find(v=>preferred.includes(v.type)&&postURL(v.url))||e.evidence.find(v=>postURL(v.url))||inbox.messages.find(m=>e.postIds.includes(m.postId)&&m.url);
   const record=e.evidence.find(v=>v.url),url=original?.url||record?.url;
   const historical=at&&at<Math.floor(Date.now()/1000)-7*86400;
   return '<article class="message-preview reset-event" data-event-id="'+esc(e.id)+'" data-phase="'+e.phase+'"><div class="message-preview-heading"><h2>'+esc(title)+'</h2><time>'+esc(time(at))+'</time></div><p class="message-preview-summary">'+esc(summary)+'</p><footer><span>'+esc((historical?'历史记录 · ':'')+badge)+'</span>'+(url?link(url,original?'查看原文':'查看记录','original-'+e.domId):'<span class="message-link-unavailable">原文暂无链接</span>')+'</footer>'+creditLine(e.attributions,e.domId)+'<details class="event-evidence" data-preserve-id="'+e.domId+'"><summary id="evidence-'+e.domId+'">消息依据'+(e.evidence.length?' · '+e.evidence.length+' 条':'')+'</summary><div>'+(e.evidence.length?e.evidence.map(v=>'<p><strong>'+esc(evidenceLabels[v.type]||'来源记录')+'</strong><span>'+esc(v.observedAt?'观测于 '+time(v.observedAt):v.publishedAt?'发布于 '+time(v.publishedAt):'采集于 '+time(v.collectedAt))+'</span>'+(v.url?link(v.url,postURL(v.url)?'查看原文':'查看来源','proof-'+e.domId+'-'+encodeURIComponent(v.id)):'')+'</p>').join(''):'<p>详细记录暂缺，先查看来源。</p>')+'<p>来源消息不代表本机额度已恢复，牌桌读数为准。</p></div></details></article>';
  }
  function renderMessage(m){
   const d=m.displayText,isRule=d.basis==='rule_summary';
   const title=['withdrawn','expired','removed'].includes(m.preview.kind)?labels[m.preview.kind]:isTibo(m)?'Tibo 的动态':m.author+'的动态';
   const basis=isRule?'规则摘要':d.source==='AIHOT'?'AIHOT 提供中文':/[\u3400-\u9fff]/.test(d.text)?'来源提供中文':'来源提供文本';
   return '<article class="message-preview message-update" data-post-id="'+m.postId+'" data-preview-kind="'+m.preview.kind+'"><div class="message-preview-heading"><h2>'+esc(title)+'</h2><time>'+esc(time(m.publishedAt))+'</time></div><p class="message-display-text">'+esc(d.text)+'</p><footer><span>'+esc(basis+(isRule&&m.preview.kind!=='related'?' · '+labels[m.preview.kind]:''))+'</span>'+(m.url?link(m.url,'查看原文','post-link-'+m.postId):'<span class="message-link-unavailable">原文链接不可用</span>')+'</footer>'+creditLine(m.attributions,'post-'+m.postId)+'</article>';
  }
  function render(){
   const s=inbox?.source,events=inbox?.resetEvents||[],eventIds=new Set(events.map(e=>e.id)),consumed=new Set(events.flatMap(e=>e.postIds));
   const items=(inbox?.messages||[]).filter(m=>!consumed.has(m.postId)&&!m.resetEventIds.some(id=>eventIds.has(id)));
   const updated=['disabled','not_configured'].includes(s?.state)?s?.lastSuccessAt?'上次更新 '+time(s.lastSuccessAt):'尚无更新时间':transport==='checking'||['checking','polling'].includes(s?.state)?'正在自动检查':s?.nextPollAt&&s.nextPollAt>Math.floor(Date.now()/1000)?'下次自动检查 '+time(s.nextPollAt):'等待下次自动检查';
   return '<header class="popup-page-heading"><h1>'+tx('消息')+'</h1></header><div class="inbox-sync-line" role="status" aria-atomic="true"><span>'+esc(health())+'</span><time>'+esc(updated)+'</time></div><section id="inbox-list" class="inbox-list" aria-label="重置消息与动态">'+(events.length?'<h2 class="inbox-section-label">重置消息</h2>'+events.map(renderEvent).join(''):'')+(items.length?(events.length?'<h2 class="inbox-section-label inbox-updates-label">近期动态</h2>':'')+items.map(renderMessage).join(''):'')+(!events.length&&!items.length?'<div class="inbox-empty"><p>还没有新消息，稍后再来看看。</p></div>':'')+'</section><details class="inbox-diagnostics" data-preserve-id="diagnostics"><summary id="inbox-source-toggle">来源与同步详情</summary><div><strong>'+esc(s?.label||'来源尚未连接')+'</strong>'+(s?.reason?'<p>'+esc(s.reason)+'</p>':'')+'<dl><div><dt>本机检查</dt><dd>'+esc(time(s?.lastAttemptAt))+'</dd></div><div><dt>最近成功</dt><dd>'+esc(time(s?.lastSuccessAt))+'</dd></div><div><dt>上游检查</dt><dd>'+esc(time(s?.upstreamCheckedAt))+'</dd></div><div><dt>上游采集</dt><dd>'+esc(time(s?.upstreamCollectedAt))+'</dd></div></dl>'+(inbox?.sources||[]).map(v=>'<p>'+esc(v.label)+' · '+esc(v.state)+(v.reason?' · '+esc(v.reason):'')+'</p>').join('')+(s?.origin&&/manual|import/.test(s.origin)?'<p>手动补查导入</p>':'')+'<p>中文由来源提供时注明出处；缺少译文时显示规则摘要。本机额度以牌桌读数为准。</p></div></details>';
  }
  const nodeKey=n=>n.dataset?.eventId?'event:'+n.dataset.eventId:n.dataset?.postId?'post:'+n.dataset.postId:n.classList?.contains('inbox-updates-label')?'updates-heading':n.classList?.contains('inbox-section-label')?'reset-heading':'empty';
  function comparableHTML(node){const copy=node.cloneNode(true);if(copy.matches('details'))copy.removeAttribute('open');for(const d of copy.querySelectorAll('details[open]'))d.removeAttribute('open');return copy.outerHTML;}
  function updateVisible(contentChanged){
   const list=document.getElementById('inbox-list');if(!list){refresh();return;}
   const active=document.activeElement,activeId=active?.id,open=new Set([...document.querySelectorAll('details[data-preserve-id][open]')].map(d=>d.dataset.preserveId));
   const scroll=list.scrollTop,box=list.getBoundingClientRect(),anchors=[...list.children].filter(n=>n.matches('article')&&n.getBoundingClientRect().bottom>box.top&&n.getBoundingClientRect().top<box.bottom).map(n=>({key:nodeKey(n),top:n.getBoundingClientRect().top}));
   const template=document.createElement('template');template.innerHTML=render();
   if(contentChanged){
    const freshNodes=[...template.content.querySelector('#inbox-list').children],desired=new Set(freshNodes.map(nodeKey)),previous=new Map([...list.children].map(n=>[nodeKey(n),n])),kept=new Set();
    for(const node of [...list.children])if(!desired.has(nodeKey(node)))node.remove();
    let cursor=list.firstElementChild;
    for(const fresh of freshNodes){
     const key=nodeKey(fresh),old=previous.get(key),node=old&&comparableHTML(old)===fresh.outerHTML?old:fresh;kept.add(node);
     if(node===cursor)cursor=cursor.nextElementSibling;else if(old===cursor){const next=cursor.nextElementSibling;cursor.replaceWith(node);cursor=next;}else list.insertBefore(node,cursor);
    }
    for(const node of [...list.children])if(!kept.has(node))node.remove();
   }
   for(const selector of ['.inbox-sync-line','.inbox-diagnostics']){
    const current=document.querySelector(selector),next=template.content.querySelector(selector);
    if(current&&next&&comparableHTML(current)!==next.outerHTML)current.replaceWith(next);
   }
   for(const d of document.querySelectorAll('details[data-preserve-id]'))d.open=open.has(d.dataset.preserveId);
   if(activeId&&!active.isConnected)document.getElementById(activeId)?.focus({preventScroll:true});
   const anchor=anchors.map(a=>({before:a,node:[...list.children].find(n=>nodeKey(n)===a.key)})).find(a=>a.node);
   if(anchor)list.scrollTop+=anchor.node.getBoundingClientRect().top-anchor.before.top;else list.scrollTop=scroll;
  }
  function receive(message){
   if(message.type!=='quota')return;
   const nextTransport=message.status==='checking'?'checking':message.status==='connected'?'connected':'offline';
   const next=message.status==='connected'?normalize(message.state?.inbox):inbox;
   const key=JSON.stringify([nextTransport,next]);if(key===signature)return;
   const contentChanged=JSON.stringify([inbox?.messages,inbox?.resetEvents])!==JSON.stringify([next?.messages,next?.resetEvents]);
   signature=key;inbox=next;transport=nextTransport;
   if(api.route()==='messages')updateVisible(contentChanged);
  }
  return {render,receive,bind:fn=>{refresh=fn;}};
 }};
})();
