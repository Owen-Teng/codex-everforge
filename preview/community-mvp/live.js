/* FE-002. In-memory local data only. Never writes quota, usage or account data. */
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  if (root) root.CommunityLive = api;
})(typeof globalThis === 'undefined' ? this : globalThis, function () {
  'use strict';
  const text = (value, max = 500) => typeof value === 'string' ? value.slice(0, max) : '';
  const time = value => Number.isSafeInteger(value) && value > 0 ? value : null;
  const number = value => Number.isInteger(value) && value >= 0 && value <= 100 ? value : null;
  const fresh = (value, now, maxAge = 600) => time(value) !== null && value <= now + 300 && now - value <= maxAge;
  function normalize(raw, now = Math.floor(Date.now() / 1000)) {
    if (!raw || raw.schemaVersion !== 1 || raw.mode !== 'live_local' || !raw.quota || raw.quota.demo === true || !raw.preferences || typeof raw.preferences.resetAlertsEnabled !== 'boolean') throw new Error('invalid_contract');
    const q = raw.quota, r = raw.reset || {};
    const quotaState = ['ready', 'loading', 'error', 'unavailable', 'login_required', 'partial'].includes(q.state) ? q.state : 'unavailable';
    const remaining = number(q.remaining), success = time(q.lastSuccess);
    const quota = { state: quotaState, remaining, partial: q.partial === true, bucket: text(q.bucket, 100), scope: text(q.scope, 150),
      updatedAt: time(q.updatedAt), lastSuccess: success,
      stale: q.stale === true || remaining !== null && (quotaState !== 'ready' || !fresh(success, now, 300)),
      windows: Array.isArray(q.windows) ? q.windows.slice(0, 8).map(w => ({ name: text(w?.name, 100), remaining: number(w?.remaining), resetsAt: time(w?.resetsAt) })) : [] };
    const incoming = r.event;
    const event = incoming && typeof incoming.id === 'string' && ['notice', 'announced', 'correction'].includes(incoming.kind) ? {
      id: text(incoming.id, 256), kind: incoming.kind, publishedAt: time(incoming.publishedAt), summary: text(incoming.summary),
      sourceUrl: text(incoming.sourceUrl, 2048), verification: r.sourceKind === 'official' && incoming.verification === 'official' ? 'official' : 'third_party',
      fresh: fresh(incoming.publishedAt, now, 86400)
    } : null;
    const recovery = r.localRecovery && typeof r.localRecovery === 'object' && !Array.isArray(r.localRecovery) && Object.keys(r.localRecovery).length ? { ...r.localRecovery } : null;
    const n = raw.notification;
    const notification = n && typeof n.id === 'string' && n.id && typeof n.kind === 'string' ? { id: text(n.id, 256), kind: text(n.kind, 40), title: text(n.title, 120), body: text(n.body), createdAt: time(n.createdAt) } : null;
    return { schemaVersion: 1, mode: 'live_local', quota,
      usage: { state: text(raw.usage?.state, 40) || 'unavailable', rankingEligible: false, reason: text(raw.usage?.reason, 160) },
      reset: { state: ['notice', 'announced', 'waiting_local', 'recovered', 'source_unavailable', 'source_stale', 'disabled', 'idle'].includes(r.state) ? r.state : 'source_unavailable', sourceKind: text(r.sourceKind, 80), sourceLabel: text(r.sourceLabel, 150), lastCheckedAt: time(r.lastCheckedAt), lastSuccessAt: time(r.lastSuccessAt), reason: text(r.reason, 100), sourceFresh: r.sourceFresh === true, event, localRecovery: recovery },
      preferences: { resetAlertsEnabled: raw.preferences.resetAlertsEnabled }, notification };
  }
  class NotificationTracker {
    constructor() { this.initialized = false; this.seen = new Set(); this.tests = new Set(); this.wasEnabled = false; this.sourceWasFresh = false; }
    accept(note, enabled, testRequested = false, now = Math.floor(Date.now() / 1000), sourceFresh = true) {
      const initial = !this.initialized, reopening = enabled && !this.wasEnabled, sourceRecovered = sourceFresh && !this.sourceWasFresh;
      this.initialized = true; this.wasEnabled = enabled; this.sourceWasFresh = sourceFresh;
      if (!note || !note.id) return false;
      const test = note.kind === 'test', ids = test ? this.tests : this.seen;
      if (ids.has(note.id)) return false;
      ids.add(note.id); if (ids.size > 256) ids.delete(ids.values().next().value);
      if (!fresh(note.createdAt, now)) return false;
      if (test) return enabled && testRequested;
      const local = ['recovered', 'local_recovery'].includes(note.kind);
      return !initial && !reopening && enabled && (local || sourceFresh && !sourceRecovered && ['notice', 'announced', 'announcement', 'reset'].includes(note.kind));
    }
  }
  function create(options = {}) {
    const transport = options.fetch || globalThis.fetch?.bind(globalThis);
    const clock = options.now || (() => Math.floor(Date.now() / 1000));
    const tracker = new NotificationTracker();
    const listeners = new Set();
    let current = { connected: false, state: null, error: options.enabled ? 'connecting' : 'demo_entry', busy: false, lastNotification: null, testSubmitted: false };
    let stopped = false, timer, request = 0, testRequested = false;
    const emit = () => { for (const fn of listeners) fn(current); };
    async function read() {
      if (!options.enabled || !transport || stopped) return current;
      const sequence = ++request;
      const abort = new AbortController(); const timeout = setTimeout(() => abort.abort(), 4000);
      try {
        const response = await transport('/api/live/state', { cache: 'no-store', credentials: 'same-origin', signal: abort.signal });
        if (!response.ok) throw new Error('service_unavailable');
        const state = normalize(await response.json(), clock());
        if (sequence !== request || stopped) return current;
        const accepted = tracker.accept(state.notification, state.preferences.resetAlertsEnabled, testRequested, clock(), state.reset.sourceFresh);
        if (accepted && state.notification.kind === 'test') testRequested = false;
        current = { ...current, connected: true, error: '', state, lastNotification: accepted ? state.notification : current.lastNotification };
      } catch (error) {
        if (sequence !== request || stopped) return current;
        const previous = current.state;
        current = { ...current, connected: false, error: error.message === 'invalid_contract' ? 'invalid_contract' : 'service_unavailable', state: previous ? { ...previous, quota: { ...previous.quota, stale: true } } : null };
      } finally { clearTimeout(timeout); }
      emit(); return current;
    }
    async function command(kind, value) {
      if (!options.enabled || !current.connected || current.busy || !transport) throw new Error('service_unavailable');
      const routes = { preferences: '/api/live/preferences', refresh: '/api/live/refresh', test: '/api/live/test-notification' };
      if (!routes[kind]) throw new Error('invalid_command');
      const body = kind === 'preferences' ? { resetAlertsEnabled: value === true } : {};
      const abort = new AbortController(); const timeout = setTimeout(() => abort.abort(), 5000);
      current = { ...current, busy: true }; emit();
      try {
        if (kind === 'test') testRequested = true;
        const response = await transport(routes[kind], { method: 'POST', credentials: 'same-origin', cache: 'no-store', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: abort.signal });
        if (!response.ok) {
          if (kind === 'preferences' && value === false) {
            const failure = await response.json().catch(() => null);
            if (failure?.error === 'preferences_not_saved' && failure?.preferences?.resetAlertsEnabled === false) {
              if (current.state) current = { ...current, state: { ...current.state, preferences: { resetAlertsEnabled: false } } };
              tracker.accept(null, false, false, clock(), false);
              throw new Error('off_not_saved');
            }
          }
          throw new Error('request_failed');
        }
        if (kind === 'test') current = { ...current, testSubmitted: true };
        await read();
        if (kind !== 'test' && (!current.connected || kind === 'preferences' && current.state?.preferences.resetAlertsEnabled !== value)) throw new Error('confirmation_unavailable');
        return current;
      } catch (error) { if (kind === 'test') testRequested = false; throw error; }
      finally { clearTimeout(timeout); current = { ...current, busy: false }; emit(); }
    }
    async function tick() { await read(); if (!stopped && options.enabled) timer = setTimeout(tick, 5000); }
    return { get current() { return current; }, subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); },
      start() { if (!options.enabled || timer || stopped) return; tick(); }, read, command,
      stop() { stopped = true; ++request; clearTimeout(timer); } };
  }
  return Object.freeze({ normalize, NotificationTracker, create });
});
