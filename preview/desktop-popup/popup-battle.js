/* Finite, cancellable Web Animations: flip -> impact -> fracture -> resolve. */
(() => {
 'use strict';
 window.MakerPopupBattle={run({stage,outcome,reduced,onPhase}){
  let cancelled=false;const animations=[];
  const play=(el,frames,options)=>{const animation=el.animate(frames,{fill:'forwards',easing:'cubic-bezier(.16,1,.3,1)',...options});animations.push(animation);return animation.finished.catch(()=>{});};
  const phase=name=>{if(cancelled)return false;stage.dataset.phase=name;onPhase(name,animations.filter(a=>a.playState==='running').length);return true;};
  async function execute(){
   if(reduced){phase('settled');return true;}
   phase('sealed');const cards=[...stage.querySelectorAll('.duel-flipper')];
   if(!phase('flipping'))return false;await Promise.all(cards.map((card,index)=>play(card,[{transform:'rotateY(0deg)'},{transform:'rotateY(180deg)'}],{duration:620,delay:index*100})));if(cancelled)return false;
   phase('collision');const wrappers=[...stage.querySelectorAll('.duel-unit')];
   await Promise.all(wrappers.map((card,index)=>play(card,[{transform:'translateX(0) rotate(0deg)'},{transform:`translateX(${index===0?34:-34}px) rotate(${index===0?3:-3}deg)`,offset:.55},{transform:'translateX(0) rotate(0deg)'}],{duration:510})));if(cancelled)return false;
   if(outcome!=='draw'){
    phase('shattering');const loser=stage.querySelector(outcome==='win'?'[data-side="right"]':'[data-side="left"]');
    const front=loser.querySelector('.duel-face.front');const fragmentLayer=document.createElement('div');fragmentLayer.className='fragment-layer';fragmentLayer.inert=true;fragmentLayer.setAttribute('aria-hidden','true');loser.append(fragmentLayer);loser.querySelector('.duel-flipper').style.visibility='hidden';
    const cuts=[[[0,0],[27,0],[23,35],[0,30]],[[27,0],[52,0],[49,30],[23,35]],[[52,0],[76,0],[79,36],[49,30]],[[76,0],[100,0],[100,31],[79,36]],[[0,30],[23,35],[28,68],[0,64]],[[23,35],[49,30],[53,65],[28,68]],[[49,30],[79,36],[74,70],[53,65]],[[79,36],[100,31],[100,66],[74,70]],[[0,64],[28,68],[24,100],[0,100]],[[28,68],[53,65],[51,100],[24,100]],[[53,65],[74,70],[77,100],[51,100]],[[74,70],[100,66],[100,100],[77,100]]];
    await Promise.all(cuts.map((vertices,index)=>{const shard=document.createElement('div');shard.className='card-shard';shard.style.clipPath=`polygon(${vertices.map(p=>p.join('% ')+'%').join(',')})`;shard.innerHTML=front.innerHTML;fragmentLayer.append(shard);const col=index%4,row=Math.floor(index/4),dx=(col-1.5)*52+(outcome==='win'?24:-24),dy=(row-1)*48+40;return play(shard,[{transform:'translate(0,0) rotate(0deg)',opacity:1},{transform:`translate(${dx}px,${dy}px) rotate(${(index%2?1:-1)*(12+index*2)}deg)`,opacity:0}],{duration:670+(index%3)*45,delay:(index%4)*25});}));
   }
   if(cancelled)return false;phase('settled');return true;
  }
  const done=execute().finally(()=>{for(const animation of animations)animation.cancel();stage.querySelectorAll('.fragment-layer').forEach(el=>el.remove());});
  return {done,cancel(){cancelled=true;for(const animation of animations)animation.cancel();stage.querySelectorAll('.fragment-layer').forEach(el=>el.remove());},get activeCount(){return animations.filter(a=>a.playState==='running').length;}};
 }};
})();
