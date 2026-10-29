// Runs before the module graph: the opaque loading screen survives slow/failed JS.
(()=>{
  const $=id=>document.getElementById(id);
  const overlay=$('boot-loader'),bar=$('boot-progress'),fill=$('boot-fill');
  let phase='loading',value=0,finishTimer;
  const slowTimer=setTimeout(()=>{
    if(phase!=='loading')return;
    $('boot-note').textContent='这次加载较慢，请稍候，也可以重新加载。';$('boot-retry').hidden=false;
  },15000);
  function progress(next){
    if(phase!=='loading')return;
    value=Math.max(value,Math.min(100,Math.round(next)));
    bar.setAttribute('aria-valuenow',String(value));fill.style.transform=`scaleX(${value/100})`;$('boot-percent').textContent=value+'%';
  }
  function fail(message='加载未完成，请重新加载。'){
    if(phase==='ready'||phase==='error')return;
    phase='error';clearTimeout(slowTimer);clearTimeout(finishTimer);overlay.setAttribute('aria-busy','false');
    $('boot-label').textContent='角色卡暂未生成';$('boot-note').textContent=message;$('boot-retry').hidden=false;
    document.querySelectorAll('[data-boot-inert]').forEach(e=>{e.inert=false;e.removeAttribute('aria-hidden');});
    document.documentElement.classList.remove('is-booting');overlay.hidden=true;
    const fallback=document.getElementById('fallback');if(fallback)fallback.hidden=false;
    const note=document.querySelector('.public-state');if(note)note.textContent='交互加载失败，已显示兼容画面；仍可通过下方链接下载桌面版。';
  }
  function complete(){
    if(phase!=='loading')return;
    progress(100);phase='finishing';clearTimeout(slowTimer);
    // Keep the page covered until the completed WebGL frame has been submitted.
    requestAnimationFrame(()=>requestAnimationFrame(()=>{
      if(phase!=='finishing')return;
      finishTimer=setTimeout(()=>{
        if(phase!=='finishing')return;
        const hadFocus=overlay.contains(document.activeElement);
        document.querySelectorAll('[data-boot-inert]').forEach(element=>{element.inert=false;element.removeAttribute('aria-hidden');});
        document.documentElement.classList.remove('is-booting');overlay.hidden=true;overlay.setAttribute('aria-busy','false');phase='ready';
        if(hadFocus)document.getElementById('stage')?.focus();
      },matchMedia('(prefers-reduced-motion: reduce)').matches?0:220);
    }));
  }
  $('boot-retry').addEventListener('click',()=>location.reload());
  window.addEventListener('error',event=>{
    if(event.target instanceof HTMLScriptElement||event.target instanceof HTMLLinkElement||event.error)fail('页面资源加载失败，请检查网络后重新加载。');
  },true);
  window.__everforgeBoot={progress,complete,fail,get state(){return{phase,value};}};
  progress(3);
})();
