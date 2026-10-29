/* Production local preference only. No login, fake user, XP, or account migration. */
(() => {'use strict';
 const KEY='maker-character-selection:v1', LOCAL_ID='local-profile';
 const ranks=['seeker','torchbearer','shaper','maker','worldweaver','worldbuilder'];
 const characters=[
 {id:'male-a',name:'清俊沉静',detail:'黑发浅金 · 深蓝长衣',accent:'#e4c27c',tint:'#283a40'},
 {id:'male-b',name:'强健利落',detail:'短发宽肩 · 敞开短外套',accent:'#e4af7b',tint:'#34363b'},
 {id:'female-a',name:'知性从容',detail:'低发髻 · 象牙白翻领',accent:'#b9d2ce',tint:'#233e45'},
 {id:'female-b',name:'金发飒爽',detail:'黑框眼镜 · 短裙黑丝',accent:'#e8c595',tint:'#303b46'}
 ];
 const find=id=>characters.find(c=>c.id===id);
 let registry={schema:1,profiles:{}},readOnly=false,saveFailed=false,selected=null,hasRecord=false;
 try{const raw=localStorage.getItem(KEY);if(raw!==null){const x=JSON.parse(raw);if(!x||x.schema!==1||!x.profiles||typeof x.profiles!=='object'||Array.isArray(x.profiles))throw Error();registry=x;hasRecord=Object.hasOwn(x.profiles,LOCAL_ID);const r=x.profiles[LOCAL_ID];if(hasRecord&&(!r||typeof r!=='object'||!(r.characterId===null||typeof r.characterId==='string')))throw Error();selected=find(r?.characterId)?r.characterId:null;}}catch{readOnly=true;}
 const active=()=>find(selected)||characters[0];
 const imageFor=(rank='seeker')=>'assets/characters/'+active().id+'-'+(ranks.includes(rank)?rank:'seeker')+'.png';
 const image=(id,rank)=>'assets/characters/'+(find(id)?id:'male-a')+'-'+(ranks.includes(rank)?rank:'seeker')+'.png';
 function persist(id){
  if(readOnly)return false;
  const next={...registry,profiles:{...registry.profiles,[LOCAL_ID]:{characterId:id}}};
  try{localStorage.setItem(KEY,JSON.stringify(next));registry=next;selected=find(id)?id:null;hasRecord=true;saveFailed=false;return true;}catch{saveFailed=true;return false;}
 }
 window.MakerCharacterUI={characters,imageFor,image,active,art(esc,icon){
  return '<div class="maker-character-background" aria-hidden="true"><img src="'+imageFor('seeker')+'" width="1024" height="1536" alt=""></div><button class="maker-character-art" data-action="character-card" aria-label="查看'+esc(active().name)+'的角色卡" title="查看角色卡"><span class="sr-only">查看角色卡</span></button><button class="maker-character-change" data-action="character-select">更换角色</button>';
 },create(api){
  const {esc,icon,tx,notify,state,view}=api;
  let picker=null,pending=null,first=false,dismissed=false,returnFocus=null,alerts=null,pendingAlerts=null,alertsError='',refresh=()=>{},home=null,rankTimer=null;
  const host=message=>window.chrome?.webview?.postMessage(message);
  function palette(){const c=active();document.documentElement.style.setProperty('--character-accent',c.accent);document.documentElement.style.setProperty('--character-tint',c.tint);document.documentElement.dataset.character=c.id;}
  function updatePicker(){picker.querySelectorAll('[data-character-option]').forEach(b=>{const checked=b.dataset.characterOption===pending;b.setAttribute('aria-checked',String(checked));b.tabIndex=checked?0:-1;b.querySelector('small.character-selected').textContent=checked?'已选':'';});picker.querySelector('[data-character-confirm]').disabled=readOnly||!find(pending);}
  function closePicker(skip=false){
   if(!picker?.open)return;
   if(skip&&first&&!hasRecord&&!readOnly)persist(null);
   dismissed=true;picker.close();(returnFocus?.isConnected?returnFocus:document.querySelector('[data-action="character-select"]'))?.focus({preventScroll:true});
  }
  function openPicker(isFirst=false){
   if(document.querySelector('dialog[open]')||window.EverforgeUpgrade?.active)return;
   if(!picker){
    picker=document.createElement('dialog');picker.className='maker-character-picker';picker.setAttribute('aria-labelledby','character-picker-title');document.body.append(picker);
    picker.addEventListener('cancel',e=>{e.preventDefault();closePicker(true);});
    picker.addEventListener('keydown',e=>{
     if(e.key==='Escape')e.stopPropagation();
     if(!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Home','End'].includes(e.key)||!e.target.matches('[data-character-option]'))return;
     e.preventDefault();const i=characters.findIndex(c=>c.id===pending);pending=characters[e.key==='Home'?0:e.key==='End'?3:(i+(e.key==='ArrowLeft'?-1:e.key==='ArrowRight'?1:e.key==='ArrowUp'?-2:2)+4)%4].id;updatePicker();picker.querySelector('[aria-checked=true]').focus();
    });
    picker.addEventListener('click',e=>{
     const bounds=picker.getBoundingClientRect();if(e.target===picker&&(e.clientX<bounds.left||e.clientX>bounds.right||e.clientY<bounds.top||e.clientY>bounds.bottom))return closePicker(true);
     const option=e.target.closest('[data-character-option]');if(option){pending=option.dataset.characterOption;updatePicker();return;}
     if(e.target.closest('[data-character-cancel]'))return closePicker(true);
     if(e.target.closest('[data-character-confirm]')){
      if(!find(pending))return;
      if(!persist(pending)){picker.querySelector('[role=alert]').textContent='角色未能保存，原记录已保留，请稍后重试。';return;}
      closePicker();refresh();document.querySelector('[data-action="character-select"]')?.focus({preventScroll:true});notify('角色已更换，作品、简介与经验保持。');
     }
    });
   }
   first=isFirst;pending=selected||characters[0].id;returnFocus=document.activeElement;
   picker.innerHTML='<header><div><h2 id="character-picker-title">'+(isFirst?'选择你的角色':'更换角色')+'</h2><p>可以跳过，之后点击“更换角色”调整。</p></div><button class="character-picker-close" data-character-cancel aria-label="关闭选角">'+icon('close')+'</button></header><div class="character-options" role="radiogroup" aria-label="四位角色">'+characters.map(c=>'<button role="radio" data-character-option="'+c.id+'"><img src="'+image(c.id,'seeker')+'" width="1024" height="1536" alt="'+esc(c.detail)+'"><span>'+c.name+'<small class="character-selected"></small></span><small>'+c.detail+'</small></button>').join('')+'</div><p class="character-picker-error" role="alert">'+(readOnly?'角色设置读取异常，原记录已保留，暂不覆盖。':'')+'</p><footer><button class="button ghost" data-character-cancel>'+(isFirst?'稍后选择':'取消')+'</button><button class="button primary" data-character-confirm>选定角色</button></footer>';
   updatePicker();picker.showModal();picker.querySelector('[aria-checked=true]').focus();
  }
  function afterRender(){
   palette();
   if(!hasRecord&&!dismissed&&!readOnly&&api.route()==='profile')queueMicrotask(()=>openPicker(true));
   document.querySelectorAll('.character-theme-option').forEach(b=>b.tabIndex=b.getAttribute('aria-checked')==='true'?0:-1);
  }
  function cancelUpgrade(){clearTimeout(rankTimer);rankTimer=null;window.EverforgeUpgrade?.cancel();}
  function receive(message){
   if(message.type==='quota'&&message.status==='connected'){
    const value=message.state?.preferences?.resetAlertsEnabled;
    if(typeof value==='boolean'){
     alerts=value;
     if(pendingAlerts&&value===pendingAlerts.value){clearTimeout(pendingAlerts.timer);pendingAlerts=null;alertsError='';}
     updateAlertControl();
    }
   }
   if((message.type==='preferenceError'||message.type==='resetAlertsResult'&&message.ok===false)&&pendingAlerts){clearTimeout(pendingAlerts.timer);pendingAlerts=null;alertsError='保存失败，仍保留上次实际设置。';updateAlertControl();}
  }
  function updateAlertControl(){
   const b=document.querySelector('[data-action="reset-alerts"]'),label=document.getElementById('reset-alerts-state');
   if(b){b.disabled=alerts===null||!!pendingAlerts;b.setAttribute('aria-checked',String(alerts===true));b.setAttribute('aria-busy',String(!!pendingAlerts));}
   if(label)label.textContent=alertsError||(pendingAlerts?'正在保存，等待本机确认…':alerts===null?'尚未取得本机设置':alerts?'已开启':'已关闭');
  }
  function changeAlerts(){
   if(alerts===null||pendingAlerts)return;
   alertsError='';const value=!alerts;
   pendingAlerts={value,timer:setTimeout(()=>{pendingAlerts=null;alertsError='未能确认保存结果，请重试。';updateAlertControl();},8000)};
   updateAlertControl();host({action:'resetAlerts',enabled:value});
  }
  function settings(){
   return home.settings()+'<div class="settings-line"><span id="reset-alerts-label">消息提醒<small id="reset-alerts-state">'+esc(alertsError||(pendingAlerts?'正在保存，等待本机确认…':alerts===null?'尚未取得本机设置':alerts?'已开启':'已关闭'))+'</small></span><button class="switch" role="switch" aria-labelledby="reset-alerts-label" aria-checked="'+(alerts===true)+'" '+(alerts===null||pendingAlerts?'disabled':'')+' aria-busy="'+!!pendingAlerts+'" data-action="reset-alerts"></button></div><div class="settings-line"><span>界面语言</span>'+api.languageSelect('popup-language')+'</div><div class="settings-line"><span id="popup-motion">减少动画<small>跳过盲比碰撞与碎裂。</small></span><button class="switch" role="switch" aria-labelledby="popup-motion" aria-checked="'+state().reducedMotion+'" data-action="motion"></button></div><p class="account-availability">资料保存在本机，在线账户尚未连接。</p>'+(readOnly||saveFailed?'<p class="storage-preserved-warning" role="alert">角色设置读取或保存异常，原记录已保留。</p>':'');
  }
  document.addEventListener('keydown',e=>{
   if(!e.target.matches('.character-theme-option')||!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;
   e.preventDefault();const options=[...document.querySelectorAll('.character-theme-option')],i=options.indexOf(e.target),next=e.key==='Home'?0:e.key==='End'?2:(i+(e.key==='ArrowLeft'?-1:1)+3)%3;options[next].click();document.querySelector('[data-theme="'+options[next].dataset.theme+'"]')?.focus();
  });
  return {afterRender,openPicker,closeLayers(){dismissed=true;if(picker?.open)closePicker(false);},cancelUpgrade,receive,settings,bind(config){home=config.home;refresh=config.refresh;},action(name,target){if(name==='character-select'){openPicker();return true;}if(name==='character-theme'){home.setTheme(target.dataset.theme);return true;}if(name==='reset-alerts'){changeAlerts();return true;}return false;}};
 }};
})();
