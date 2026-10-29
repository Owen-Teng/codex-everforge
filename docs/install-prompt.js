import { DOWNLOAD_GUIDE_URL } from './download-config.js';

const promptText=document.getElementById('install-prompt-text');
const button=document.getElementById('copy-prompt');
const label=document.getElementById('copy-label');
const hint=document.getElementById('copy-hint');
const status=document.getElementById('copy-status');
// A stable guide chooses the current release; the homepage never embeds a package version.
const prompt=`请按 ${DOWNLOAD_GUIDE_URL} 为本机下载安装并配置 Codex Everforge，校验安装包后执行安装，完成后检查运行状态。`;
promptText.textContent=prompt;
const defaultHint=hint.textContent;
let busy=false,resetTimer;
button.addEventListener('click',async()=>{
  if(busy)return;busy=true;clearTimeout(resetTimer);
  button.classList.remove('copied');button.disabled=true;label.textContent='Copy';status.textContent='';
  try{
    if(!window.isSecureContext||!navigator.clipboard?.writeText)throw new Error('clipboard-unavailable');
    await navigator.clipboard.writeText(prompt);
    button.classList.add('copied');label.textContent='Copied';
    button.setAttribute('aria-label','已复制安装指令');hint.textContent=defaultHint;
    status.textContent='安装指令已复制，可以粘贴给 AI 助手。';
    resetTimer=setTimeout(()=>{button.classList.remove('copied');label.textContent='Copy';button.setAttribute('aria-label','复制安装指令');},3500);
  }catch{
    // Keep a real manual route; never report success when the browser denies copying.
    promptText.focus();const range=document.createRange();range.selectNodeContents(promptText);
    const selection=window.getSelection();selection.removeAllRanges();selection.addRange(range);
    label.textContent='Copy';button.setAttribute('aria-label','重试复制安装指令');
    hint.textContent='自动复制未成功，文字已选中，请手动复制后发给 AI 助手。';status.textContent=hint.textContent;
  }finally{busy=false;button.disabled=false;}
});
