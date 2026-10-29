(() => {
 'use strict';
 document.documentElement.dataset.inputMode='pointer';
 document.addEventListener('pointerdown',()=>{document.documentElement.dataset.inputMode='pointer';},true);
 document.addEventListener('keydown',e=>{if(['Tab','Enter',' ','ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Home','End'].includes(e.key))document.documentElement.dataset.inputMode='keyboard';},true);
 const KEY='maker-desktop-popup:v1',engine=window.CommunityGrowth,domain=window.CommunityDomain;
 const app=document.querySelector('#app'),dialog=document.querySelector('#dialog'),toast=document.querySelector('#toast');
 const languages={'zh-Hans':'简体中文',en:'English',ja:'日本語',th:'ไทย'};
 // The legacy ledger is kept solely for storage compatibility. No award or simulation path is installed.
 const initial=()=>({schema:1,scenario:'fresh',language:'zh-Hans',reducedMotion:false,created:false,publicOptIn:false,profile:{displayName:'本机玩家',handle:'local_maker',project:null},ledger:engine.create(),drafts:{},lastEvent:null});
 let readOnly=false,state;
 try{const raw=localStorage.getItem(KEY);if(raw===null)state=initial();else{const p=JSON.parse(raw);if(p.schema!==1||!p.profile||typeof p.profile.displayName!=='string'||!p.profile.displayName.trim()||[...p.profile.displayName].length>24||!/^[A-Za-z0-9_]{2,24}$/.test(p.profile.handle)||!languages[p.language])throw Error();engine.validate(p.ledger);state=p;}}
 catch{readOnly=true;state=initial();}
 const esc=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 const t=(source,vars={})=>{const value=state.language==='zh-Hans'?source:window.COMMUNITY_TRANSLATIONS?.[state.language]?.[source]||source;return value.replace(/\{(\w+)\}/g,(all,key)=>Object.hasOwn(vars,key)?String(vars[key]):all);};
 const tx=(s,v)=>esc(t(s,v));
  const icons={card:'<rect x="5" y="2" width="14" height="20" rx="3"/><path d="m12 6 3 4-3 4-3-4zM9 18h6"/>',growth:'<path d="M4 20V4m0 16h17M8 15l4-5 4 2 5-8"/>',ledger:'<path d="M5 3h14v18H5zM9 7h6M9 12h6M9 17h4"/>',duel:'<path d="m4 3 16 17M20 3 4 20M3 3l1 5 4-4-5-1Zm18 0-1 5-4-4 5-1ZM3 21l5-1-4-4-1 5Zm18 0-5-1 4-4 1 5Z"/>',settings:'<path d="M4 6h16M4 12h16M4 18h16"/><circle cx="8" cy="6" r="2"/><circle cx="16" cy="12" r="2"/><circle cx="10" cy="18" r="2"/>',close:'<path d="m6 6 12 12M6 18 18 6"/>',lock:'<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4M12 14v3"/>',check:'<path d="m5 12 4 4L19 6"/>',seeker:'<circle cx="12" cy="12" r="4"/><path d="M12 2v3m0 14v3M2 12h3m14 0h3M5 5l2 2m10 10 2 2M5 19l2-2M17 7l2-2"/>',torchbearer:'<path d="M14 2c2 7-5 6-3 11-4-1-3-4-3-4-5 6-1 12 4 12s10-7 2-19Z"/>',shaper:'<path d="m12 3 9 16H3zM12 3v16M3 19l13-9M21 19 8 10"/>',maker:'<path d="m12 2 8 5v10l-8 5-8-5V7zM4 7l8 5 8-5M12 12v10M8 4.5l8 5v5"/>',worldweaver:'<path d="m5 5 14 14M5 19 19 5M5 5h14v14H5z"/><circle cx="5" cy="5" r="2"/><circle cx="19" cy="5" r="2"/><circle cx="19" cy="19" r="2"/><circle cx="5" cy="19" r="2"/>',worldbuilder:'<circle cx="12" cy="12" r="8"/><ellipse cx="12" cy="12" rx="4" ry="8"/><path d="M4 12h16M7 6c4 3 6 3 10 0M7 18c4-3 6-3 10 0"/>',external:'<path d="M14 3h7v7m-9 2 9-9M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5"/>'};

 const icon=name=>`<svg viewBox="0 0 24 24" aria-hidden="true">${icons[name]||icons.maker}</svg>`;
 let route='profile',toastTimer;
 const view=()=>({rank:{id:'seeker',nameZh:'寻光者',nameEn:'Seeker'},displayRankId:'seeker',level:1,totalXp:null});
 function notify(text){toast.textContent=t(text);toast.classList.add('toast-show');clearTimeout(toastTimer);toastTimer=setTimeout(()=>toast.classList.remove('toast-show'),3000);}
 function savePreference(field,value){if(readOnly){notify('资料读取异常，原记录已保留。');return false;}const next={...state,[field]:value};try{localStorage.setItem(KEY,JSON.stringify(next));state=next;return true;}catch{notify('未能保存设置，原记录已保留。');return false;}}
 const surface=window.MakerPopupSurface.install({app,dialog,toast,domain,rules:window.CommunityGrowthRules,view,t,tx,esc,icon,getState:()=>state,getRoute:()=>route,setRoute:value=>{route=value;},storageReadOnly:()=>readOnly,notify,
 languageSelect:id=>`<select id="${id}" data-language aria-label="界面语言">${Object.entries(languages).map(([key,name])=>`<option value="${key}" ${state.language===key?'selected':''}>${name}</option>`).join('')}</select>`,
 toggleMotion:()=>savePreference('reducedMotion',!state.reducedMotion)});
 document.addEventListener('click',e=>{const target=e.target.closest('[data-action]');if(target&&!target.disabled){e.preventDefault();surface.action(target.dataset.action,target);return;}const nav=e.target.closest('[data-route]');if(nav)surface.navigate(nav.dataset.route);});
 document.addEventListener('change',e=>{if(e.target.matches('[data-language]')&&languages[e.target.value]){if(savePreference('language',e.target.value))surface.render();}});
 surface.render();surface.ready();
})();
