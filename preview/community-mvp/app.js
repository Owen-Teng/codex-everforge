/* FE-001: self-contained, synthetic community candidate. No account or service calls. */
(() => {
  'use strict';
  const STORAGE_KEY = 'maker-community-demo:v1';
  const LANGUAGES = { 'zh-Hans': '简体中文', en: 'English', ja: '日本語', th: 'ไทย' };
  const domain = window.CommunityDomain;
  const root = document.querySelector('#app');
  const dialog = document.querySelector('#dialog');
  const toast = document.querySelector('#toast');
  const defaults = () => ({
    schema: 1, language: 'zh-Hans', reducedMotion: false, created: false, resetAlert: false,
    card: { id: 'demo-you', handle: 'maker', displayName: 'Owen', rankId: 'maker', xp: 840, foundingMember: false, publicOptIn: false,
      project: { title: 'Focus Garden', summary: '把专注过程变成可回看的花园。', url: 'https://example.com/focus-garden', kind: 'website' } },
    resetState: 'notice', remaining: 72, events: [], drafts: {}
  });
  let storageWarning = false;
  function load() {
    const base = defaults();
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (!raw) return base;
      const saved = JSON.parse(raw);
      if (saved.schema !== 1 || !saved.card || !domain.validateProject(saved.card.project).ok) throw new Error('invalid demo state');
      const name = String(saved.card.displayName || '').trim();
      const handle = String(saved.card.handle || '');
      if (!name || [...name].length > 24 || !/^[a-zA-Z0-9_]{2,24}$/.test(handle)) throw new Error('invalid profile');
      base.language = Object.hasOwn(LANGUAGES, saved.language) ? saved.language : base.language;
      ['created', 'reducedMotion', 'resetAlert'].forEach(key => { base[key] = saved[key] === true; });
      Object.assign(base.card, { displayName: name, handle, foundingMember: saved.card.foundingMember === true,
        publicOptIn: saved.created === true && saved.card.publicOptIn === true,
        project: domain.validateProject(saved.card.project).value });
      base.resetState = ['notice', 'waiting_local', 'recovered', 'source_unavailable'].includes(saved.resetState) ? saved.resetState : 'notice';
      base.remaining = [72, 84].includes(saved.remaining) ? saved.remaining : 72;
      base.events = Array.isArray(saved.events) ? saved.events.slice(-100).filter(x => x && typeof x.event === 'string' && typeof x.at === 'string') : [];
      base.drafts = saved.drafts && typeof saved.drafts === 'object' ? saved.drafts : {};
      return base;
    } catch { storageWarning = true; return base; }
  }
  let state = load();
  let route = 'profile';
  let cardBack = false;
  let galleryFilter = 'all';
  let outcomeScenario = 'win';
  let duel = null;
  let modal = null;
  let toastTimer;
  let returnFocus;
  const liveEntry = location.protocol === 'http:' && location.hostname === '127.0.0.1' && location.port === '43188';
  const live = window.CommunityLive.create({ enabled: liveEntry });
  const html = value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
  function t(source, vars = {}) {
    const translated = state.language === 'zh-Hans' ? source : window.COMMUNITY_TRANSLATIONS?.[state.language]?.[source] || source;
    return translated.replace(/\{(\w+)\}/g, (all, key) => Object.hasOwn(vars, key) ? String(vars[key]) : all);
  }
  const tx = (source, vars) => html(t(source, vars));
  const ICONS = {
    maker: '<path d="m12 2 8 5v10l-8 5-8-5V7z"/><path d="m4 7 8 5 8-5M12 12v10M8 4.5l8 5v5"/>',
    card: '<rect x="5" y="2" width="14" height="20" rx="3"/><path d="m12 6 3 4-3 4-3-4zM9 18h6"/>',
    duel: '<path d="m4 3 12 12m-8 4 11-11M3 3l1 5 4-4-5-1Zm18 0-1 5-4-4 5-1ZM3 21l5-1-4-4-1 5Zm18 0-5-1 4-4 1 5Z"/>',
    gallery: '<rect x="3" y="3" width="7" height="8" rx="1"/><rect x="14" y="3" width="7" height="8" rx="1"/><path d="M3 16h7m4 0h7M3 21h7m4 0h7"/>',
    gift: '<path d="M3 10h18v4H3zM5 14v7h14v-7M12 10v11"/><path d="M12 10C4 10 4 3 8 3c3 0 4 7 4 7Zm0 0c8 0 8-7 4-7-3 0-4 7-4 7Z"/>',
    settings: '<path d="M4 6h16M4 12h16M4 18h16"/><circle cx="8" cy="6" r="2" fill="currentColor"/><circle cx="16" cy="12" r="2" fill="currentColor"/><circle cx="10" cy="18" r="2" fill="currentColor"/>',
    arrow: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
    external: '<path d="M14 3h7v7m-9 2 9-9M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5"/>',
    check: '<path d="m5 12 4 4L19 6"/>',
    close: '<path d="m6 6 12 12M6 18 18 6"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7v.1"/>',
    bell: '<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/>',
    edit: '<path d="m15 4 5 5M4 20l5-1L21 7l-5-5L4 14v6Z"/>',
    flip: '<path d="M4 7h13l-4-4m7 14H7l4 4M4 7v8m16 2V9"/>'
  };
  const icon = name => `<svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[name] || ICONS.maker}</svg>`;
  function button(source, action, kind = '', extra = '') { return `<button class="button ${kind}" data-action="${action}" ${extra}>${tx(source)}</button>`; }
  function save() {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); }
    catch { storageWarning = true; }
  }
  function event(name, detail = {}) {
    state.events.push({ event: name, at: new Date().toISOString(), source: 'synthetic', ...detail });
    state.events = state.events.slice(-100); save();
  }
  function notify(source, vars) {
    toast.textContent = t(source, vars); toast.classList.add('toast-show');
    clearTimeout(toastTimer); toastTimer = setTimeout(() => toast.classList.remove('toast-show'), 4000);
  }
  function languageSelect(id) {
    return `<select id="${id}" class="language-select" data-language aria-label="${tx('界面语言')}">${Object.entries(LANGUAGES).map(([key, label]) => `<option value="${key}" ${state.language === key ? 'selected' : ''}>${label}</option>`).join('')}</select>`;
  }
  const snapshot = tokens => ({ periodId: 'demo-2026-w43', startDate: '2026-10-12', endDateExclusive: '2026-10-19', tokens, source: 'synthetic', complete: true, version: 1 });
  function demoPlayer(id, name, handle, title, summary, tokens) {
    return { id, displayName: name, handle, rankId: 'maker', xp: 620, foundingMember: false, publicOptIn: true,
      project: { title, summary, url: `https://example.com/${handle}`, kind: 'website' }, tokens };
  }
  function galleryPlayers() {
    return [demoPlayer('demo-luna', 'Luna', 'luna', 'Paper Atlas', t('把散落的阅读笔记连成自己的知识地图。'), 18600000),
      demoPlayer('demo-ren', 'Ren', 'ren', 'Tiny Orbit', t('为独立开发者留住每天的小小进展。'), 31200000),
      demoPlayer('demo-mali', 'Mali', 'mali', 'Open Seeds', t('收集可复用的小工具，让好点子继续生长。'), 24800000)];
  }
  const localCard = () => ({ ...state.card, tokens: 24800000 });
  const formatTokens = tokens => (tokens / 1000000).toFixed(1) + ' M';
  function projectSummary(project) {
    return project.summary === '把专注过程变成可回看的花园。' ? t('把专注过程变成可回看的花园。') : project.summary;
  }
  function projectLink(player, dialogLink = false) {
    const checked = domain.validateProject(player.project);
    if (!checked.ok) return `<span class="subtle">${tx('链接不可用，请编辑作品')}</span>`;
    const host = new URL(checked.value.url).hostname;
    return `<button class="work-link ${dialogLink ? 'on-light' : ''}" data-action="project-preview" data-player="${html(player.id)}">${icon('external')}${html(host)}</button>`;
  }
  function playerCard(player, { highlight = false, back = false, preview = false } = {}) {
    if (back) return `<article class="player-card card-back" aria-label="${tx('成长路线')}" data-player-card>
      <h2>${tx('每一次投入，都留下痕迹。')}</h2><p>${tx('六阶称号与经验仅供体验，不能证明真实技能。')}</p>
      <ul class="rank-list">${['寻光者', '执火者', '塑形师', '造物师', '织界者', '创世者'].map(name => `<li class="${name === '造物师' ? 'current' : ''}">${icon('maker')}<span>${tx(name)}${name === '造物师' ? ' · Maker' : ''}</span></li>`).join('')}</ul>
      <p>${tx('首批身份只作纪念，不增加胜率。')}</p></article>`;
    return `<article class="player-card ${highlight ? 'reveal-card' : ''}" data-player-card aria-label="${tx('{name}的玩家卡', { name: player.displayName })}">
      <div class="card-crown"><span class="rank-seal">${icon('maker')}</span><span class="card-status">${preview ? tx('待创建 · 示例卡') : player.foundingMember ? tx('首批同行 · 模拟') : tx('玩家卡 · 模拟')}</span></div>
      <div class="card-art"><img src="assets/maker.png" alt="${tx('原创造物师角色，双手塑造微光中的世界')}" width="1024" height="1536"></div>
      <div class="card-content"><div class="card-heading"><h2>${tx('造物师')}</h2>${icon('maker')}</div>
        <p class="card-name">Maker · <strong>${html(player.displayName)}</strong> <span>@${html(player.handle)}</span></p>
        <div class="card-xp"><span>${tx('成长经验 · 示例')}</span><span>${player.xp} / 2,000 XP</span></div><div class="xp-track"><span style="width:${Math.min(100, player.xp / 20)}%"></span></div>
        <div class="card-stats"><div><span>${tx('本期投入 · 模拟')}</span><strong>${formatTokens(player.tokens ?? 24800000)}</strong></div><div><span>${tx('公开状态')}</span><strong class="card-visibility">${player.publicOptIn ? tx('已自主公开') : tx('未加入广场')}</strong></div></div>
        <section class="card-work ${highlight ? 'work-focus' : ''}" data-project-highlight="${highlight}" aria-label="${tx('卡片作品')}"><div class="card-work-label">${icon('edit')}${tx('正在创造')}</div><h3>${html(player.project.title)}</h3><p>${html(projectSummary(player.project))}</p>${projectLink(player)}</section>
      </div></article>`;
  }
  function title(heading, subtitle, action = '') {
    return `<header class="page-title"><div><h1 tabindex="-1">${tx(heading)}</h1><p>${tx(subtitle)}</p></div>${action}</header>`;
  }
  function publicSwitch(id = 'public-profile') {
    return `<div class="toggle-row"><span id="${id}-label">${tx('让我的玩家卡出现在广场')}<small id="${id}-help">${tx('默认不公开；关闭后立即移出本机模拟广场。')}</small></span><button id="${id}" class="switch" role="switch" aria-checked="${state.card.publicOptIn}" aria-labelledby="${id}-label" aria-describedby="${id}-help" data-action="public" ${state.created ? '' : 'disabled'}></button></div>`;
  }
  function profilePage() {
    const me = localCard();
    return `${title('我的玩家卡', '让对方记住你，也看见你正在创造的东西。', `<div class="mobile-primary">${state.created ? button('去盲比', 'go-duel', 'primary') : button('创建卡片', 'create', 'primary')}</div>`)}
      <div class="profile-layout"><div><div class="card-stage">${playerCard(me, { back: cardBack, preview: !state.created })}</div>
        <div class="card-under"><button class="text-link" data-action="flip">${icon('flip')}${cardBack ? tx('查看正面') : tx('翻面看成长')}</button><span class="subtle">${tx('原创角色 · 候选卡面')}</span></div></div>
        <div class="detail-column"><h2>${state.created ? tx('把投入变成一次有趣的相遇。') : tx('你的第一张造物者身份。')}</h2>
          <p>${tx('作品一直在卡片里。参与盲比后，胜者整张卡向本场对手展示，作品部分会短暂亮起。')}</p>
          <div class="row-actions">${state.created ? button('去盲比', 'go-duel', 'primary') + button('编辑昵称', 'edit-profile') : button('创建我的玩家卡', 'create', 'primary')}</div>
          <div class="section-label"><span>${tx('卡片中的作品')}</span><button class="text-link" data-action="edit-project">${tx('编辑作品')}</button></div>
          <section class="project-detail"><h3>${html(me.project.title)}</h3><p>${html(projectSummary(me.project))}</p>${projectLink(me, true)}</section>
          ${publicSwitch()}<p class="scene-note">${tx('参与对局即同意向本场对手展示胜者卡片；广场公开需要另外选择。')}</p>
          <div class="insight">${icon('info')}<div>${tx('24.8 M、经验和称号都是模拟值。真实使用数据尚未接通。')}</div></div>
          <div class="founder-note"><strong>${tx('首批福利，首发全部免费。')}</strong>${tx('卡面体验、对局与重置消息没有付费门槛。')}<br><button class="text-link" data-action="go-benefits">${tx('看看首批福利')}</button></div>
        </div></div>`;
  }
  function scenarioTools() {
    return `<details class="demo-tools" ${duel ? '' : 'open'}><summary>${tx('演示情景 · 切换胜负与异常')}</summary><div class="demo-controls"><label for="scenario">${tx('下一局情景')}<select id="scenario">${[['win', '我方胜'], ['loss', '对方胜'], ['draw', '平局'], ['zero', '零投入平局'], ['incomplete', '快照不完整'], ['mismatch', '周期不一致']].map(([value, label]) => `<option value="${value}" ${outcomeScenario === value ? 'selected' : ''}>${tx(label)}</option>`).join('')}</select></label><span class="scene-note">${tx('只改模拟对手投入，不影响任何真实成绩。')}</span></div></details>`;
  }
  function duelPage() {
    let main;
    if (!duel) {
      main = `<div class="duel-lobby"><div class="duel-mat"><div class="sealed-card">${icon('maker')}<strong>${tx('你的投入')}</strong><small>${tx('揭晓前隐藏数值')}</small></div><span class="versus">VS</span><div class="sealed-card">${icon('maker')}<strong>${tx('一位造物者')}</strong><small>${tx('同周期 · 模拟对手')}</small></div></div>
        <div class="duel-action"><p>${tx('模拟周期 · 2026.10.12—10.18')}<br>${tx('同周期实际投入较高者胜。首批身份不影响结果。')}</p>
        <label class="check-line duel-consent"><input type="checkbox" id="duel-consent"><span>${tx('我了解：胜者整张玩家卡会展示给本场对手，作品区高亮；不会因此公开到广场。')}</span></label>
        ${state.created ? button('开始盲比', 'start-duel', 'primary', 'disabled id="start-duel"') : button('先创建玩家卡', 'create', 'primary')}
        <p class="scene-note">${tx('开局锁定卡片和快照；揭晓后修改作品不会改变本局卡片。')}</p></div>${scenarioTools()}</div>`;
    } else if (!duel.result.ok) {
      main = `<div class="duel-lobby"><div class="empty">${icon('info')}<h2>${tx('这次不能判胜')}</h2><p>${duel.result.reason === 'incomplete' ? tx('投入快照不完整，缺失数据不能当作零。') : tx('双方周期不一致，请使用相同周期的完整快照。')}</p>${button('返回准备区', 'again', 'primary')}</div>${scenarioTools()}</div>`;
    } else if (!duel.revealed) {
      main = `<div class="duel-lobby"><div class="duel-mat"><div class="sealed-card">${icon('card')}<strong>${html(duel.me.displayName)}</strong><small>${tx('已锁定快照')}</small></div><span class="versus">VS</span><div class="sealed-card">${icon('card')}<strong>${html(duel.opponent.displayName)}</strong><small>${tx('已锁定快照')}</small></div></div><div class="duel-action"><h2>${tx('同一段时间，各自创造。')}</h2><p>${tx('快照与卡片已在本机锁定。准备好看看这次相遇了吗？')}</p>${button('揭晓这场相遇', 'reveal', 'primary')} ${button('返回准备区', 'again', 'ghost')}</div></div>`;
    } else {
      const draw = duel.result.outcome === 'draw';
      const winner = duel.result.outcome === 'win' ? duel.me : duel.opponent;
      const resultHeading = draw ? '势均力敌，一起继续创造。' : duel.result.outcome === 'win' ? '这次，你的投入更高。' : '这次，看见另一位造物者。';
      main = `<div class="scoreline"><div><span>${html(duel.me.displayName)}</span><strong>${formatTokens(duel.left.tokens)}</strong></div><span class="versus">${draw ? '=' : 'VS'}</span><div><span>${html(duel.opponent.displayName)}</span><strong>${formatTokens(duel.right.tokens)}</strong></div></div>
        <div class="result-layout"><div class="card-stage">${playerCard(draw ? duel.me : winner, { highlight: !draw })}</div><div class="result-detail"><h2>${tx(resultHeading)}</h2><p>${tx('模拟周期 · 2026.10.12—10.18')}</p>
          <p class="highlight-explain">${draw ? tx('平局没有胜者。这里保留你的卡片，不冒造胜者或作品奖励。') : tx('胜者整卡已展示，作品区短暂高亮：看看这份投入正在创造什么。')}</p>
          <p>${tx('投入量只是这次游戏的比较口径，不是作品质量、技能或工作绩效评价。')}</p>
          <p>${tx('本次展示没有改变你的广场公开选择。')}</p><div class="row-actions">${button('再体验一局', 'again', 'primary')}${button('查看玩家广场', 'go-gallery')}</div>
          <p class="view-caption">${tx('本局没有真实匹配或经验入账；所有人物和成绩都是示例。')}</p></div></div>${scenarioTools()}`;
    }
    return title('投入盲比', '把同一段时间的投入揭晓，遇见另一位造物者。') + main;
  }
  function galleryPage() {
    let players = galleryPlayers();
    if (state.created && state.card.publicOptIn) players.unshift(localCard());
    if (galleryFilter === 'mine') players = players.filter(player => player.id === state.card.id);
    return `${title('玩家广场', '展示的是主动公开的整张玩家卡，每张卡都有正在创造的作品。')}
      <div class="gallery-empty"><div>${state.card.publicOptIn ? tx('你的卡片已加入本机模拟广场。') : tx('你的卡片尚未公开。获胜也不会自动加入广场。')}<br><span class="scene-note">${tx('下方其他玩家为预置的自愿公开示例，没有真实用户。')}</span></div>${button(state.card.publicOptIn ? '管理公开选择' : '管理我的玩家卡', 'go-profile')}</div>
      <div class="filters" role="group" aria-label="${tx('广场筛选')}">${[['all', '全部示例卡'], ['mine', '只看我的']].map(([value, label]) => `<button class="chip-button ${galleryFilter === value ? 'active' : ''}" data-action="filter" data-filter="${value}" aria-pressed="${galleryFilter === value}">${tx(label)}</button>`).join('')}</div>
      ${players.length ? `<div class="gallery-grid">${players.map((player, i) => `<article class="gallery-item"><div class="gallery-art ${i % 2 ? 'cool' : 'warm'}"><img src="assets/maker.png" alt="" width="1024" height="1536"><span>Maker</span></div><div class="gallery-body"><div class="gallery-meta"><span>${html(player.displayName)}</span><span>· ${tx('自愿公开 · 模拟')}</span></div><h3>${html(player.project.title)}</h3><p>${html(projectSummary(player.project))}</p>${button('查看整张玩家卡', 'view-card', 'small', `data-player="${html(player.id)}"`)}</div></article>`).join('')}</div>` : `<div class="empty"><h2>${tx('这里还没有你的公开卡片')}</h2><p>${tx('保持不公开也能参加对局和体验免费福利。')}</p>${button('返回我的玩家卡', 'go-profile')}</div>`}
      <p class="view-caption">${tx('作品由玩家自行声明，链接格式通过不代表内容安全或权属认证。')}</p>`;
  }
  const resetLabels = () => ({ notice: t('来源公告 · 模拟'), waiting_local: t('等待本机确认 · 模拟'), recovered: t('本机已观察到恢复 · 模拟'), source_unavailable: t('来源暂不可用 · 模拟') });
  function resetDescription() {
    if (state.resetState === 'notice') return t('示例来源提到可能的额度重置。公告不等于你的本机额度已恢复。');
    if (state.resetState === 'waiting_local') return t('已收到模拟预告，正在等待本机观察；示例余量仍为 {count}。', { count: state.remaining });
    if (state.resetState === 'recovered') return t('已模拟本机恢复观察，示例余量从 72 更新为 84。没有连接真实额度。');
    return t('无法读取示例来源。保留上次示例余量，不凭空认定已经重置。');
  }
  function liveTime(seconds) {
    if (!Number.isSafeInteger(seconds) || seconds <= 0) return t('未知');
    return new Intl.DateTimeFormat(state.language, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(seconds * 1000));
  }
  function liveStatus() {
    const current = live.current, q = current.state?.quota;
    if (!current.connected) return q?.remaining !== null && q?.remaining !== undefined ? t('本机旧值：{count}', { count: q.remaining }) + ' · ' + t('实时服务未连接') : t(current.error === 'connecting' ? '正在连接本机服务…' : '实时服务未连接');
    if (!q || q.remaining === null) return t('本机额度暂不可用');
    return t(q.stale ? '本机旧值：{count}' : '本机余量：{count}', { count: q.remaining });
  }
  function liveSourceLink(event) {
    if (!event) return '';
    let url;
    try { url = new URL(event.sourceUrl); } catch { return ''; }
    if (url.protocol !== 'https:' || url.username || url.password || url.port || !['x.com', 'twitter.com', 'www.x.com', 'www.twitter.com'].includes(url.hostname) || !/^\/[A-Za-z0-9_]{1,15}\/status\/\d+\/?$/.test(url.pathname)) return '';
    return `<a class="text-link" href="${html(url.origin + url.pathname)}" target="_blank" rel="noopener noreferrer">${tx('查看来源原帖')}${icon('external')}</a>`;
  }
  function liveDetails(compact = false) {
    const current = live.current, data = current.state;
    if (!data) return `<p>${tx('尚未收到真实本机状态。请使用 43188 实时入口，并等待本机服务启动。')}</p>`;
    const q = data.quota, reset = data.reset, item = reset.event;
    const label = !current.connected ? '连接中断，保留的值已标为旧值。' : q.stale ? '这是旧值，不能当作当前额度。' : q.partial ? '仅部分窗口可用，缺失窗口不当作零。' : q.state === 'ready' && q.remaining !== null ? '额度由本机工具只读获取，不上传到社区。' : '额度尚不可用，缺失值不会当作零。';
    const sourceStatus = !data.preferences.resetAlertsEnabled ? '提醒已关闭；来源轮询停止，额度继续自动刷新。' : reset.sourceFresh ? '来源可读取；公告不等于本机恢复。' : reset.reason === 'upstream_freshness_unknown' ? '来源可读取，但消息时效尚未核验；暂不发送消息提醒。' : reset.state === 'source_stale' || reset.reason === 'upstream_stale' ? '来源信息已过期，重置时间尚未确认。' : '暂时无法读取来源；不会据此判断额度恢复。';
    if (compact) return `<p>${tx(label)}</p><p class="status-caption">${html(q.scope)} · ${tx('上次成功：{time}', { time: liveTime(q.lastSuccess) })}</p>
      <p>${html(reset.sourceLabel || t('来源未提供'))}<br>${tx(reset.sourceKind === 'official' ? '官方来源' : '第三方转述，未独立核实原帖')}</p><p>${tx(sourceStatus)}</p>
      ${item ? `<p class="live-source-text">${html(item.summary)}</p><p class="status-caption">${tx(item.fresh ? '第三方公告' : '消息已过期或时间异常')} · ${html(liveTime(item.publishedAt))}</p>${liveSourceLink(item)}` : ''}
      <p>${tx(reset.localRecovery ? '本机额度已回升' : '尚未观察到本机回升')}</p><p class="status-caption">${tx(data.usage.state === 'available' ? '用量接口可用，暂不用于排名。' : '用量接口暂不可用，暂不用于排名。')}</p>`;
    return `<p>${tx(label)}</p><p class="status-caption">${html(q.scope)} · ${tx('上次成功：{time}', { time: liveTime(q.lastSuccess) })}</p>
      ${q.windows.length ? `<ul class="live-windows">${q.windows.map(w => `<li>${html(w.name)} <strong>${w.remaining === null ? '—' : w.remaining}</strong></li>`).join('')}</ul>` : ''}
      <div class="divider"></div><p><strong>${tx('消息来源')}</strong><br>${html(reset.sourceLabel || t('来源未提供'))} · ${tx(reset.sourceKind === 'official' ? '官方来源' : '第三方转述，未独立核实原帖')}</p>
      <p>${tx(!data.preferences.resetAlertsEnabled ? '提醒已关闭；来源轮询停止，额度继续自动刷新。' : reset.sourceFresh ? '来源可读取；公告不等于本机恢复。' : reset.reason === 'upstream_freshness_unknown' ? '来源可读取，但消息时效尚未核验；暂不发送消息提醒。' : reset.state === 'source_stale' || reset.reason === 'upstream_stale' ? '来源信息已过期，重置时间尚未确认。' : '暂时无法读取来源；不会据此判断额度恢复。')}</p><p class="status-caption">${tx('最后检查：{time}', { time: liveTime(reset.lastCheckedAt) })}<br>${tx('上游成功检查：{time}', { time: liveTime(reset.lastSuccessAt) })}</p>
      ${item ? `<p class="live-source-text">${html(item.summary)}</p><p class="status-caption">${tx(item.fresh ? item.verification === 'official' ? '来源公告' : '第三方公告' : '消息已过期或时间异常')} · ${html(liveTime(item.publishedAt))}</p>${liveSourceLink(item)}` : `<p>${tx('尚无可展示的新公告。')}</p>`}
      <p><strong>${tx(reset.localRecovery ? '本机额度已回升' : '尚未观察到本机回升')}</strong><br>${tx('本机恢复与公告分别判断，不推断由消息源引发。')}</p>
      <p class="status-caption">${tx(data.usage.state === 'available' ? '用量接口可用，暂不用于排名。' : '用量接口暂不可用，暂不用于排名。')}<br>${tx('真实 usage 暂不用于对局，玩家卡与比赛成绩仍为模拟。')}</p>`;
  }
  function livePanel() {
    const current = live.current, enabled = current.state?.preferences.resetAlertsEnabled === true;
    return `<h2>${tx('本机额度与重置提醒')}</h2><p>${tx('真实额度仅在本机展示；公告、失效与本机恢复分开显示。')}</p>
      <div class="toggle-row"><span id="live-alert-label">${tx('重置消息提醒')}<small id="live-alert-help">${tx('默认关闭。关闭停止来源轮询和提醒，额度继续自动更新。')}</small></span><button class="switch" role="switch" data-action="live-alert" aria-checked="${enabled}" aria-labelledby="live-alert-label" aria-describedby="live-alert-help" ${!current.connected || current.busy ? 'disabled' : ''}></button></div>
      <div class="signal-preview" data-live-connected="${current.connected}"><strong>${icon('bell')}${html(liveStatus())}</strong>${liveDetails(true)}<div class="row-actions">${button('查看消息详情', 'notice', 'small')}${button('立即检查', 'live-refresh', 'small', !current.connected || current.busy ? 'disabled' : '')}</div></div>
      <div class="row-actions">${button('发送测试提醒', 'live-test', 'small', !current.connected || !enabled || current.busy ? 'disabled' : '')}<span class="status-caption" role="status">${tx(current.testSubmitted ? '测试请求已提交；是否看到通知仍未知。' : !enabled ? '开启提醒后可测试，不会更改额度或产生正式事件。' : '由 Windows 任务栏程序提交原生通知，不请求浏览器权限。')}</span></div>`;
  }
  function updateLiveRegions() {
    document.querySelectorAll('[data-live-status]').forEach(el => { el.textContent = liveStatus(); });
    if (liveEntry) {
      const focused = document.activeElement?.dataset?.action;
      document.querySelectorAll('[data-live-panel]').forEach(el => { el.innerHTML = livePanel(); });
      document.querySelectorAll('[data-live-details]').forEach(el => { el.innerHTML = liveDetails(); });
      if (focused && focused.startsWith('live-')) document.querySelector(`[data-action="${focused}"]`)?.focus({ preventScroll: true });
    }
  }
  async function liveCommand(kind, value) {
    try { await live.command(kind, value); notify(kind === 'test' ? '测试请求已提交；是否看到通知仍未知。' : kind === 'preferences' ? '提醒设置已由本机服务确认。' : '本机状态已重新读取。'); }
    catch (error) { notify(error.message === 'off_not_saved' ? '本次已关，重启后设置可能恢复。' : '未能提交请求。请检查本机服务，当前设置未被当作成功。'); }
  }
  function benefitsPage() {
    return `${title('首批福利', '在这里，先一起创造。首发全部免费。')}
      <div class="benefit-layout"><section class="benefit-panel">${icon('gift')}<h2>${tx('给第一批同行的人')}</h2><div class="launch-free">${tx('首发免费')}</div><p>${tx('不绑卡，不自动扣费，没有试用倒计时。')}</p><ul>${['原创玩家卡与作品展示', '同周期投入盲比', '自愿加入玩家广场', '重置消息体验与语言设置'].map(source => `<li>${icon('check')}${tx(source)}</li>`).join('')}</ul>
        ${state.card.foundingMember ? `<span class="founder-badge">${icon('check')}${tx('已领取首批徽章 · 模拟')}</span>` : button(state.created ? '领取首批同行徽章' : '建卡并体验首批福利', state.created ? 'claim-founder' : 'create', 'gold full')}
        <p class="small-print">${tx('徽章只作首批同行纪念，不提高胜率。未来商业化与老用户待遇另行确认。')}</p></section>
      <div class="benefit-detail">${liveEntry ? `<div data-live-panel>${livePanel()}</div>` : `<h2>${tx('重置消息，先分清三件事。')}</h2><p>${tx('预告、来源公告、本机恢复分别显示。当前只有假数据预演，没有真实 X 接入或系统推送。')}</p>
        <div class="toggle-row"><span id="reset-label">${tx('开启重置消息预演')}<small id="reset-help">${tx('所有用户可用，只在这个页面演示消息。')}</small></span><button class="switch" role="switch" aria-checked="${state.resetAlert}" aria-labelledby="reset-label" aria-describedby="reset-help" data-action="reset-alert"></button></div>
        <div class="signal-preview" data-reset-status="${state.resetState}"><strong>${icon('bell')}${html(resetLabels()[state.resetState])}</strong><p>${html(resetDescription())}</p><p class="status-caption">${tx('当前示例余量：{count}', { count: state.remaining })}</p>${button('查看消息详情', 'notice', 'small')}</div>
        <label class="field"><span>${tx('模拟消息情景')}</span><select id="reset-scenario">${Object.entries(resetLabels()).map(([value, label]) => `<option value="${value}" ${value === state.resetState ? 'selected' : ''}>${html(label)}</option>`).join('')}</select></label>
        <p class="scene-note">${tx('切换情景不会发送任何消息，也不会产生消耗、经验或真实恢复记录。')}</p><p class="scene-note">${tx('真实状态请在 43188 本机实时入口查看，此入口保持纯演示。')}</p>`}</div></div>`;
  }
  function settingsPage() {
    return `${title('设置', '语言、动态与本机演示数据由你掌握。')}<div class="settings">
      <div class="settings-line"><span>${tx('界面语言')}<small>${tx('中、英、日、泰为本轮候选译文；玩家填写的内容保留原文。')}</small></span>${languageSelect('settings-language')}</div>
      <div class="settings-line"><span id="motion-label">${tx('减少动画')}<small>${tx('关闭卡片倾斜与揭晓动画；同样遵循系统偏好。')}</small></span><button class="switch" role="switch" aria-checked="${state.reducedMotion}" aria-labelledby="motion-label" data-action="motion"></button></div>
      <div class="settings-line"><span>${tx('数据保存在此浏览器')}<small>${tx('只有虚构卡片、选择与演示事件；不连接账号，不上传数据。')}</small></span><span class="pill">${tx('本机模拟')}</span></div>
      <div class="settings-line"><span>${tx(liveEntry ? '本机额度与重置提醒' : '首批权益')}<small>${liveEntry ? `<span data-live-status>${html(liveStatus())}</span>` : tx('首发全部免费，消息和卡面没有付费门槛。')}</small></span>${button(liveEntry ? '查看提醒设置' : '查看福利', 'go-benefits', 'small')}</div>
      <div class="settings-line"><span>${tx('清空并重新体验')}<small>${tx('删除这个 Demo 的卡片、草稿、公开选择和事件，其他应用数据不受影响。')}</small></span>${button('重置演示', 'reset-demo', 'small danger')}</div>
      <details class="demo-tools"><summary>${tx('本机演示事件（{count}条）', { count: state.events.length })}</summary><p class="scene-note">${tx('这些点击只证明交互可用，不是真实注册、比赛、投放或转化。')}</p><ol class="event-log">${state.events.slice(-12).reverse().map(item => `<li><code>${html(item.event)}</code><small>${html(item.at)} · synthetic</small></li>`).join('') || `<li>${tx('还没有演示事件')}</li>`}</ol></details>
      <p class="language-warning">${tx(liveEntry ? '本机额度独立读取；玩家卡、对局、公开与经验仍为模拟。' : '当前候选未接通真实账号、使用采集、支付、X 或通知服务。')}</p></div>`;
  }
  const ROUTES = { profile: ['我的卡片', 'card'], duel: ['投入盲比', 'duel'], gallery: ['玩家广场', 'gallery'], benefits: ['首批福利', 'gift'], settings: ['设置', 'settings'] };
  function render(focusHeading = false) {
    const focused = document.activeElement;
    const focusId = focused?.id;
    const focusAction = focused?.dataset?.action;
    const focusRoute = focused?.dataset?.route;
    document.documentElement.lang = state.language;
    document.title = t('造物者 · 社区 MVP 预演');
    document.querySelector('.skip').textContent = t('跳到内容');
    document.body.classList.toggle('reduce-motion', state.reducedMotion);
    const contents = { profile: profilePage, duel: duelPage, gallery: galleryPage, benefits: benefitsPage, settings: settingsPage };
    root.innerHTML = `<div class="app-frame"><aside class="sidebar"><div class="brand">${icon('maker')}<div class="brand-name">${tx('造物者')}<small>MAKER COMMUNITY</small></div></div>
      <nav class="nav" aria-label="${tx('主要导航')}">${Object.entries(ROUTES).map(([key, [label, glyph]]) => `<button data-route="${key}" class="${route === key ? 'active' : ''}" aria-label="${tx(label)}" ${route === key ? 'aria-current="page"' : ''}>${icon(glyph)}<span>${tx(label)}</span></button>`).join('')}</nav><div class="nav-spacer"></div><div class="sidebar-foot"><div class="account"><span class="avatar">${html(state.card.displayName.slice(0, 1))}</span><span>${html(state.card.displayName)}<br><small>${tx('本机演示身份')}</small></span></div><span data-live-status>${liveEntry ? html(liveStatus()) : tx('额度工具未连接')}</span><br>${tx('所有人物与成绩均为模拟。')}</div></aside>
      <div class="workspace"><div class="topbar"><div class="preview-label"><strong ${liveEntry ? 'data-live-status' : ''}>${liveEntry ? html(liveStatus()) : tx('交互预演 · 示例数据')}</strong><span>${tx(liveEntry ? '玩家卡与对局仍为模拟' : '不会发布或收费')}</span></div><div class="top-actions"><span class="member-state">${tx('首发全部免费')}</span>${languageSelect('top-language')}</div></div>
      <main class="content" id="content" tabindex="-1">${storageWarning ? `<p class="inline-notice" role="status">${tx('浏览器存储不可用或旧数据已损坏；当前可继续体验，本机保存可能不可用。')}</p>` : ''}${contents[route]()}</main></div></div>`;
    if (focusHeading) document.querySelector('h1')?.focus({ preventScroll: true });
    else if (!dialog.open) {
      const next = focusId ? document.getElementById(focusId) : focusAction ? root.querySelector(`[data-action="${focusAction}"]`) : focusRoute ? root.querySelector(`[data-route="${focusRoute}"]`) : null;
      next?.focus({ preventScroll: true });
    }
  }
  function findPlayer(id) {
    if (route === 'duel' && duel && duel.me.id === id) return duel.me;
    if (route === 'duel' && duel && duel.opponent.id === id) return duel.opponent;
    return id === state.card.id ? localCard() : galleryPlayers().find(player => player.id === id);
  }
  function draftKey() { return modal?.type === 'create' ? 'create' : modal?.type === 'edit-profile' ? 'profile' : 'project'; }
  function stashDraft() {
    const form = dialog.querySelector('form[data-editor]');
    if (!form) return;
    state.drafts[draftKey()] = Object.fromEntries(new FormData(form)); save();
  }
  function closeModal() {
    stashDraft(); modal = null;
    if (dialog.open) dialog.close();
    returnFocus?.isConnected && returnFocus.focus({ preventScroll: true });
  }
  function openModal(type, data = {}) {
    returnFocus = document.activeElement;
    modal = { type, ...data }; renderModal(); dialog.showModal();
    dialog.querySelector('input, button, a, select')?.focus();
  }
  function field(name, label, value, { limit, help = '', multiline = false, placeholder = '' } = {}) {
    const attrs = `id="field-${name}" name="${name}" ${limit ? `maxlength="${limit}"` : ''} ${help ? `aria-describedby="help-${name}"` : ''}`;
    return `<label class="field" for="field-${name}"><span>${tx(label)}</span>${multiline ? `<textarea ${attrs}>${html(value)}</textarea>` : `<input ${attrs} value="${html(value)}" placeholder="${html(placeholder)}" autocomplete="off">`}${help ? `<small id="help-${name}">${tx(help)}</small>` : ''}</label>`;
  }
  function renderModal() {
    const type = modal.type;
    dialog.className = type === 'view-card' ? 'dialog dialog-card' : 'dialog';
    let heading = '', body = '';
    if (['create', 'edit-project', 'edit-profile'].includes(type)) {
      const draft = state.drafts[draftKey()] || {};
      const values = { displayName: state.card.displayName, handle: state.card.handle, ...state.card.project, ...draft };
      const profile = type !== 'edit-project';
      const project = type !== 'edit-profile';
      heading = type === 'create' ? '创建我的玩家卡' : type === 'edit-profile' ? '编辑昵称' : '编辑卡片作品';
      body = `<p>${tx(type === 'create' ? '只在本机创建演示卡片，不注册真实账号。作品是卡片固定的一部分。' : '保存后更新你的卡片；已经锁定的对局仍保留开局版本。')}</p><form data-editor novalidate>
        ${profile ? field('displayName', '昵称', values.displayName, { limit: 48, help: '1–24 个字符' }) + field('handle', '玩家标识', values.handle, { limit: 24, help: '2–24 位英文字母、数字或下划线' }) : ''}
        ${project ? field('title', '作品标题', values.title, { limit: 120, help: '1–60 个字符' }) + field('summary', '一句话简介', values.summary, { limit: 320, help: '可留空，最多 160 个字符', multiline: true }) + field('url', 'GitHub 或产品网站', values.url, { help: '仅 HTTPS 公共链接；拒绝本机地址与带凭据链接。', placeholder: 'https://example.com/your-project' }) : ''}
        <div class="form-error" id="form-error" role="alert" aria-live="polite"></div><div class="row-actions"><button class="button primary" type="submit">${tx(type === 'create' ? '创建演示卡片' : '保存修改')}</button>${button('取消', 'close-modal', 'ghost', 'type="button"')}</div><p class="scene-note">${tx('关闭时保留草稿；不会自动公开或发送任何内容。')}</p></form>`;
    } else if (type === 'public-consent') {
      heading = '公开整张玩家卡？';
      body = `<p>${tx('这将把昵称、角色、模拟投入和作品一起放到本机模拟广场。其他预置玩家卡仅是示例。')}</p><p>${tx('你可以随时关闭公开。参与对局和首发福利不要求加入广场。')}</p><div class="row-actions">${button('确认加入模拟广场', 'confirm-public', 'primary')}${button('保持不公开', 'close-modal')}</div>`;
    } else if (type === 'view-card') {
      heading = '玩家卡详情'; body = `${playerCard(modal.player)}<div class="dialog-tail"><span class="scene-note">${tx('自愿公开 · 模拟卡片')}</span>${button('关闭', 'close-modal', 'small')}</div>`;
    } else if (type === 'project-preview') {
      const player = modal.player;
      const validated = domain.validateProject(player.project);
      heading = '查看卡片中的作品';
      body = `<h3 class="wrap-anywhere">${html(player.project.title)}</h3><p>${html(projectSummary(player.project))}</p><p>${tx('作品链接为玩家自行填写；格式验证不代表内容安全或权属认证。示例地址没有真实作品。')}</p><p class="wrap-anywhere">${html(player.project.url)}</p><div class="row-actions">${validated.ok ? `<a class="button primary" href="${html(validated.value.url)}" target="_blank" rel="noopener noreferrer" data-action="open-project" data-player="${html(player.id)}">${tx('打开外部网站')}${icon('external')}</a>` : ''}${button('返回', 'close-modal')}</div>`;
    } else if (type === 'notice') {
      heading = '重置消息详情';
      body = liveEntry ? `<p class="pill">${tx('本机私有只读')}</p><div data-live-details>${liveDetails()}</div>${button('知道了', 'close-modal', 'primary')}` : `<p class="pill">${tx('模拟消息 · 未接真实来源')}</p><h3>${html(resetLabels()[state.resetState])}</h3><p>${html(resetDescription())}</p><ul class="notice-facts"><li>${tx('预告：可能发生重置，还没有确认。')}</li><li>${tx('公告：示例来源的消息，不改本机余量。')}</li><li>${tx('恢复：仅本机观察能确认本机余量；这里也只是模拟。')}</li></ul><p>${tx('消息本身不会产生消耗或经验。')}</p>${button('知道了', 'close-modal', 'primary')}`;
    } else if (type === 'reset-demo') {
      heading = '重置本机演示？'; body = `<p>${tx('将清除本 Demo 的卡片、草稿、公开选择、语言设置和事件。可以重新创建，不影响其他应用。')}</p><div class="row-actions">${button('清空并重新开始', 'confirm-reset', 'danger')}${button('取消', 'close-modal')}</div>`;
    }
    dialog.innerHTML = `<div class="dialog-head"><h2 id="dialog-title">${tx(heading)}</h2><button class="close" data-action="close-modal" aria-label="${tx('关闭')}">${icon('close')}</button></div>${body}`;
    dialog.setAttribute('aria-labelledby', 'dialog-title');
  }
  function navigate(next) {
    if (!Object.hasOwn(ROUTES, next)) return;
    route = next; render(true); window.scrollTo({ top: 0, behavior: 'instant' });
  }
  function startDuel() {
    if (!state.created || !document.querySelector('#duel-consent')?.checked) return;
    const me = structuredClone(localCard());
    const opponent = structuredClone(galleryPlayers()[outcomeScenario === 'loss' ? 1 : outcomeScenario === 'draw' ? 2 : 0]);
    if (outcomeScenario === 'zero') { me.tokens = 0; opponent.tokens = 0; }
    const left = snapshot(me.tokens), right = snapshot(opponent.tokens);
    if (outcomeScenario === 'incomplete') right.complete = false;
    if (outcomeScenario === 'mismatch') right.periodId = 'demo-2026-w42';
    duel = { me, opponent, left, right, result: domain.compareSnapshots(left, right), revealed: false };
    event('duel_started', { scenario: outcomeScenario }); render(true);
  }
  function handleAction(action, target) {
    if (action === 'live-alert') return liveCommand('preferences', !live.current.state?.preferences.resetAlertsEnabled);
    if (action === 'live-refresh') return liveCommand('refresh');
    if (action === 'live-test') return liveCommand('test');
    if (action.startsWith('go-')) return navigate(action.slice(3));
    if (['create', 'edit-profile', 'edit-project', 'reset-demo', 'notice'].includes(action)) return openModal(action);
    if (action === 'close-modal') return closeModal();
    if (action === 'flip') { cardBack = !cardBack; render(); }
    if (action === 'filter') { galleryFilter = target.dataset.filter; render(); }
    if (action === 'view-card') { const player = findPlayer(target.dataset.player); if (player) openModal('view-card', { player: structuredClone(player) }); }
    if (action === 'project-preview') {
      const player = modal?.type === 'view-card' ? modal.player : findPlayer(target.dataset.player);
      if (player) { if (dialog.open) closeModal(); openModal('project-preview', { player: structuredClone(player) }); }
    }
    if (action === 'open-project') event('project_opened', { playerId: target.dataset.player });
    if (action === 'public') {
      if (!state.created) return;
      if (!state.card.publicOptIn) return openModal('public-consent');
      state.card.publicOptIn = false; event('public_opt_in', { value: false }); render(); notify('已移出本机模拟广场。');
    }
    if (action === 'confirm-public') { state.card.publicOptIn = true; event('public_opt_in', { value: true }); closeModal(); render(); notify('已加入本机模拟广场，可随时关闭。'); }
    if (action === 'claim-founder') { if (!state.created || state.card.foundingMember) return; state.card.foundingMember = true; save(); render(); notify('已领取模拟徽章。首批身份不影响胜率。'); }
    if (action === 'reset-alert') { state.resetAlert = !state.resetAlert; event('reset_alert_enabled', { value: state.resetAlert }); render(); notify(state.resetAlert ? '消息预演已开启；不会发送系统通知。' : '消息预演已关闭。'); }
    if (action === 'motion') { state.reducedMotion = !state.reducedMotion; save(); render(); }
    if (action === 'start-duel') startDuel();
    if (action === 'reveal') {
      if (!duel?.result.ok || duel.revealed) return;
      duel.revealed = true; event('duel_revealed', { outcome: duel.result.outcome });
      if (duel.result.outcome !== 'draw') event('winner_card_viewed', { playerId: duel.result.outcome === 'win' ? duel.me.id : duel.opponent.id });
      render(true);
    }
    if (action === 'again') { duel = null; render(true); }
    if (action === 'confirm-reset') {
      closeModal(); try { localStorage.removeItem(STORAGE_KEY); } catch { /* Memory-only mode remains usable. */ }
      state = defaults(); route = 'profile'; duel = null; cardBack = false; galleryFilter = 'all'; outcomeScenario = 'win';
      storageWarning = false;
      save(); render(true); notify('本机演示已重置。');
    }
  }
  document.addEventListener('click', e => {
    const action = e.target.closest('[data-action]');
    if (action && !action.disabled) { if (action.tagName !== 'A') e.preventDefault(); handleAction(action.dataset.action, action); return; }
    const nav = e.target.closest('[data-route]'); if (nav) navigate(nav.dataset.route);
  });
  document.addEventListener('change', e => {
    if (e.target.matches('[data-language]')) {
      stashDraft(); state.language = e.target.value; save(); render(); if (modal) renderModal();
    }
    if (e.target.id === 'scenario') outcomeScenario = e.target.value;
    if (e.target.id === 'duel-consent') { const start = document.querySelector('#start-duel'); if (start) start.disabled = !e.target.checked; }
    if (e.target.id === 'reset-scenario') {
      state.resetState = e.target.value;
      if (state.resetState === 'recovered') state.remaining = 84;
      save(); render();
    }
  });
  dialog.addEventListener('cancel', e => { e.preventDefault(); closeModal(); });
  dialog.addEventListener('click', e => {
    if (e.target !== dialog) return;
    const bounds = dialog.getBoundingClientRect();
    if (e.clientX < bounds.left || e.clientX > bounds.right || e.clientY < bounds.top || e.clientY > bounds.bottom) closeModal();
  });
  dialog.addEventListener('input', () => stashDraft());
  dialog.addEventListener('submit', e => {
    e.preventDefault();
    if (!e.target.matches('[data-editor]')) return;
    const data = Object.fromEntries(new FormData(e.target));
    const error = dialog.querySelector('#form-error');
    const showError = (source, fieldName) => { error.textContent = t(source); const input = e.target.elements[fieldName]; input?.setAttribute('aria-invalid', 'true'); input?.setAttribute('aria-errormessage', 'form-error'); input?.focus(); };
    dialog.querySelectorAll('[aria-invalid]').forEach(input => input.removeAttribute('aria-invalid'));
    if (modal.type !== 'edit-project') {
      data.displayName = data.displayName.trim(); data.handle = data.handle.trim();
      if (!data.displayName || [...data.displayName].length > 24) return showError('昵称需要 1–24 个字符。', 'displayName');
      if (!/^[a-zA-Z0-9_]{2,24}$/.test(data.handle)) return showError('玩家标识需要 2–24 位英文字母、数字或下划线。', 'handle');
    }
    let validation;
    if (modal.type !== 'edit-profile') {
      validation = domain.validateProject(data);
      const errors = {
        title_required: t('请填写作品标题。'), title_long: t('作品标题最多 60 个字符。'), summary_long: t('简介最多 160 个字符。'),
        url_required: t('请填写 HTTPS 公共链接。'), url_https: t('链接需要以 HTTPS 开头。'), url_credentials: t('链接不能包含用户名或密码。'),
        url_local: t('请使用公共网站，不能使用本机、私有或保留地址。'), url_invalid: t('链接格式不正确，请检查网址。')
      };
      if (!validation.ok) { const [fieldName, code] = Object.entries(validation.errors)[0]; return showError(errors[code] || t('链接格式不正确，请检查网址。'), fieldName); }
    }
    const type = modal.type;
    if (type !== 'edit-project') { state.card.displayName = data.displayName; state.card.handle = data.handle; }
    if (validation) state.card.project = validation.value;
    if (type === 'create') { state.created = true; event('card_created'); }
    if (validation) event('project_saved');
    const key = draftKey(); closeModal(); delete state.drafts[key]; save(); render(); notify(type === 'create' ? '演示卡片已创建，默认不公开。' : '修改已保存在本机演示中。');
  });
  root.addEventListener('pointermove', e => {
    if (e.pointerType !== 'mouse' || state.reducedMotion || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const card = e.target.closest('.player-card'); if (!card) return;
    const bounds = card.getBoundingClientRect();
    card.style.setProperty('--rx', `${-(e.clientY - bounds.top - bounds.height / 2) / bounds.height * 4}deg`);
    card.style.setProperty('--ry', `${(e.clientX - bounds.left - bounds.width / 2) / bounds.width * 5}deg`);
  });
  root.addEventListener('pointerout', e => {
    const card = e.target.closest('.player-card');
    if (card && !card.contains(e.relatedTarget)) { card.style.setProperty('--rx', '0deg'); card.style.setProperty('--ry', '0deg'); }
  });
  render();
  live.subscribe(updateLiveRegions);
  live.start();
  window.addEventListener('pagehide', () => live.stop(), { once: true });
})();
