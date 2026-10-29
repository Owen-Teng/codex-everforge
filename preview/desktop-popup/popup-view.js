/* N52 static surfaces: real quota/messages stay intact; no simulation or XP mutation API. */
(()=>{'use strict';
window.MakerPopupSurface={install(api){
 const {app,dialog,esc,icon,tx,t,notify}=api;
 const state=()=>api.getState(),route=()=>api.getRoute(),host=m=>window.chrome?.webview?.postMessage(m);
 const routes={profile:['牌桌','card'],duel:['盲比','duel'],messages:['消息','ledger'],settings:['设置','settings']};
 let mode='popup',memberOpen=false,returnFocus=null,registerInfo=false;
 const home=window.MakerHome.create({...api,state,route,refresh:()=>render()});
 const characters=window.MakerCharacterUI.create({...api,state,route});
 const inbox=window.MakerInboxUI.create({...api,state,route});
 characters.bind({home,refresh:()=>render()});inbox.bind(()=>render());
 const safeName=()=>api.storageReadOnly()?'资料读取异常':state().profile.displayName||'本机玩家';
 const level=()=>'<span class="member-initial-rank" title="初始等级，尚未同步">Lv.1 寻光者</span>';
 let lastTheme=null;
 function metric(){const value={action:'metrics',route:mode==='card'||route()==='profile'?'card':route(),phase:'idle',activeAnimations:0};if(!home.readOnly&&['a','b','c'].includes(home.theme)&&home.theme!==lastTheme){value.theme=home.theme;lastTheme=home.theme;}host(value);}
 function authored(){
  const p=home.profile(state().profile);
  return '<section class="duel-authored" aria-label="本机作品与简介"><button data-action="home-work" title="'+esc(p.project?.title||'')+'"><strong>展示作品</strong><span>'+esc(home.readOnly?'资料读取异常':p.project?.title||'未填写，可留空')+'</span>'+icon('external')+'</button><button data-action="home-bio" title="'+esc(p.bio||'')+'"><strong>个人简介</strong><span>'+esc(home.readOnly?'资料读取异常':p.bio||'未填写，可留空')+'</span>'+icon('external')+'</button></section>';
 }
 function beta(){
  return '<header class="popup-page-heading beta-heading"><h1>盲比</h1><span class="beta-status">待内测</span></header><p class="beta-intro">内测开放后，完成会员注册、确认上传授权并获得资格即可参与。</p>'+authored();
 }

 function settings(){return '<header class="popup-page-heading"><h1>设置</h1></header>'+characters.settings().replace('资料保存在本机，在线账户尚未连接。','资料保存在本机，尚未登录。');}
 function header(){
  return '<header class="popup-top"><span class="popup-brand">'+icon('maker')+'<span class="popup-identity"><button class="popup-account member-name" data-action="member-info" title="'+esc(safeName())+'">'+esc(safeName())+'</button>'+level()+'</span><span class="popup-product">Codex Everforge</span></span></header>';
 }
 function characterCard(){
  const p=home.profile(state().profile),c=window.MakerCharacterUI.active();
  return '<div class="character-surface-shell"><header class="character-surface-header"><button data-action="surface-back"><span>返回</span></button><h1>角色卡</h1></header><main class="character-card-content" tabindex="-1" aria-label="角色卡"><article class="static-character-card"><div class="static-character-picture"><img src="'+window.MakerCharacterUI.imageFor('seeker')+'" width="1024" height="1536" alt="'+esc(c.name)+'上半身立绘"></div><div class="static-character-info"><h2 title="'+esc(safeName())+'">'+esc(safeName())+'</h2><p class="static-rank">Lv.1 寻光者 <small>初始</small></p><dl><div><dt>展示作品</dt><dd title="'+esc(p.project?.title||'')+'">'+esc(home.readOnly?'资料读取异常':p.project?.title||'未填写')+'</dd></div><div><dt>个人简介</dt><dd title="'+esc(p.bio||'')+'">'+esc(home.readOnly?'资料读取异常':p.bio||'未填写')+'</dd></div></dl></div></article><button class="character-card-change" data-action="character-select">更换角色</button></main></div>';
 }
 function render(focus=false){
  const activeId=document.activeElement?.id;
  document.documentElement.lang=state().language;document.title='Codex Everforge';document.body.dataset.surface=mode;
  document.body.classList.toggle('reduce-motion',state().reducedMotion);
  document.body.classList.toggle('maker-home-page',mode==='popup'&&route()==='profile');
  for(const theme of ['a','b','c'])document.body.classList.toggle('theme-'+theme,home.theme===theme);
  if(mode==='card')app.innerHTML=characterCard();
  else{
   const current=Object.hasOwn(routes,route())?route():'profile';
   const body=current==='profile'?home.render():current==='duel'?beta():current==='messages'?inbox.render():settings();
   app.innerHTML='<div class="popup-shell">'+header()+'<main id="content" tabindex="-1" class="popup-content page-'+current+'">'+body+'</main><nav class="popup-nav" aria-label="主要导航">'+Object.entries(routes).map(([id,[label,glyph]])=>'<button data-route="'+id+'" class="'+(id===current?'active':'')+'" aria-label="'+label+'">'+icon(glyph)+'<span>'+label+'</span></button>').join('')+'</nav></div>';
  }
  characters.afterRender();
  if(focus)app.querySelector('main')?.focus({preventScroll:true});else if(activeId)document.getElementById(activeId)?.focus({preventScroll:true});
  metric();
 }
 function closeMember(){
  if(!memberOpen)return;memberOpen=false;if(dialog.open)dialog.close();
  (returnFocus?.isConnected?returnFocus:app.querySelector('.member-name'))?.focus({preventScroll:true});
 }
 function openMember(registration=false){
  if(document.querySelector('dialog[open]'))return;memberOpen=true;registerInfo=registration;returnFocus=document.activeElement;
  dialog.className='dialog member-dialog';dialog.setAttribute('aria-labelledby','member-info-title');dialog.removeAttribute('aria-label');
  dialog.innerHTML='<header class="dialog-head"><h2 id="member-info-title" tabindex="-1">会员信息</h2><button class="close" data-action="member-close" aria-label="关闭会员信息">'+icon('close')+'</button></header><p class="member-login-state">尚未登录</p><dl class="member-facts"><div><dt>本机昵称</dt><dd>'+esc(safeName())+'</dd></div><div><dt>等级称号</dt><dd>Lv.1 寻光者 <small>初始</small></dd></div><div><dt>等级状态</dt><dd>尚未同步</dd></div></dl><section class="member-registration" id="member-registration"><h3>会员服务准备中</h3><p>注册开放后，可创建会员账号，并在确认上传资料的范围后申请内测。</p><p>当前没有提交注册、上传授权或内测申请。</p></section><button class="button primary" data-action="member-close">返回</button>';
  dialog.showModal();dialog.querySelector('h2').focus();
  if(registerInfo)dialog.querySelector('#member-registration')?.scrollIntoView({block:'nearest'});
 }
 function closeLayers(){
  characters.closeLayers();if(memberOpen)closeMember();else if(dialog.open)home.action('home-close');
 }
 function popupSurface(focus=true){
  closeLayers();mode='popup';api.setRoute('profile');host({action:'surface',mode:'popup'});render();
  if(focus)app.querySelector('main')?.focus({preventScroll:true});else if(activeId)document.getElementById(activeId)?.focus({preventScroll:true});
 }
 function openCard(){
  if(document.querySelector('dialog[open]'))return;
  mode='card';api.setRoute('profile');host({action:'surface',mode:'card'});render();
  app.querySelector('main')?.focus({preventScroll:true});
 }
 function navigate(next){
  if(!Object.hasOwn(routes,next))return;
  closeLayers();if(mode==='card'){mode='popup';host({action:'surface',mode:'popup'});}
  api.setRoute(next);render(true);
 }
 function action(name,target){
  if(name==='character-card')return openCard();
  if(name==='surface-back')return popupSurface();
  if(name==='member-info')return openMember();
  if(name==='member-register')return openMember(true);
  if(name==='member-close')return closeMember();
  if(['character-select','character-theme','reset-alerts'].includes(name))return characters.action(name,target);
  if(['home-work','home-bio','home-close','home-clear'].includes(name))return home.action(name);
  if(name==='motion'){if(api.toggleMotion())render();return;}
  // No old create/equip/scenario/reveal/reset/retry action can reach the legacy ledger.
  if(name.startsWith('go-')&&Object.hasOwn(routes,name.slice(3)))return navigate(name.slice(3));
  notify('此入口尚未开放。');
 }
 document.addEventListener('keydown',e=>{
  if(e.key!=='Escape'||document.querySelector('dialog[open]'))return;
  e.preventDefault();if(mode==='card')popupSurface();else host({action:'close'});
 });
 dialog.addEventListener('cancel',e=>{if(memberOpen){e.preventDefault();closeMember();}});
 dialog.addEventListener('click',e=>{if(!memberOpen||e.target!==dialog)return;const b=dialog.getBoundingClientRect();if(e.clientX<b.left||e.clientX>b.right||e.clientY<b.top||e.clientY>b.bottom)closeMember();});
 document.addEventListener('click',e=>{const link=e.target.closest('a[target="_blank"]');if(!link)return;e.preventDefault();if(api.domain.validateProject({title:'link',url:link.href}).ok)host({action:'openExternal',url:link.href});},true);
 window.addEventListener('hashchange',()=>{const next=location.hash.slice(1);if(Object.hasOwn(routes,next))navigate(next);});
 window.chrome?.webview?.addEventListener('message',e=>{
  const m=e.data;if(m?.type==='quota'){home.receive(m);characters.receive(m);inbox.receive(m);}
  else if(m?.type==='preferenceError'||m?.type==='resetAlertsResult')characters.receive(m);
  else if(m?.type==='surface'&&m.mode==='popup'){document.documentElement.dataset.inputMode='pointer';closeLayers();mode='popup';api.setRoute('profile');render();app.querySelector('main')?.focus({preventScroll:true});}
  else if(m?.type==='navigate'&&Object.hasOwn(routes,m.route))navigate(m.route);
  else if(m?.type==='cancel'){closeLayers();mode='popup';api.setRoute('profile');}
 });
 window.addEventListener('pagehide',closeLayers);
 return {render,navigate,action,ready(){host({action:'ready'});metric();}};
 }};
})();
