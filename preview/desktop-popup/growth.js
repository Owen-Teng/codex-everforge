/* Synthetic candidate ledger. Rules are generated from the shared v1 JSON.
   This local demo is not authentication, a trusted clock or anti-cheat. */
(function(root,factory){
  const api=typeof module==='object'&&module.exports?factory(require('./rules.js'),require('./domain.js')):factory(root.CommunityGrowthRules,root.CommunityDomain);
  if(typeof module==='object'&&module.exports)module.exports=api;if(root)root.CommunityGrowth=api;
})(typeof globalThis==='undefined'?this:globalThis,function(rules,domain){
  'use strict';
  const fail=code=>{const error=new Error(code);error.code=code;throw error;};
  const integer=value=>Number.isSafeInteger(value)&&value>=0;
  const word=value=>typeof value==='string'&&value.trim().length>0&&value.length<=160;
  function stable(value){if(Array.isArray(value))return '['+value.map(stable).join(',')+']';if(value&&typeof value==='object')return '{'+Object.keys(value).sort().map(key=>JSON.stringify(key)+':'+stable(value[key])).join(',')+'}';return JSON.stringify(value);}
  function rankForXp(xp){if(!integer(xp))fail('invalid_xp');return rules.ranks.filter(rank=>rank.minXp<=xp).at(-1);}
  function utcDay(seconds){if(!integer(seconds)||!Number.isFinite(new Date(seconds*1000).getTime()))fail('invalid_clock');return new Date(seconds*1000).toISOString().slice(0,10);}
  function create({userId='demo-growth-you',seedXp=0,claimed=false}={}){
    if(!word(userId)||!integer(seedXp))fail('invalid_ledger');
    return {version:rules.version,userId,source:'synthetic',seedXp,totalXp:seedXp,displayRankId:rankForXp(seedXp).id,equipMode:'auto',claimed:claimed?{card_created:true,project_completed:true}:{},qualifications:{},daily:{},events:{},history:[]};
  }
  function validate(ledger){if(!ledger||ledger.version!==rules.version||ledger.source!=='synthetic'||!integer(ledger.totalXp)||!integer(ledger.seedXp)||!word(ledger.userId)||!ledger.events||!ledger.claimed||!ledger.daily||!ledger.qualifications||!Array.isArray(ledger.history))fail('invalid_ledger');rankForXp(ledger.totalXp);return ledger;}
  function award(ledger,event,{now=Math.floor(Date.now()/1000)}={}){
    validate(ledger);if(!event||Array.isArray(event)||!word(event.eventId)||event.userId!==ledger.userId||event.source!=='synthetic')fail('invalid_event');
    const date=utcDay(now),signature=stable(event),stored=ledger.events[event.eventId];
    if(stored){if(stored.signature!==signature)fail('event_conflict');return {...stored.receipt,applied:false,replayed:true};}
    let amount=0,reason='awarded',qualification=null;
    if(event.type==='card_created')amount=rules.rewards.card_created;
    else if(event.type==='project_completed'){
      if(!domain.validateProject(event.project).ok||!event.project.summary?.trim())fail('invalid_project');amount=rules.rewards.project_completed;
    }else if(event.type==='duel'){
      if(!word(event.opponentId)||event.opponentId===event.userId||!word(event.periodId)||!Object.hasOwn(rules.rewards.duel,event.outcome)||event.proof?.completed!==true)fail('invalid_duel');
      const comparison=domain.compareSnapshots(event.proof.left,event.proof.right);
      if(!comparison.ok||comparison.outcome!==event.outcome||event.proof.left.periodId!==event.periodId)fail('invalid_duel');
      qualification=JSON.stringify([event.opponentId,event.periodId]);
      if(ledger.qualifications[qualification]){amount=0;reason='opponent_period_already_rewarded';}
      else if((ledger.daily[date]||0)>=rules.maxRewardedDuelsPerDay){amount=0;reason='daily_limit';}
      else amount=rules.rewards.duel[event.outcome];
    }else fail('invalid_type');
    if(event.type!=='duel'&&ledger.claimed[event.type]){amount=0;reason='first_reward_already_awarded';}
    if(!integer(ledger.totalXp+amount))fail('xp_overflow');
    const previousRank=rankForXp(ledger.totalXp),total=ledger.totalXp+amount,rank=rankForXp(total);
    const receipt={eventId:event.eventId,type:event.type,applied:amount>0,awardedXp:amount,totalXp:total,rankId:rank.id,previousRankId:previousRank.id,levelUp:rank.id!==previousRank.id,reason,rulesVersion:rules.version,replayed:false,equippedRankId:ledger.equipMode==='manual'?ledger.displayRankId:rank.id,titleMode:ledger.equipMode==='manual'?'manual':'auto',source:'synthetic',date,outcome:event.outcome||null,opponentId:event.opponentId||null};
    ledger.totalXp=total;
    if(ledger.equipMode!=='manual')ledger.displayRankId=rank.id;
    if(event.type!=='duel'&&amount>0)ledger.claimed[event.type]=true;
    if(event.type==='duel'&&amount>0){ledger.qualifications[qualification]=true;ledger.daily[date]=(ledger.daily[date]||0)+1;}
    ledger.events[event.eventId]={signature,receipt:{...receipt}};ledger.history.push({...receipt});
    return receipt;
  }
  function summary(ledger,{now=Math.floor(Date.now()/1000)}={}){
    validate(ledger);const rank=rankForXp(ledger.totalXp),index=rules.ranks.indexOf(rank),next=rules.ranks[index+1]||null;
    const unlocked=rules.ranks.filter(item=>item.minXp<=ledger.totalXp).map(item=>item.id);
    return {totalXp:ledger.totalXp,rank,level:index+1,nextRank:next,toNext:next?next.minXp-ledger.totalXp:0,progress:next?Math.min(1,(ledger.totalXp-rank.minXp)/(next.minXp-rank.minXp)):1,
      unlocked,displayRankId:unlocked.includes(ledger.displayRankId)?ledger.displayRankId:rank.id,equipMode:ledger.equipMode==='manual'?'manual':'auto',remainingRewardedDuels:Math.max(0,rules.maxRewardedDuelsPerDay-(ledger.daily[utcDay(now)]||0)),date:utcDay(now),recentReceipts:ledger.history.slice(-20).reverse(),
      userId:ledger.userId,rankId:rank.id,currentRankMinXp:rank.minXp,nextRankId:next?.id||null,nextRankMinXp:next?.minXp??null,xpToNext:next?next.minXp-ledger.totalXp:0,intervalProgress:next?Math.min(1,(ledger.totalXp-rank.minXp)/(next.minXp-rank.minXp)):1,unlockedRankIds:unlocked,equippedRankId:unlocked.includes(ledger.displayRankId)?ledger.displayRankId:rank.id,titleMode:ledger.equipMode==='manual'?'manual':'auto',remainingRewardedDuelsToday:Math.max(0,rules.maxRewardedDuelsPerDay-(ledger.daily[utcDay(now)]||0)),rulesVersion:rules.version,dayBoundary:rules.dayBoundary,source:'synthetic'};
  }
  function equip(ledger,rankId){const view=summary(ledger);if(!view.unlocked.includes(rankId))fail('rank_locked');ledger.displayRankId=rankId;ledger.equipMode='manual';return summary(ledger);}
  function autoEquip(ledger){validate(ledger);ledger.equipMode='auto';ledger.displayRankId=rankForXp(ledger.totalXp).id;return summary(ledger);}
  return Object.freeze({create,validate,award,summary,equip,autoEquip,rankForXp,utcDay,rules});
});
