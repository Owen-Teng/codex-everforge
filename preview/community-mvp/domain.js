/* Synthetic-demo domain rules. No network requests or account access. */
(function (root, factory) {
  'use strict';
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  if (root) root.CommunityDomain = api;
})(typeof globalThis !== 'undefined' ? globalThis : this, function () {
  'use strict';

  function text(value) { return typeof value === 'string' ? value.trim() : ''; }
  function length(value) { return Array.from(value).length; }

  function publicIPv4(host) {
    var bytes = host.split('.').map(Number);
    if (bytes.length !== 4 || bytes.some(function (n) { return !Number.isInteger(n) || n < 0 || n > 255; })) return false;
    var a = bytes[0], b = bytes[1], c = bytes[2];
    return !(a === 0 || a === 10 || a === 127 || a >= 224 ||
      (a === 100 && b >= 64 && b <= 127) ||
      (a === 169 && b === 254) ||
      (a === 172 && b >= 16 && b <= 31) ||
      (a === 192 && (b === 168 || (b === 0 && (c === 0 || c === 2)) || (b === 88 && c === 99))) ||
      (a === 198 && (b === 18 || b === 19 || (b === 51 && c === 100))) ||
      (a === 203 && b === 0 && c === 113));
  }

  function publicIPv6(host) {
    // WHATWG URL has already canonicalized this literal. Keep only ordinary
    // global unicast addresses; exclude local, mapped, transition and test space.
    var value = host.replace(/^\[|\]$/g, '');
    var halves = value.split('::');
    if (halves.length > 2) return false;
    var left = halves[0] ? halves[0].split(':') : [];
    var right = halves.length === 2 && halves[1] ? halves[1].split(':') : [];
    var zeroCount = halves.length === 2 ? 8 - left.length - right.length : 0;
    var parts = left.concat(Array(Math.max(0, zeroCount)).fill('0'), right);
    if (parts.length !== 8 || parts.some(function (part) { return !/^[a-f\d]{1,4}$/i.test(part); })) return false;
    var words = parts.map(function (part) { return parseInt(part, 16); });
    if (words[0] < 0x2000 || words[0] > 0x3fff) return false;
    if (words[0] === 0x2001 && (words[1] <= 0x01ff || words[1] === 0x0db8)) return false;
    if (words[0] === 0x2002 || words[0] === 0x3fff) return false;
    return true;
  }

  function validateURL(raw) {
    if (!raw) return { error: 'url_required' };
    // No silent newline/tab stripping or browser repair of backslash URLs.
    if (/[\u0000-\u0020\u007f\\]/.test(raw)) return { error: 'url_invalid' };
    var parsed;
    try { parsed = new URL(raw); } catch (_) { return { error: 'url_invalid' }; }
    if (parsed.protocol !== 'https:') return { error: 'url_https' };
    if (!/^https:\/\//i.test(raw)) return { error: 'url_invalid' };
    if (parsed.username || parsed.password) return { error: 'url_credentials' };
    if (parsed.port && parsed.port !== '443') return { error: 'url_invalid' };
    var host = parsed.hostname.toLowerCase().replace(/\.$/, '');
    if (!host) return { error: 'url_invalid' };
    if (host[0] === '[') {
      if (!publicIPv6(host)) return { error: 'url_local' };
    } else if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
      if (!publicIPv4(host)) return { error: 'url_local' };
    } else {
      var labels = host.split('.');
      if (labels.length < 2 || /(^|\.)(localhost|local|internal|lan|home|test|invalid|example)$/.test(host)) return { error: 'url_local' };
      if (host.length > 253 || labels.some(function (part) { return !/^[a-z\d](?:[a-z\d-]{0,61}[a-z\d])?$/i.test(part); })) return { error: 'url_invalid' };
    }
    return { url: parsed.href, kind: host === 'github.com' ? 'github' : 'website' };
  }

  function validateProject(project) {
    var input = project && typeof project === 'object' ? project : {};
    var value = { title: text(input.title), summary: text(input.summary), url: text(input.url), kind: 'website' };
    var errors = {};
    if (!value.title) errors.title = 'title_required';
    else if (length(value.title) > 60) errors.title = 'title_long';
    if (length(value.summary) > 160) errors.summary = 'summary_long';
    var result = validateURL(value.url);
    if (result.error) errors.url = result.error;
    else { value.url = result.url; value.kind = result.kind; }
    return { ok: Object.keys(errors).length === 0, errors: errors, value: value };
  }

  function validDate(value) {
    if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
    var date = new Date(value + 'T00:00:00.000Z');
    return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
  }

  function compareSnapshots(left, right) {
    function reject(reason) { return { ok: false, outcome: null, reason: reason }; }
    if (!left || !right || typeof left !== 'object' || typeof right !== 'object') return reject('snapshot_missing');
    if (left.complete !== true || right.complete !== true) return reject('incomplete');
    if (![left.tokens, right.tokens].every(function (n) { return Number.isSafeInteger(n) && n >= 0; })) return reject('tokens_invalid');
    if (![left, right].every(function (s) {
      return typeof s.periodId === 'string' && s.periodId.trim().length > 0 &&
        validDate(s.startDate) && validDate(s.endDateExclusive) && s.startDate < s.endDateExclusive;
    })) return reject('period_invalid');
    if (left.periodId !== right.periodId || left.startDate !== right.startDate || left.endDateExclusive !== right.endDateExclusive) return reject('period_mismatch');
    if (left.source !== 'synthetic' || right.source !== 'synthetic') return reject('source_incompatible');
    if (![left.version, right.version].every(function (n) { return Number.isSafeInteger(n) && n >= 1; })) return reject('version_invalid');
    if (left.version !== right.version) return reject('version_mismatch');
    return { ok: true, outcome: left.tokens > right.tokens ? 'win' : left.tokens < right.tokens ? 'loss' : 'draw', reason: null };
  }

  return Object.freeze({ validateProject: validateProject, compareSnapshots: compareSnapshots });
});
