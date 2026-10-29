import { profiles } from './profiles.js';
import { setProfile, setCardStatus, setInterfacePaused } from './app.js';

const $=id=>document.getElementById(id);
const dialog=$('account-dialog'),status=$('account-status'),account=$('account-button');
let current='guest',pending=null,generation=0,cardState='ready',opener=account;
function present(){
  account.textContent=current==='guest'?'身份预演':profiles[current].username;
  account.setAttribute('aria-label',current==='guest'?'身份预演':`${profiles[current].username}，管理示例账户`);
  $('account-title').textContent=current==='guest'?'让卡片成为你的身份':'你的创造者身份';
  $('session-info').textContent=current==='guest'?'仅示例身份切换；真实账号尚未接通。':`${profiles[current].username} · ${profiles[current].rank} / ${profiles[current].rankEnglish} · ${profiles[current].level}`;
  $('logout-button').hidden=current==='guest';
  document.querySelectorAll('[data-profile]').forEach(button=>button.disabled=!!pending);
}
function show(){opener=document.activeElement;$('view-menu').open=false;present();dialog.showModal();setInterfacePaused(true);}
function finish(){
  if(pending){generation++;pending=null;setCardStatus(cardState);status.textContent='同步已取消，可重新选择示例身份。';present();}
  setInterfacePaused(false);if(opener?.isConnected)opener.focus();
}
account.addEventListener('click',show);
$('close-account').addEventListener('click',()=>dialog.close());
dialog.addEventListener('close',finish);
document.addEventListener('keydown',event=>{
  if(!dialog.open||event.key!=='Tab')return;
  const controls=[...dialog.querySelectorAll('button, select, summary, [href], [tabindex="0"]')].filter(element=>{
    const closedDetails=element.closest('details:not([open])');
    return !element.disabled&&element.getClientRects().length&&(!closedDetails||(element.tagName==='SUMMARY'&&element.parentElement===closedDetails));
  });
  const first=controls[0],last=controls[controls.length-1],active=document.activeElement;
  if(event.shiftKey&&(active===first||!dialog.contains(active))){event.preventDefault();last.focus();}
  else if(!event.shiftKey&&(active===last||!dialog.contains(active))){event.preventDefault();first.focus();}
});
dialog.addEventListener('click',event=>{const r=dialog.getBoundingClientRect();if(event.target===dialog&&(event.clientX<r.left||event.clientX>r.right||event.clientY<r.top||event.clientY>r.bottom))dialog.close();});
document.querySelectorAll('[data-provider]').forEach(button=>button.addEventListener('click',()=>{status.textContent=`${button.dataset.provider} 登录尚未接通。请用下方示例身份体验，当前不会跳转或收集密码。`;}));
async function sync(id,mode='ready'){
  const token=++generation;pending=id;setCardStatus('syncing');status.textContent=`正在同步 ${profiles[id].username} 的示例身份…`;present();if(dialog.open)$('close-account').focus();
  await new Promise(resolve=>setTimeout(resolve,950));
  if(token!==generation)return;
  current=id;pending=null;cardState=mode;setProfile(profiles[id]);setCardStatus(mode);present();
  if(mode==='ready'){
    status.textContent='示例身份已同步，正反面已更新。';$('sync-mode').value='ready';
    if(dialog.open)dialog.close();
  }else{
    status.textContent=mode==='error'?'模拟同步失败。关闭此窗口后可在卡片旁重试。':'这个示例账户尚未生成卡片。关闭此窗口后可载入示例卡。';
    $('dialog-retry').hidden=false;
  }
}
document.querySelectorAll('[data-profile]').forEach(button=>button.addEventListener('click',()=>{ $('dialog-retry').hidden=true;sync(button.dataset.profile,$('sync-mode').value); }));
function retry(){ $('dialog-retry').hidden=true;sync(current==='guest'?'lin':current); }
$('dialog-retry').addEventListener('click',retry);
$('card-retry').addEventListener('click',retry);
function logout(){
  generation++;pending=null;current='guest';cardState='ready';setProfile(profiles.guest);setCardStatus('ready');$('sync-mode').value='ready';$('dialog-retry').hidden=true;
  status.textContent='已退出示例账户，恢复访客卡片。';present();if(dialog.open)dialog.close();
}
$('logout-button').addEventListener('click',logout);
$('card-reset').addEventListener('click',logout);
window.__accountDemo={get state(){return{profileId:current,pending,cardState,dialogOpen:dialog.open,persisted:false};}};
